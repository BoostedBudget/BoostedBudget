package database

// Static scanner for session-timezone-dependent SQL in migration files (Issue #1461/#1464
// follow-up; see .claude/skills/db-migration-guide/SKILL.md, "Migrations do not receive the
// application pool's UTC time zone pin"). No database or build tag required — this reads
// backend/migrations/*.up.sql and *.down.sql as text.
//
// Why this exists: database.NewPool/NewPoolFromURL pins every application connection's session
// TimeZone to UTC (Issue #1461), but no migration-application path in this repo goes through
// that pool (`make migrate-up`'s `psql -f`, the standalone `migrate` CLI used by CI and
// docker-entrypoint.sh, and scripts/test-migrations.sh all connect unpinned). Postgres evaluates
// calendar-interval arithmetic (days/weeks/months/years) and CURRENT_DATE in the SESSION time
// zone, not UTC — so a migration that does this arithmetic is silently session-zone-dependent.
// This scanner catches new instances of that pattern before they ship; it does not (and cannot)
// retroactively fix the pre-existing instances already applied to production, which are
// allowlisted below by exact occurrence, each with its own justification.
//
// Scanning BOTH *.up.sql and *.down.sql matters: 000017_migrate_to_timestamp_snapshots.down.sql
// has two of the six hazards on the allowlist below, and a scanner limited to *.up.sql (as an
// earlier, narrower design would have been) would have missed it entirely — exactly how this
// class of bug got into the repo unflagged in the first place.

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// ─── Comment stripping ───────────────────────────────────────────────────────

