package repository

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// #1631 gap D1. A CONTRADICTION between a helper's guarantee and ~45 field doc strings.
//
// domain.formatDateOnly renders a time.Time in the value's OWN location and says so, for a good
// reason it also states: pgx hands a DATE back as midnight in some location, and converting
// midnight-in-a-positive-offset to UTC rolls the calendar day backwards — the very shift the
// display-sibling pattern exists to prevent. Meanwhile every date display field in internal/domain
// documents its output as the "UTC calendar day (YYYY-MM-DD)".
//
// Both can only be true when the value handed to the formatter ALREADY names the intended day in
// its own location. For a pgx-scanned DATE that holds: pgx decodes DATE as midnight in time.UTC.
// For a COMPUTED value it does not — a range echo, a clamp, a caller-supplied RFC3339 offset — and
// the promise silently becomes a lie in exactly the population that has no column behind it.
//
// The fix is to make the GUARANTEE true rather than to weaken 45 accurate doc strings: every
// computed producing site reduces its instant to the UTC calendar day first, via
// domain.FormatCalendarDayUTC (budget_service.go's budgetCalendarDayUTC was the precedent, and now
// delegates to the same primitive).
//
// This guard is what keeps that from regressing. A bare `x.Format(time.DateOnly)` anywhere under
// internal/ formats in x's own location, so every such site must either be routed through
// domain.FormatCalendarDayUTC (which does not match this pattern at all) or be allowlisted HERE
// with the provenance that makes its own location the right one.
//
// ⚠ What this guard does NOT check, so nobody reads more into a green run than is here:
//   - An allowlist entry's REASON is prose. Nothing compares it to the code. Re-derive a site's
//     provenance before trusting one.
//   - It matches on the SYNTAX `<expr>.Format(time.DateOnly)`. A value formatted with a
//     hand-written "2006-01-02" layout, or via a variable holding that layout, is invisible.
//     domain.formatDateOnly is the one such site and is allowlisted below.
//   - It says nothing about whether a value is the RIGHT day — only about which location decides
//     the day.
//   - It only sees the FORMATTER. A caller-supplied instant assigned onto a date field and then
//     rendered by a blessed Populate*Display method is invisible here — that was the live
//     savings_goal_service.UpdateGoal defect, and
//     TestNoFlexibleDateReachesADateFieldUnnormalised below is its guard. Neither subsumes the
//     other.
func TestNoComputedDateDisplayFormatsInItsOwnLocation(t *testing.T) {
	violations, matches := findOwnLocationDateFormats(t, dateOnlyOwnLocationAllowlist())

	// A zero match count means the scan broke (a moved tree, a renamed constant), and a guard that
	// measures nothing reads as a pass forever.
	if matches == 0 {
		t.Fatal("found no `x.Format(time.DateOnly)` calls under internal/ at all. Either the " +
			"pattern changed or this scan is broken; refusing to pass without checking anything.")
	}
	if len(violations) > 0 {
		t.Fatalf("%d site(s) format a date in the value's OWN location while the field they feed "+
			"promises the UTC calendar day.\n\nRoute the value through domain.FormatCalendarDayUTC, "+
			"or add it to dateOnlyOwnLocationAllowlist with the provenance that makes its own "+
			"location correct:\n  %s", len(violations), strings.Join(violations, "\n  "))
	}
}

