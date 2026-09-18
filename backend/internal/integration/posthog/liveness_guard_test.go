package posthog

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// Issue #1652 Todo 13 (iv) — the liveness guard.
//
// WHAT IT ASSERTS. Every catalog event with `status: active` or `status: legacy` must have at least
// one PRODUCTION emitter in the Go tree (`backend/**/*.go`) or the TypeScript tree
// (`apps/web/src/**/*.{ts,tsx}`). A declared-but-unemitted event is a dead contract: dashboards and
// funnels built on it silently read zero, and nothing else in the suite notices.
//
// THE TWO EXCLUSIONS THAT MAKE IT MEAN ANYTHING.
//
//  1. The declarations themselves (`events_generated.go`, `events.generated.ts`,
//     `config/posthog_events.yaml`) are excluded. They name every event by construction, so counting
//     them would mark every event live and prove nothing. `TestLivenessScanDoesNotCountGeneratedDeclarations`
//     constructs the exclusion and proves it.
//  2. Test files (`*_test.go`, `*.test.ts`, `*.test.tsx`) are excluded: this guard counts PRODUCTION
//     emitters only. A test that references a constant is not a call site that ships; treating it as
//     one would let a deleted emitter keep the contract "live" as long as a test still mentions it.
//     This is the recorded decision the plan asks for.
//
// `status: deprecated` and `status: future` are excluded structurally: they are not expected to
// emit. `mcp_worker` producers are Go-tree symbols too, so they are covered by the same Go scan.
//
// WHAT IS NOT COVERED. An event whose emitter exists but never runs (a dead branch) still counts as
// live — this guard proves a call site exists, not that it is reachable. That is the same boundary
// `validate-posthog-rollout.ts` stops at; the per-path tests in todos 6-12 cover reachability.

type catalogEventEntry struct {
	ContractID string   `yaml:"contract_id"`
	Name       string   `yaml:"name"`
	Status     string   `yaml:"status"`
	Producers  []string `yaml:"producers"`
	Symbols    struct {
		TypeScript []string `yaml:"typescript"`
		Go         []string `yaml:"go"`
	} `yaml:"symbols"`
}

type catalogDocument struct {
	Events []catalogEventEntry `yaml:"events"`
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller failed")
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..")
}

func parseCatalog(t *testing.T) []catalogEventEntry {
	t.Helper()
	path := filepath.Join(repoRoot(t), "config", "posthog_events.yaml")
	source, err := os.ReadFile(path)
	require.NoError(t, err)
	var document catalogDocument
	require.NoError(t, yaml.Unmarshal(source, &document))
	require.NotEmpty(t, document.Events, "catalog parsed empty — the schema probably drifted")
	return document.Events
}

// goPayloadTypeName maps a generated Go event symbol to its generated payload struct name:
// `EventFoo` -> `FooPayload`. Events without properties generate no payload type, so the symbol
// itself is the only emitter form for them; both are searched.
func goPayloadTypeName(symbol string) string {
	if !strings.HasPrefix(symbol, "Event") {
		return ""
	}
	return strings.TrimPrefix(symbol, "Event") + "Payload"
}