// stripSQLComments removes `-- ...` line comments and `/* ... */` block comments from sql,
// replacing stripped bytes with spaces (newlines are preserved) so byte offsets and line numbers
// stay stable. Content inside single-quoted string literals (respecting the standard doubled
// single-quote escape form) is copied verbatim and never treated as the start of a comment, so a
// string literal that happens to contain "--" or "/*" is not misparsed.
func stripSQLComments(sql string) string {
	var b strings.Builder
	b.Grow(len(sql))
	i, n := 0, len(sql)
	for i < n {
		c := sql[i]
		switch {
		case c == '\'':
			b.WriteByte(c)
			i++
			for i < n {
				if sql[i] == '\'' {
					if i+1 < n && sql[i+1] == '\'' {
						b.WriteString("''")
						i += 2
						continue
					}
					b.WriteByte('\'')
					i++
					break
				}
				b.WriteByte(sql[i])
				i++
			}
		case c == '-' && i+1 < n && sql[i+1] == '-':
			for i < n && sql[i] != '\n' {
				b.WriteByte(' ')
				i++
			}
		case c == '/' && i+1 < n && sql[i+1] == '*':
			start := i
			i += 2
			for i+1 < n && (sql[i] != '*' || sql[i+1] != '/') {
				i++
			}
			if i+1 < n {
				i += 2
			} else {
				i = n
			}
			for k := start; k < i; k++ {
				if sql[k] == '\n' {
					b.WriteByte('\n')
				} else {
					b.WriteByte(' ')
				}
			}
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

// ─── Hazard model ────────────────────────────────────────────────────────────

type tzHazard struct {
	category string
	snippet  string // whitespace-normalized matched text
}

func normalizeSnippet(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// stringLiteralInteriors returns the [start,end) byte range of the CONTENT of every
// single-quoted string literal in sql (excluding the delimiting quotes themselves; doubled
// single-quote escapes are treated as content, not a boundary). Used to suppress matches that are entirely
// prose sitting inside a string literal — e.g. a COMMENT ON ... IS '...CURRENT_DATE...' or a
// RAISE EXCEPTION message that merely mentions a hazard keyword — rather than executable SQL.
// A match that only partially overlaps a string (e.g. `INTERVAL '20 days'`, where the keyword
// is outside the quotes and only the literal's value is inside) is NOT suppressed by this: only
// containment (fully inside one interior span) counts.
func stringLiteralInteriors(sql string) []struct{ start, end int } {
	var spans []struct{ start, end int }
	i, n := 0, len(sql)
	for i < n {
		if sql[i] != '\'' {
			i++
			continue
		}
		i++ // consume opening quote
		start := i
		for i < n {
			if sql[i] == '\'' {
				if i+1 < n && sql[i+1] == '\'' {
					i += 2
					continue
				}
				break
			}
			i++
		}
		spans = append(spans, struct{ start, end int }{start, i})
		if i < n {
			i++ // consume closing quote
		}
	}
	return spans
}

// ─── Balanced-paren call extraction (for DATE(...), make_interval(...), date_trunc(...)) ────

type call struct {
	start, end int
	inner      string
}

// findCalls finds all case-insensitive, whole-word calls to name(...) in s, using balanced-paren
// scanning (not a regex) so nested parens in the argument list are handled correctly.
func findCalls(s, name string) []call {
	var out []call
	re := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(name) + `\s*\(`)
	for _, loc := range re.FindAllStringIndex(s, -1) {
		open := loc[1] - 1
		depth := 1
		j := loc[1]
		for j < len(s) && depth > 0 {
			switch s[j] {
			case '(':
				depth++
			case ')':
				depth--
			}
			j++
		}
		if depth == 0 {
			out = append(out, call{start: loc[0], end: j, inner: s[open+1 : j-1]})
		}
	}
	return out
}

// countTopLevelCommas counts commas in s that are not nested inside parens.
func countTopLevelCommas(s string) int {
	depth, count := 0, 0
	for _, c := range s {
		switch c {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				count++
			}
		}
	}
	return count
}

// ─── Detection patterns ──────────────────────────────────────────────────────

const calendarUnit = `(?:day|days|week|weeks|month|months|year|years)`

var (
	reCurrentDate = regexp.MustCompile(`(?i)\bCURRENT_DATE\b`)

	// Captures which type token preceded "DEFAULT CURRENT_TIMESTAMP": TIMESTAMPTZ and
	// TIMESTAMP WITH TIME ZONE are absolute-instant-safe; bare TIMESTAMP is not (the implicit
	// cast renders CURRENT_TIMESTAMP's absolute instant in the session zone).
	reCurrentTimestampDefault = regexp.MustCompile(`(?i)(TIMESTAMPTZ|TIMESTAMP\s+WITH\s+TIME\s+ZONE|TIMESTAMP)\s*(?:NOT\s+NULL\s*)?DEFAULT\s+CURRENT_TIMESTAMP`)

	clockFunc             = `(?:NOW\(\)|CURRENT_TIMESTAMP|clock_timestamp\(\)|transaction_timestamp\(\))`
	calendarIntervalLit   = `(?:INTERVAL\s*'[^']*?\b` + calendarUnit + `\b[^']*')`
	calendarIntervalCast  = `(?:'[^']*?\b` + calendarUnit + `\b[^']*'\s*::\s*interval)`
	reClockThenInterval   = regexp.MustCompile(`(?i)` + clockFunc + `\s*[+\-]\s*(?:` + calendarIntervalLit + `|` + calendarIntervalCast + `)`)
	reIntervalThenClock   = regexp.MustCompile(`(?i)(?:` + calendarIntervalLit + `|` + calendarIntervalCast + `)\s*[+\-]\s*` + clockFunc)
	reCalendarIntervalLit = regexp.MustCompile(`(?i)` + calendarIntervalLit)
	reCalendarIntervalCst = regexp.MustCompile(`(?i)` + calendarIntervalCast)

	reMakeIntervalCalendarArg = regexp.MustCompile(`(?i)\b(?:days|weeks|months|years)\s*=>`)

	// Bare identifier chain immediately followed by ::date (e.g. `snapshot_at::date`). Must
	// start with a letter/underscore so it never matches a `$1::date` placeholder or a
	// `'2024-01-01'::date` literal — both already carry an explicit type and are TZ-safe.
	reDateCastBareIdent  = regexp.MustCompile(`(?i)\b([A-Za-z_][\w.]*)\s*::\s*date\b`)
	reDateCastCloseParen = regexp.MustCompile(`(?i)\)\s*::\s*date\b`)
)

// ─── Scanner ──────────────────────────────────────────────────────────────────

// scanForTZHazards strips comments from sqlRaw and returns every session-timezone-dependent
// construct found, in a fixed priority order. Once a byte span is claimed by an earlier
// (more specific) category it cannot be re-claimed by a later, more general one — this avoids
// reporting `NOW() - INTERVAL '20 days'` as both "clock_func_calendar_interval" (the combined
// form) and "calendar_interval_literal" (the standalone form) for the same occurrence.
func scanForTZHazards(sqlRaw string) []tzHazard {
	sql := stripSQLComments(sqlRaw)
	var hazards []tzHazard

	type span struct{ start, end int }
	var claimed []span
	overlaps := func(start, end int) bool {
		for _, c := range claimed {
			if start < c.end && end > c.start {
				return true
			}
		}
		return false
	}
	stringInteriors := stringLiteralInteriors(sql)
	insideStringLiteral := func(start, end int) bool {
		for _, s := range stringInteriors {
			if start >= s.start && end <= s.end {
				return true
			}
		}
		return false
	}
	add := func(category string, start, end int) {
		if insideStringLiteral(start, end) {
			return // prose inside a string literal (e.g. COMMENT ON ... IS '...'), not real SQL
		}
		if overlaps(start, end) {
			return
		}
		claimed = append(claimed, span{start, end})
		hazards = append(hazards, tzHazard{category: category, snippet: normalizeSnippet(sql[start:end])})
	}

	// 1. CURRENT_DATE — resolves to "today" in the session zone.
	for _, loc := range reCurrentDate.FindAllStringIndex(sql, -1) {
		add("CURRENT_DATE", loc[0], loc[1])
	}

	// 2. CURRENT_TIMESTAMP as DEFAULT into a naked (non-TZ) TIMESTAMP column.
	for _, m := range reCurrentTimestampDefault.FindAllStringSubmatchIndex(sql, -1) {
		typeToken := strings.ToUpper(normalizeSnippet(sql[m[2]:m[3]]))
		if typeToken == "TIMESTAMP" {
			add("CURRENT_TIMESTAMP_untz_column", m[0], m[1])
		}
	}

	// 3. A clock function combined with a calendar-unit interval, either operand order,
	// either + or -. (NOW()/CURRENT_TIMESTAMP/clock_timestamp()/transaction_timestamp() are all
	// absolute instants and safe on their own — see negative fixtures — but calendar-unit
	// interval arithmetic against any of them is evaluated in the session zone.)
	for _, loc := range reClockThenInterval.FindAllStringIndex(sql, -1) {
		add("clock_func_calendar_interval", loc[0], loc[1])
	}
	for _, loc := range reIntervalThenClock.FindAllStringIndex(sql, -1) {
		add("clock_func_calendar_interval", loc[0], loc[1])
	}

	// 4. Standalone calendar-unit INTERVAL literal / cast not already claimed above.
	for _, loc := range reCalendarIntervalLit.FindAllStringIndex(sql, -1) {
		add("calendar_interval_literal", loc[0], loc[1])
	}
	for _, loc := range reCalendarIntervalCst.FindAllStringIndex(sql, -1) {
		add("calendar_interval_cast", loc[0], loc[1])
	}

	// 5. make_interval(...) with any calendar-unit named argument. Flagged unconditionally
	// (not just when combined with a clock function) because make_interval's only use is to be
	// added to/subtracted from a timestamp — that's the entire hazard by itself.
	for _, c := range findCalls(sql, "make_interval") {
		if reMakeIntervalCalendarArg.MatchString(c.inner) {
			add("calendar_make_interval", c.start, c.end)
		}
	}

	// 6. DATE(<expr>) where <expr> does not already pin AT TIME ZONE.
	for _, c := range findCalls(sql, "DATE") {
		if !strings.Contains(strings.ToUpper(c.inner), "AT TIME ZONE") {
			add("date_func_untz", c.start, c.end)
		}
	}

	// 7. <expr>::date where <expr> does not already pin AT TIME ZONE (bare identifier form).
	for _, m := range reDateCastBareIdent.FindAllStringSubmatchIndex(sql, -1) {
		add("timestamptz_date_cast", m[0], m[1])
	}

	// 7b. (<expr>)::date — parenthesized form; walk back to the matching '(' and check inside.
	for _, loc := range reDateCastCloseParen.FindAllStringIndex(sql, -1) {
		closeIdx := loc[0] // index of the ')'
		depth, openIdx := 0, -1
		for k := closeIdx; k >= 0; k-- {
			switch sql[k] {
			case ')':
				depth++
			case '(':
				depth--
				if depth == 0 {
					openIdx = k
				}
			}
			if openIdx != -1 {
				break
			}
		}
		if openIdx == -1 {
			continue
		}
		inner := sql[openIdx+1 : closeIdx]
		if !strings.Contains(strings.ToUpper(inner), "AT TIME ZONE") {
			add("timestamptz_date_cast", openIdx, loc[1])
		}
	}

	// 8. date_trunc(...) without an explicit third (timezone) argument.
	for _, c := range findCalls(sql, "date_trunc") {
		if countTopLevelCommas(c.inner) < 2 {
			add("date_trunc_no_tz", c.start, c.end)
		}
	}

	return hazards
}

// ─── Allowlist ────────────────────────────────────────────────────────────────

// allowedTZHazard is one exact, pre-existing hazard occurrence in an already-applied migration.
// Allowlisting is by (file, category, exact normalized snippet, count) — never by whole file —
// so any new hazard added to an allowlisted file, or any change to the count of an existing one,
// fails the test. Removing the underlying hazard (e.g. if the migration were ever rewritten,
// which it can't be — it's already applied) must also fail, as a stale-entry check: see
// TestMigrationTZHazardScan_NoUnallowlistedHazards, which compares both directions.
type allowedTZHazard struct {
	file     string
	category string
	snippet  string
	count    int
	reason   string
}

var tzHazardAllowlist = []allowedTZHazard{
	{
		file:     "000105_add_past_due_started_at.up.sql",
		category: "clock_func_calendar_interval",
		snippet:  "NOW() - INTERVAL '20 days'",
		count:    1,
		reason: "Pre-existing, already-applied migration. Writes past_due_started_at, the anchor " +
			"for the 21-day auto-cancel grace window, using session-zone calendar-day arithmetic. " +
			"Immutable (already applied); documented in .claude/skills/db-migration-guide/SKILL.md.",
	},
	{
		file:     "000016_create_balance_snapshots_table.up.sql",
		category: "CURRENT_DATE",
		snippet:  "CURRENT_DATE",
		count:    1,
		reason: "Pre-existing, already-applied migration. Backfills snapshot_date (a DATE column) " +
			"from CURRENT_DATE, which resolves in the session zone. Immutable.",
	},
	{
		file:     "000017_migrate_to_timestamp_snapshots.down.sql",
		category: "date_func_untz",
		snippet:  "DATE(bs2.snapshot_at)",
		count:    1,
		reason: "Pre-existing rollback migration. Decides which duplicate snapshot rows get " +
			"deleted using a session-zone DATE() cast with no AT TIME ZONE. Only runs on manual " +
			"rollback, but the SQL is already applied/committed history and cannot be edited.",
	},
	{
		file:     "000017_migrate_to_timestamp_snapshots.down.sql",
		category: "date_func_untz",
		snippet:  "DATE(bs1.snapshot_at)",
		count:    1,
		reason:   "Same statement as bs2.snapshot_at above — the other operand of the duplicate-detection comparison.",
	},
	{
		file:     "000017_migrate_to_timestamp_snapshots.down.sql",
		category: "date_func_untz",
		snippet:  "DATE(snapshot_at)",
		count:    1,
		reason: "Pre-existing rollback migration. USING clause converts TIMESTAMPTZ back to DATE " +
			"via a session-zone DATE() cast with no AT TIME ZONE. Immutable.",
	},
	{
		file:     "000004_create_subscription_overrides_table.up.sql",
		category: "CURRENT_TIMESTAMP_untz_column",
		snippet:  "TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP",
		count:    2,
		reason: "Pre-existing, already-applied migration. created_at and updated_at are both a " +
			"naked TIMESTAMP (no time zone) with DEFAULT CURRENT_TIMESTAMP; the implicit cast " +
			"renders CURRENT_TIMESTAMP's absolute instant in the session zone. Discovered " +
			"independently while building this scanner — not previously documented anywhere. " +
			"Immutable (already applied). Migration 000130 converts expires_at/created_at/" +
			"updated_at to TIMESTAMPTZ, so this specific hazard no longer describes the LIVE " +
			"schema on any database that has applied 000130 -- but 000004's own file text is " +
			"unchanged (already-applied migrations are never edited), so this scanner, which " +
			"reads file text rather than a live database, still finds and must still allowlist " +
			"this occurrence.",
	},
}

// ─── Real-repo test: no unallowlisted hazards, and no stale allowlist entries ───────────────

func migrationsDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "migrations")
}