// dateOnlyOwnLocationAllowlist maps a file (relative to backend/internal) to the receiver
// expressions inside it that may format in their own location, each with the provenance that makes
// that correct. Keyed by expression TEXT rather than line number so the entries survive edits
// above them.
func dateOnlyOwnLocationAllowlist() map[string]map[string]string {
	return map[string]map[string]string{
		"domain/date_display.go": {
			// The primitive itself. It must NOT call .UTC() — that is the day-roll its own doc
			// comment warns about — so the location discipline lives in its callers, and
			// FormatCalendarDayUTC is the wrapper computed callers use.
			"t": "domain.formatDateOnly: the primitive whose contract this guard enforces on callers.",
		},
		"repository/transaction_repository.go": {
			// V:2 keyset cursor sort values, not display fields. The cursor's job is to reproduce
			// the SQL ORDER BY's own comparison, and the values compared are pgx-scanned DATEs
			// (transactions.date / authorized_date, migration 000010) and the unified feed's
			// COALESCE(authorized_date, date)::date — all midnight UTC out of pgx. Normalising
			// here would be a no-op that implied the cursor was a display surface.
			"effDate":           "keyset cursor sort value over a pgx-scanned DATE (already UTC).",
			"tx.Date":           "keyset cursor sort value over transactions.date, a pgx-scanned DATE.",
			"tx.AuthorizedDate": "keyset cursor sort value over transactions.authorized_date, a pgx-scanned DATE.",
			"last.SortDate":     "keyset cursor sort value over COALESCE(authorized_date, date)::date, pgx-scanned.",
		},
		"services/dashboard_service.go": {
			// BalanceHistoryPoint.Date is scanned from `(snapshot_at AT TIME ZONE 'UTC')::date`
			// (balance_snapshot_repository.go), so pgx hands it back as midnight UTC already.
			"point.Date": "balance_snapshots snapshot_date, scanned as (snapshot_at AT TIME ZONE 'UTC')::date.",
		},
		"services/rule_preview_service.go": {
			"tx.Date": "transactions.date, a pgx-scanned DATE on a read-only preview over persisted rows.",
		},
	}
}

// findOwnLocationDateFormats returns one description per unallowlisted `x.Format(time.DateOnly)`
// site, plus the total number of matching sites seen (allowlisted or not).
func findOwnLocationDateFormats(t *testing.T, allow map[string]map[string]string) ([]string, int) {
	t.Helper()
	return scanOwnLocationDateFormats(goProductionFiles(t, repoInternalRoot(t), true, false), allow)
}

// scanOwnLocationDateFormats is the matching half, taking an already-parsed corpus so the guard's
// own meta-test can drive it over synthetic source instead of the tree.
func scanOwnLocationDateFormats(files []goProductionFile, allow map[string]map[string]string) ([]string, int) {
	var violations []string
	matches := 0
	used := map[string]bool{}

	for _, f := range files {
		ast.Inspect(f.file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 1 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Format" || !isTimeDateOnly(call.Args[0]) {
				return true
			}
			matches++
			recv := exprText(f.fset, sel.X)
			if _, ok := allow[f.rel][recv]; ok {
				used[f.rel+"|"+recv] = true
				return true
			}
			pos := f.fset.Position(call.Pos())
			violations = append(violations, f.rel+":"+strconv.Itoa(pos.Line)+": "+recv+".Format(time.DateOnly)")
			return true
		})
	}

	// A stale allowlist entry is the failure mode this package's other allowlists all have: it
	// reads as a considered exemption and covers nothing, so the next reader trusts a claim about
	// code that has moved or gone. Reported as a violation in its own right.
	for file, byExpr := range allow {
		for expr := range byExpr {
			if !used[file+"|"+expr] {
				violations = append(violations, file+": allowlist entry "+expr+
					" matches no `.Format(time.DateOnly)` site — stale, remove it")
			}
		}
	}

	sort.Strings(violations)
	return violations, matches
}

// isTimeDateOnly reports whether e is the selector `time.DateOnly`.
func isTimeDateOnly(e ast.Expr) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "DateOnly" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "time"
}

func exprText(fset *token.FileSet, e ast.Expr) string {
	var b strings.Builder
	if err := printer.Fprint(&b, fset, e); err != nil {
		return "<unprintable>"
	}
	return b.String()
}