func livenessSymbols(events []catalogEventEntry) (goSymbols, tsSymbols []string) {
	goSet := map[string]bool{}
	tsSet := map[string]bool{}
	for _, event := range events {
		for _, symbol := range event.Symbols.Go {
			goSet[symbol] = true
			if payload := goPayloadTypeName(symbol); payload != "" {
				goSet[payload] = true
			}
		}
		for _, symbol := range event.Symbols.TypeScript {
			tsSet[symbol] = true
		}
	}
	return sortedKeys(goSet), sortedKeys(tsSet)
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// livenessViolations is the guard core: the sorted names of active/legacy events with no production
// emitter and no allowlist entry. It is exported to the tests only (lowercase) so the meta-tests can
// drive it with fabricated catalogs and source maps.
func livenessViolations(events []catalogEventEntry, goEmitters, tsEmitters map[string]string, allowlist map[string]string) []string {
	var violations []string
	for _, event := range events {
		if event.Status != "active" && event.Status != "legacy" {
			continue
		}
		if _, allowed := allowlist[event.Name]; allowed {
			continue
		}
		if eventIsLive(event, goEmitters, tsEmitters) {
			continue
		}
		violations = append(violations, event.Name)
	}
	sort.Strings(violations)
	return violations
}

func eventIsLive(event catalogEventEntry, goEmitters, tsEmitters map[string]string) bool {
	producesBackend := containsString(event.Producers, "backend") || containsString(event.Producers, "mcp_worker")
	producesFrontend := containsString(event.Producers, "frontend")
	if producesBackend {
		for _, symbol := range event.Symbols.Go {
			if _, ok := goEmitters[symbol]; ok {
				return true
			}
			if payload := goPayloadTypeName(symbol); payload != "" {
				if _, ok := goEmitters[payload]; ok {
					return true
				}
			}
		}
	}
	if producesFrontend {
		for _, symbol := range event.Symbols.TypeScript {
			if _, ok := tsEmitters[symbol]; ok {
				return true
			}
		}
	}
	return false
}

type emitterEligibility func(path string, entry fs.DirEntry) bool

func isGoEmitterFile(path string, entry fs.DirEntry) bool {
	if entry.IsDir() || !strings.HasSuffix(path, ".go") {
		return false
	}
	if strings.HasSuffix(path, "_test.go") {
		return false
	}
	return filepath.Base(path) != "events_generated.go"
}

func isTypeScriptEmitterFile(path string, entry fs.DirEntry) bool {
	if entry.IsDir() {
		return false
	}
	if !strings.HasSuffix(path, ".ts") && !strings.HasSuffix(path, ".tsx") {
		return false
	}
	if strings.Contains(filepath.Base(path), ".test.") {
		return false
	}
	return filepath.Base(path) != "events.generated.ts"
}

// scanEmitterFiles walks root and returns symbol -> first production file that references it. Only
// files satisfying eligible are read; the declaration and test files are filtered out by the two
// eligibility predicates above, which the exclusion meta-test drives directly.
func scanEmitterFiles(root string, symbols []string, eligible emitterEligibility) (map[string]string, error) {
	patterns := make(map[string]*regexp.Regexp, len(symbols))
	for _, symbol := range symbols {
		patterns[symbol] = regexp.MustCompile(`\b` + regexp.QuoteMeta(symbol) + `\b`)
	}
	found := make(map[string]string)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case "node_modules", ".git", "dist", "build", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if !eligible(path, entry) {
			return nil
		}
		source, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		content := string(source)
		for _, symbol := range symbols {
			if _, already := found[symbol]; already {
				continue
			}
			if !strings.Contains(content, symbol) {
				continue
			}
			if patterns[symbol].MatchString(content) {
				found[symbol] = path
			}
		}
		return nil
	})
	return found, err
}

func goEmitterFiles(root string, symbols []string) (map[string]string, error) {
	return scanEmitterFiles(root, symbols, isGoEmitterFile)
}

func typeScriptEmitterFiles(root string, symbols []string) (map[string]string, error) {
	return scanEmitterFiles(root, symbols, isTypeScriptEmitterFile)
}

// TestEveryCatalogEventHasALiveEmitter is the guard. It parses the real catalog and scans the real
// Go and TS trees; a new orphan (or an emitter deleted from an event not on the allowlist) fails it.
func TestEveryCatalogEventHasALiveEmitter(t *testing.T) {
	root := repoRoot(t)
	events := parseCatalog(t)
	goSymbols, tsSymbols := livenessSymbols(events)

	goEmitters, err := goEmitterFiles(filepath.Join(root, "backend"), goSymbols)
	require.NoError(t, err)
	tsEmitters, err := typeScriptEmitterFiles(filepath.Join(root, "apps", "web", "src"), tsSymbols)
	require.NoError(t, err)

	violations := livenessViolations(events, goEmitters, tsEmitters, livenessAllowlist)
	assert.Empty(t, violations,
		"catalog events with no production emitter and no allowlist entry (add an emitter, or allowlist with a reason)")
}