func loadMigrationSQLFiles(t *testing.T) map[string]string {
	t.Helper()
	dir := migrationsDir(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read migrations dir %s: %v", dir, err)
	}
	files := map[string]string{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || (!strings.HasSuffix(name, ".up.sql") && !strings.HasSuffix(name, ".down.sql")) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("failed to read %s: %v", name, err)
		}
		files[name] = string(b)
	}
	if len(files) == 0 {
		t.Fatalf("no .up.sql/.down.sql files found under %s — migrationsDir() path is probably wrong", dir)
	}
	return files
}

type hazardKey struct {
	file, category, snippet string
}

// TestMigrationTZHazardScan_NoUnallowlistedHazards scans every migration file (up AND down) in
// backend/migrations/ for session-timezone-dependent SQL and fails if:
//   - any hazard occurrence is found that isn't on the allowlist (a NEW hazard — the real
//     regression this test exists to catch), or
//   - any allowlist entry's expected count doesn't match what's actually in the file (either a
//     hazard occurrence disappeared — a stale entry that must be deleted from the allowlist — or
//     there are now MORE occurrences than allowlisted, which is itself a new, unreviewed hazard).
func TestMigrationTZHazardScan_NoUnallowlistedHazards(t *testing.T) {
	files := loadMigrationSQLFiles(t)

	actual := map[hazardKey]int{}
	for file, content := range files {
		for _, h := range scanForTZHazards(content) {
			actual[hazardKey{file, h.category, h.snippet}]++
		}
	}

	expected := map[hazardKey]int{}
	for _, a := range tzHazardAllowlist {
		expected[hazardKey{a.file, a.category, a.snippet}] += a.count
	}

	allKeys := map[hazardKey]bool{}
	for k := range actual {
		allKeys[k] = true
	}
	for k := range expected {
		allKeys[k] = true
	}

	keys := make([]hazardKey, 0, len(allKeys))
	for k := range allKeys {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].file != keys[j].file {
			return keys[i].file < keys[j].file
		}
		if keys[i].category != keys[j].category {
			return keys[i].category < keys[j].category
		}
		return keys[i].snippet < keys[j].snippet
	})

	for _, k := range keys {
		gotN, wantN := actual[k], expected[k]
		if gotN == wantN {
			continue
		}
		switch {
		case wantN == 0:
			t.Errorf("UNALLOWLISTED TZ HAZARD in %s [%s]: %q (found %d occurrence(s)). "+
				"If this is a real hazard in new/changed SQL, fix it (pin AT TIME ZONE 'UTC' or "+
				"use clock_timestamp() without calendar-unit interval arithmetic). If it's a "+
				"false positive, or a pre-existing already-applied migration, add an explicit "+
				"allowlist entry to tzHazardAllowlist with a reason.",
				k.file, k.category, k.snippet, gotN)
		case gotN == 0:
			t.Errorf("STALE ALLOWLIST ENTRY for %s [%s]: %q — expected %d occurrence(s) but found "+
				"0. Delete this entry from tzHazardAllowlist; the hazard it documented no longer "+
				"exists in the file.", k.file, k.category, k.snippet, wantN)
		default:
			t.Errorf("ALLOWLIST COUNT MISMATCH for %s [%s]: %q — allowlisted count is %d, actual "+
				"count is %d. Update tzHazardAllowlist to match (and if the count went up, review "+
				"the new occurrence(s) — they may be a genuinely new hazard).",
				k.file, k.category, k.snippet, wantN, gotN)
		}
	}
}