// The guard's own meta-test: it constructs the violation the guard exists to catch and proves the
// guard fails on it, then proves the allowlist is a per-site exemption rather than a blanket pass.
// Without this, a refactor that quietly stopped matching anything would look identical to a clean
// tree.
func TestDateDisplayLocationGuardCatchesTheViolationItDescribes(t *testing.T) {
	cases := []struct {
		name       string
		src        string
		allow      map[string]map[string]string
		wantIssues int
		wantMatch  int
	}{
		{
			name: "a bare .Format(time.DateOnly) on a computed value is caught",
			src: `package svc

func render(resp *R, filters F) { resp.StartDateDisplay = filters.StartDate.Format(time.DateOnly) }
`,
			wantIssues: 1,
			wantMatch:  1,
		},
		{
			name: "routing through domain.FormatCalendarDayUTC is clean",
			src: `package svc

func render(resp *R, filters F) { resp.StartDateDisplay = domain.FormatCalendarDayUTC(filters.StartDate) }
`,
			wantIssues: 0,
			wantMatch:  0,
		},
		{
			name: "an allowlisted receiver expression in that file is exempt",
			src: `package svc

func render(resp *R, p P) { resp.DateDisplay = p.Date.Format(time.DateOnly) }
`,
			allow:      map[string]map[string]string{"svc/x.go": {"p.Date": "pgx-scanned DATE."}},
			wantIssues: 0,
			wantMatch:  1,
		},
		{
			name: "an allowlist entry for a DIFFERENT expression in the same file does not exempt this one",
			src: `package svc

func render(resp *R, p P) { resp.DateDisplay = p.Other.Format(time.DateOnly) }
`,
			// Two violations, not one: the unexempted site, plus the now-stale entry itself.
			allow:      map[string]map[string]string{"svc/x.go": {"p.Date": "pgx-scanned DATE."}},
			wantIssues: 2,
			wantMatch:  1,
		},
		{
			name: "an allowlist entry that covers nothing is itself a violation",
			src: `package svc

func render(resp *R) { resp.DateDisplay = "" }
`,
			allow:      map[string]map[string]string{"svc/x.go": {"p.Date": "pgx-scanned DATE."}},
			wantIssues: 1,
			wantMatch:  0,
		},
		{
			name: "a Format call with a different layout is not this guard's business",
			src: `package svc

func render(resp *R, p P) { resp.DateDisplay = p.Date.Format(time.RFC3339) }
`,
			wantIssues: 0,
			wantMatch:  0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "x.go", tc.src, 0)
			if err != nil {
				t.Fatalf("parsing fixture: %v", err)
			}
			corpus := []goProductionFile{{rel: "svc/x.go", fset: fset, file: file}}
			violations, matches := scanOwnLocationDateFormats(corpus, tc.allow)
			if len(violations) != tc.wantIssues {
				t.Fatalf("got %d violation(s) %v, want %d", len(violations), violations, tc.wantIssues)
			}
			if matches != tc.wantMatch {
				t.Fatalf("got %d match(es), want %d", matches, tc.wantMatch)
			}
		})
	}
}