// livenessAllowlist pins the catalog entries with no production emitter TODAY. Every entry is a
// PRE-EXISTING pre-#1650 orphan — none is introduced by #1652, and none is emitted by any Go or TS
// production file. The guard's value is that a NEW orphan, or an emitter deleted from an event not
// listed here, still fails. Do not add an entry for a new event; give it an emitter.
var livenessAllowlist = map[string]string{
	// The analytics-*_viewed family was declared for the pre-#1650 analytics surface that the
	// insights surface replaced; no production TS reference remains for any of them.
	"analytics_financial_summary_viewed":     "pre-#1650 analytics surface removed; superseded by insights_page_viewed",
	"analytics_savings_rate_viewed":          "pre-#1650 analytics surface removed; superseded by insights_page_viewed",
	"analytics_mom_change_viewed":            "pre-#1650 analytics surface removed; superseded by insights_page_viewed",
	"analytics_recurring_subs_viewed":        "pre-#1650 analytics surface removed; superseded by insights_page_viewed",
	"analytics_unusual_spend_viewed":         "pre-#1650 analytics surface removed; superseded by insights_page_viewed",
	"analytics_budget_vs_actual_viewed":      "pre-#1650 analytics surface removed; superseded by insights_page_viewed",
	"analytics_net_worth_trend_viewed":       "pre-#1650 analytics surface removed; superseded by insights_page_viewed",
	"analytics_income_sources_viewed":        "pre-#1650 analytics surface removed; superseded by insights_page_viewed",
	"analytics_largest_transactions_viewed":  "pre-#1650 analytics surface removed; superseded by insights_page_viewed",
	"analytics_average_monthly_spend_viewed": "pre-#1650 analytics surface removed; superseded by insights_page_viewed",
	"budget_edited":                          "pre-#1650 orphan; the edit surface now emits budget_category_updated instead",
	"budget_sub_allocation_added":            "pre-#1650 orphan; no production TS reference remains",
	"custom_category_manager_opened":         "pre-#1650 orphan; no production TS reference remains",
	"datatable_column_pinned":                "pre-#1650 orphan; no production TS reference remains",
	"transfer_suggestion_match_declined":     "pre-#1650 orphan; no production TS reference remains",
	"invalid_signature_webhook_rejected":     "pre-#1650 backend orphan; the ingress guard it documents was never wired to emit it",
}

// TestLivenessGuardDetectsOrphanedEvent is the meta-test: a fabricated active event with a symbol
// but no emitter must be reported, proving the guard fails when an emitter is deleted.
func TestLivenessGuardDetectsOrphanedEvent(t *testing.T) {
	orphan := catalogEventEntry{Name: "orphaned_fixture_event", Status: "active", Producers: []string{"backend"}}
	orphan.Symbols.Go = []string{"EventOrphanedFixture"}

	violations := livenessViolations([]catalogEventEntry{orphan}, map[string]string{}, map[string]string{}, map[string]string{})
	assert.Equal(t, []string{"orphaned_fixture_event"}, violations)

	// The same event is live once an emitter exists — so the guard is not vacuously failing.
	live := livenessViolations([]catalogEventEntry{orphan},
		map[string]string{"EventOrphanedFixture": "internal/services/orphan.go"}, map[string]string{}, map[string]string{})
	assert.Empty(t, live)

	// A typed-payload emitter (no raw constant reference) also counts.
	typed := livenessViolations([]catalogEventEntry{orphan},
		map[string]string{"OrphanedFixturePayload": "internal/services/orphan.go"}, map[string]string{}, map[string]string{})
	assert.Empty(t, typed)

	// The allowlist suppresses it, and only with an entry.
	allowed := livenessViolations([]catalogEventEntry{orphan}, map[string]string{}, map[string]string{},
		map[string]string{"orphaned_fixture_event": "deliberate pre-existing orphan"})
	assert.Empty(t, allowed)
}