// ─── Fixture-based falsifiability tests ──────────────────────────────────────
//
// The tests above only prove the scanner agrees with itself on the current state of the repo.
// These tests prove the scanner is actually detecting the right thing: each positive fixture is
// a minimal snippet that MUST be flagged, each negative fixture is a minimal snippet that MUST
// NOT be flagged. Mutating scanForTZHazards to break either direction should turn one of these
// red.

func TestScanForTZHazards_PositiveFixtures(t *testing.T) {
	cases := []struct {
		name     string
		sql      string
		category string
		count    int
	}{
		{
			name:     "now_minus_calendar_days",
			sql:      `UPDATE t SET x = NOW() - INTERVAL '5 days';`,
			category: "clock_func_calendar_interval",
			count:    1,
		},
		{
			name:     "calendar_interval_plus_now_reverse_order",
			sql:      `SELECT INTERVAL '3 days' + NOW();`,
			category: "clock_func_calendar_interval",
			count:    1,
		},
		{
			name:     "clock_timestamp_minus_calendar_weeks",
			sql:      `SELECT clock_timestamp() - INTERVAL '2 weeks';`,
			category: "clock_func_calendar_interval",
			count:    1,
		},
		{
			name:     "transaction_timestamp_plus_calendar_months",
			sql:      `SELECT transaction_timestamp() + INTERVAL '6 months';`,
			category: "clock_func_calendar_interval",
			count:    1,
		},
		{
			name:     "current_timestamp_minus_calendar_years",
			sql:      `SELECT CURRENT_TIMESTAMP - INTERVAL '1 years';`,
			category: "clock_func_calendar_interval",
			count:    1,
		},
		{
			name:     "current_date_literal",
			sql:      `INSERT INTO t (d) VALUES (CURRENT_DATE);`,
			category: "CURRENT_DATE",
			count:    1,
		},
		{
			name:     "current_timestamp_into_naked_timestamp_column",
			sql:      `CREATE TABLE t (created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP);`,
			category: "CURRENT_TIMESTAMP_untz_column",
			count:    1,
		},
		{
			name:     "calendar_interval_cast_form",
			sql:      `SELECT NOW() + '20 days'::interval;`,
			category: "clock_func_calendar_interval",
			count:    1,
		},
		{
			name:     "standalone_calendar_interval_cast_no_clock_func",
			sql:      `SELECT '6 months'::interval AS x;`,
			category: "calendar_interval_cast",
			count:    1,
		},
		{
			name:     "make_interval_months",
			sql:      `UPDATE t SET x = clock_timestamp() + make_interval(months => 6);`,
			category: "calendar_make_interval",
			count:    1,
		},
		{
			name:     "make_interval_days_mixed_with_hours",
			sql:      `SELECT make_interval(days => 1, hours => 2);`,
			category: "calendar_make_interval",
			count:    1,
		},
		{
			name:     "date_func_without_at_time_zone",
			sql:      `SELECT 1 WHERE DATE(created_at) = other_date;`,
			category: "date_func_untz",
			count:    1,
		},
		{
			name:     "bare_date_cast_without_at_time_zone",
			sql:      `SELECT created_at::date FROM t;`,
			category: "timestamptz_date_cast",
			count:    1,
		},
		{
			name:     "parenthesized_date_cast_without_at_time_zone",
			sql:      `SELECT (created_at)::date FROM t;`,
			category: "timestamptz_date_cast",
			count:    1,
		},
		{
			name:     "now_call_date_cast_without_at_time_zone",
			sql:      `SELECT now()::date;`,
			category: "timestamptz_date_cast",
			count:    1,
		},
		{
			name:     "date_trunc_two_arg_no_tz",
			sql:      `SELECT date_trunc('month', created_at);`,
			category: "date_trunc_no_tz",
			count:    1,
		},
		{
			name:     "duplicate_date_calls_two_distinct_occurrences",
			sql:      `SELECT 1 WHERE DATE(a.ts) = DATE(b.ts);`,
			category: "date_func_untz",
			count:    2, // two distinct snippets, both category date_func_untz
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hazards := scanForTZHazards(tc.sql)
			n := 0
			for _, h := range hazards {
				if h.category == tc.category {
					n++
				}
			}
			if n != tc.count {
				t.Errorf("scanForTZHazards(%q) found %d hazard(s) of category %q, want %d. All hazards: %+v",
					tc.sql, n, tc.category, tc.count, hazards)
			}
		})
	}
}