// TestNoFlexibleDateReachesADateFieldUnnormalised closes the hole the guard above cannot see, and
// the hole was real: savings_goal_service.UpdateGoal shipped `goal.Deadline =
// (*time.Time)(req.Deadline)` followed by goal.PopulateDateDisplay(), which routes through the
// BLESSED formatter — so nothing above flags it — while handing that formatter a value whose
// location the CALLER chose.
//
// domain.FlexibleDate is the only source of that shape in this tree. It accepts a full RFC3339
// timestamp, so a request may name a day in any offset on earth, and the fields it feeds are DATE
// columns whose display siblings promise the UTC calendar day. Every value leaving a FlexibleDate
// for such a field must therefore pass through domain.CalendarDayUTC (or its budget-local alias)
// first.
//
// ⚠ It does NOT check the reverse direction, or values that reach a date field from anywhere other
// than a FlexibleDate. Those have no caller-chosen offset to go wrong.
func TestNoFlexibleDateReachesADateFieldUnnormalised(t *testing.T) {
	// Sites where a FlexibleDate value legitimately keeps the caller's own instant, with the reason.
	allow := map[string]map[string]string{
		"services/category_preview_service.go": {
			"req.StartDate.Time": "a query RANGE bound, not a date field with a display sibling — and " +
				"the end bound deliberately uses EndOfDay() to respect an instant the caller chose.",
			"req.EndDate.Time":     "same: a query range bound, never serialized as a calendar day.",
			"req.EndDate.EndOfDay": "same, and EndOfDay exists precisely to preserve a caller's instant.",
		},
	}

	normalised := map[token.Pos]bool{}
	var candidates []struct {
		rel, expr string
		pos       token.Pos
		line      int
	}

	for _, f := range goProductionFiles(t, repoInternalRoot(t), true, false) {
		if f.rel == "domain/flexible_date.go" {
			continue // the type's own methods, which define the conversions rather than consume them
		}
		ast.Inspect(f.file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 1 {
				return true
			}
			if isCalendarDayUTCCall(call.Fun) {
				normalised[call.Args[0].Pos()] = true
				return true
			}
			if !isTimeTimeConversion(call.Fun) && !isFlexibleDateAccessor(f.fset, call.Fun) {
				return true
			}
			candidates = append(candidates, struct {
				rel, expr string
				pos       token.Pos
				line      int
			}{f.rel, exprText(f.fset, call.Fun), call.Pos(), f.fset.Position(call.Pos()).Line})
			return true
		})
	}

	if len(candidates) == 0 {
		t.Fatal("found no FlexibleDate conversions under internal/ at all. Either the type was " +
			"renamed or this scan is broken; refusing to pass without checking anything.")
	}

	var violations []string
	for _, c := range candidates {
		if normalised[c.pos] {
			continue
		}
		if _, ok := allow[c.rel][c.expr]; ok {
			continue
		}
		violations = append(violations, c.rel+":"+strconv.Itoa(c.line)+": "+c.expr+"(...)")
	}
	sort.Strings(violations)
	if len(violations) > 0 {
		t.Fatalf("%d FlexibleDate value(s) reach a date field without being reduced to the UTC "+
			"calendar day.\n\nWrap in domain.CalendarDayUTC, or allowlist with the reason the "+
			"caller's own instant is correct there:\n  %s", len(violations), strings.Join(violations, "\n  "))
	}
}

// isCalendarDayUTCCall recognises domain.CalendarDayUTC / CalendarDayUTC / FormatCalendarDayUTC and
// budget_service.go's budgetCalendarDayUTC alias, which delegates to the same primitive.
func isCalendarDayUTCCall(fun ast.Expr) bool {
	name := ""
	switch f := fun.(type) {
	case *ast.Ident:
		name = f.Name
	case *ast.SelectorExpr:
		name = f.Sel.Name
	default:
		return false
	}
	switch name {
	case "CalendarDayUTC", "FormatCalendarDayUTC", "budgetCalendarDayUTC":
		return true
	}
	return false
}

// isTimeTimeConversion recognises `time.Time(x)` and `(*time.Time)(x)`.
func isTimeTimeConversion(fun ast.Expr) bool {
	if sel, ok := fun.(*ast.SelectorExpr); ok {
		pkg, isIdent := sel.X.(*ast.Ident)
		return isIdent && pkg.Name == "time" && sel.Sel.Name == "Time"
	}
	paren, ok := fun.(*ast.ParenExpr)
	if !ok {
		return false
	}
	star, ok := paren.X.(*ast.StarExpr)
	if !ok {
		return false
	}
	return isTimeTimeConversion(star.X)
}

// isFlexibleDateAccessor recognises the two methods that hand a FlexibleDate's instant out,
// matched by name on a receiver whose text begins with `req.` — the only shape a request-bound
// FlexibleDate takes in this tree.
func isFlexibleDateAccessor(fset *token.FileSet, fun ast.Expr) bool {
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok || (sel.Sel.Name != "Time" && sel.Sel.Name != "EndOfDay") {
		return false
	}
	return strings.HasPrefix(exprText(fset, sel.X), "req.")
}