// TestLivenessGuardIgnoresDeprecatedAndFutureStatuses proves the structural status exclusion.
func TestLivenessGuardIgnoresDeprecatedAndFutureStatuses(t *testing.T) {
	for _, status := range []string{"deprecated", "future"} {
		event := catalogEventEntry{Name: "retired_fixture_event", Status: status, Producers: []string{"backend"}}
		event.Symbols.Go = []string{"EventRetiredFixture"}
		assert.Empty(t, livenessViolations([]catalogEventEntry{event}, map[string]string{}, map[string]string{}, map[string]string{}), status)
	}
}

// TestLivenessScanDoesNotCountGeneratedDeclarations proves exclusion (1) above: a tree containing
// ONLY the generated declaration must not report the event live, while the same content in a
// production file must.
func TestLivenessScanDoesNotCountGeneratedDeclarations(t *testing.T) {
	dir := t.TempDir()
	declaration := filepath.Join(dir, "events_generated.go")
	require.NoError(t, os.WriteFile(declaration, []byte("package posthog\n\nconst EventFixture = \"fixture\"\n"), 0o644))

	found, err := goEmitterFiles(dir, []string{"EventFixture"})
	require.NoError(t, err)
	assert.Empty(t, found, "the generated declaration must not count as an emitter")

	tsDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tsDir, "events.generated.ts"), []byte("export const FIXTURE = 'fixture'\n"), 0o644))
	tsFound, err := typeScriptEmitterFiles(tsDir, []string{"FIXTURE"})
	require.NoError(t, err)
	assert.Empty(t, tsFound, "the generated TS declaration must not count as an emitter")

	production := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(production, "fixture.go"), []byte("package services\n\nfunc f() { _ = EventFixture }\n"), 0o644))
	found, err = goEmitterFiles(production, []string{"EventFixture"})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"EventFixture": filepath.Join(production, "fixture.go")}, found,
		"a production reference must count — proving the exclusion, not a broken scan, is what suppressed the declaration")
}

// TestLivenessScanIgnoresTestFiles proves exclusion (2): a test-only reference is not an emitter.
func TestLivenessScanIgnoresTestFiles(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "fixture_test.go"), []byte("package services\n\nfunc TestX() { _ = EventFixture }\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "fixture.test.ts"), []byte("export const x = FIXTURE\n"), 0o644))

	goFound, err := goEmitterFiles(dir, []string{"EventFixture"})
	require.NoError(t, err)
	assert.Empty(t, goFound)

	tsFound, err := typeScriptEmitterFiles(dir, []string{"FIXTURE"})
	require.NoError(t, err)
	assert.Empty(t, tsFound)
}

// TestLivenessGuardFailsWhenARealEmitterIsDeleted is the real-tree red proof: it takes a real,
// currently-live event out of the real catalog, confirms the real scan marks it live, deletes its
// emitter from the scanned map, and confirms the guard then reports it. This is the mutation the
// guard exists to catch — a production emitter removed without deleting the contract.
func TestLivenessGuardFailsWhenARealEmitterIsDeleted(t *testing.T) {
	root := repoRoot(t)
	events := parseCatalog(t)
	goSymbols, tsSymbols := livenessSymbols(events)
	goEmitters, err := goEmitterFiles(filepath.Join(root, "backend"), goSymbols)
	require.NoError(t, err)
	tsEmitters, err := typeScriptEmitterFiles(filepath.Join(root, "apps", "web", "src"), tsSymbols)
	require.NoError(t, err)

	target := findEvent(t, events, "revenuecat_access_inactive")
	require.True(t, eventIsLive(target, goEmitters, tsEmitters),
		"precondition: the real tree must currently emit this event")

	for _, symbol := range target.Symbols.Go {
		delete(goEmitters, symbol)
		delete(goEmitters, goPayloadTypeName(symbol))
	}
	assert.False(t, eventIsLive(target, goEmitters, tsEmitters))
	assert.Equal(t, []string{"revenuecat_access_inactive"},
		livenessViolations([]catalogEventEntry{target}, goEmitters, tsEmitters, livenessAllowlist))
}

func findEvent(t *testing.T, events []catalogEventEntry, name string) catalogEventEntry {
	t.Helper()
	for _, event := range events {
		if event.Name == name {
			return event
		}
	}
	t.Fatalf("catalog event %q not found", name)
	return catalogEventEntry{}
}