func TestScanForTZHazards_NegativeFixtures(t *testing.T) {
	cases := []struct {
		name string
		sql  string
	}{
		{
			name: "now_plus_fixed_duration_hours",
			sql:  `SELECT NOW() + INTERVAL '72 hours';`,
		},
		{
			name: "now_minus_fixed_duration_seconds",
			sql:  `SELECT NOW() - INTERVAL '30 seconds';`,
		},
		{
			name: "now_minus_fixed_duration_minutes",
			sql:  `SELECT clock_timestamp() - INTERVAL '5 minutes';`,
		},
		{
			name: "bare_now_no_interval",
			sql:  `SELECT NOW();`,
		},
		{
			name: "bare_clock_timestamp_no_interval",
			sql:  `UPDATE t SET x = clock_timestamp();`,
		},
		{
			name: "make_interval_hours_only",
			sql:  `SELECT clock_timestamp() + make_interval(hours => 72);`,
		},
		{
			name: "make_interval_secs_only",
			sql:  `SELECT clock_timestamp() - make_interval(secs => 30);`,
		},
		{
			name: "current_timestamp_into_timestamptz_column",
			sql:  `CREATE TABLE t (created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP);`,
		},
		{
			name: "current_timestamp_into_timestamp_with_time_zone_column",
			sql:  `CREATE TABLE t (created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP);`,
		},
		{
			name: "date_func_with_at_time_zone",
			sql:  `SELECT DATE(created_at AT TIME ZONE 'UTC');`,
		},
		{
			name: "date_cast_with_at_time_zone",
			sql:  `SELECT (created_at AT TIME ZONE 'UTC')::date;`,
		},
		{
			name: "date_trunc_with_explicit_tz",
			sql:  `SELECT date_trunc('month', created_at, 'UTC');`,
		},
		{
			name: "typed_parameter_date_cast",
			sql:  `SELECT * FROM t WHERE d = $1::date;`,
		},
		{
			name: "literal_date_cast",
			sql:  `SELECT * FROM t WHERE d = '2024-01-01'::date;`,
		},
		{
			name: "hazard_text_inside_line_comment",
			sql: "-- Example: NOW() - INTERVAL '5 days' would be wrong here, see docs.\n" +
				"-- CURRENT_DATE is also mentioned only in prose, not executed.\n" +
				"SELECT 1;",
		},
		{
			name: "hazard_text_inside_block_comment",
			sql:  "/* NOW() - INTERVAL '10 days' and CURRENT_DATE, both just discussed here. */\nSELECT 1;",
		},
		{
			name: "hazard_text_inside_string_literal",
			sql:  `COMMENT ON COLUMN t.x IS 'Always pass DATE(now() AT TIME ZONE ''UTC'') from application code, never bare CURRENT_DATE.';`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hazards := scanForTZHazards(tc.sql)
			if len(hazards) != 0 {
				t.Errorf("scanForTZHazards(%q) found %d hazard(s), want 0: %+v", tc.sql, len(hazards), hazards)
			}
		})
	}
}

// TestStripSQLComments_PreservesStringLiterals is a focused unit test for the comment stripper
// itself, independent of hazard detection: a naive stripper that doesn't track string-literal
// state would truncate the statement below at the "--" inside the quoted string.
func TestStripSQLComments_PreservesStringLiterals(t *testing.T) {
	in := `SELECT 'a--b/*c*/d' AS x; -- trailing comment
SELECT 2;`
	out := stripSQLComments(in)
	if !strings.Contains(out, `'a--b/*c*/d'`) {
		t.Fatalf("stripSQLComments mangled a string literal containing comment-like text: %q", out)
	}
	if strings.Contains(out, "trailing comment") {
		t.Fatalf("stripSQLComments failed to strip a real line comment: %q", out)
	}
	if !strings.Contains(out, "SELECT 2;") {
		t.Fatalf("stripSQLComments dropped content after the comment: %q", out)
	}
}
