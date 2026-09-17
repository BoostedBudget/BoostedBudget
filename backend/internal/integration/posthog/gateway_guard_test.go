package posthog

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProductionWiringNeverInjectsRawPostHogClient(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "..", "cmd", "server", "main.go"))
	require.NoError(t, err)
	assert.NotContains(t, string(source), "SetAnalytics(phClient)")
	assert.NotContains(t, string(source), "NewGDPRClient(phClient")
	assert.Contains(t, string(source), "SetTransactionSyncCompletionRecorder(transactionSyncService, linkedItemRepo)")
}

func TestOnlyClientAdapterConstructsPostHogSDKMessages(t *testing.T) {
	backendRoot := filepath.Join("..", "..", "..")
	err := filepath.WalkDir(backendRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return walkErr
		}
		if filepath.Base(path) == "client.go" && filepath.Base(filepath.Dir(path)) == "posthog" {
			return nil
		}
		source, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		assert.NotContains(t, string(source), "github.com/posthog/posthog-go", path)
		assert.NotContains(t, string(source), "posthoggo.Capture", path)
		return nil
	})
	require.NoError(t, err)
}

func TestLegacyRawCaptureProducersArePinned(t *testing.T) {
	// Task #1650 grandfathered these legacy producers while forcing every new producer through
	// ProductAnalytics.CaptureEvent and a generated payload. A path missing from this map, or an
	// extra Capture call in an existing path, is a migration regression that needs an explicit
	// contract review rather than silently expanding the raw API surface.
	want := map[string][]string{
		"internal/huma/operations/transfer_suggestion.go":          {"Capture:phclient.EventTransferSuggestionValidated", "Capture:phclient.EventTransferSuggestionValidated"},
		"internal/queue/jobs/email_change_propagation.go":          {"Capture:phclient.EventEmailChanged"},
		"internal/queue/jobs/subscription_webhook.go":              {"Capture:phclient.EventHouseholdOverCapacityEntered", "Capture:phclient.EventPaymentFailed", "Capture:phclient.EventPaymentSucceeded", "Capture:phclient.EventSubscriptionCancelled", "Capture:phclient.EventSubscriptionCreated"},
		"internal/services/boost_execution_service.go":             {"Capture:phclient.EventBoostTriggered"},
		"internal/services/data_export_service.go":                 {"Capture:phclient.EventDataExportGenerated"},
		"internal/services/deletion_service.go":                    {"Capture:phclient.EventDeletionCancelled", "Capture:phclient.EventDeletionRequested", "Capture:phclient.EventUserDeleted"},
		"internal/services/household_service.go":                   {"Capture:event"},
		"internal/services/investment_sync_service.go":             {"Capture:phclient.EventInvestmentHoldingsSynced", "Capture:phclient.EventInvestmentTransactionsSynced"},
		"internal/services/liability_sync_service.go":              {"Capture:phclient.EventLiabilitiesSynced"},
		"internal/services/merchant_service.go":                    {"Capture:phclient.EventMerchantOverrideCleared", "Capture:phclient.EventMerchantOverrideSet", "Capture:phclient.EventMerchantOverrideSet", "Capture:phclient.EventMerchantOverrideSet", "Capture:phclient.EventMerchantOverrideSet", "Capture:phclient.EventMerchantsBulkOverridden", "Capture:phclient.EventMerchantsMerged"},
		"internal/services/registration_service.go":                {"Capture:phclient.EventUserRegistered"},
		"internal/services/rule_application_service.go":            {"Capture:phclient.EventAutoCategorizationExecuted", "Capture:phclient.EventAutoCategorizationExecuted"},
		"internal/services/savings_goal_service.go":                {"Capture:phclient.EventSavingsGoalProgressBasisChanged"},
		"internal/services/transaction_service.go":                 {"Capture:phclient.EventTransactionRecategorized", "Capture:phclient.EventTransactionRecategorized", "Capture:phclient.EventTransactionReviewStateChanged"},
		"internal/services/transfer_correlation_service.go":        {"Capture:phclient.EventTransferSuggestionMatched", "Capture:phclient.EventTransferSuggestionMatched"},
		"internal/services/transfer_suggestion_service.go":         {"CaptureIfConsented:phclient.TransferSuggestionAccepted", "CaptureIfConsented:phclient.TransferSuggestionCanceled", "CaptureIfConsented:phclient.TransferSuggestionRejected"},
		"internal/workers/plaid_sync_provider.go":                  {"Capture:phclient.EventAccountSynced", "Capture:phclient.EventSyncCompleted", "Capture:phclient.EventSyncFailed"},
		"internal/workers/webhook_processor.go":                    {"Capture:posthog.EventWebhookReceived", "Capture:posthog.EventWebhookReceived"},
	}

	backendRoot := filepath.Join("..", "..", "..")
	got := make(map[string][]string)
	err := filepath.WalkDir(backendRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return walkErr
		}
		rel, relErr := filepath.Rel(backendRoot, path)
		require.NoError(t, relErr)
		if strings.HasPrefix(filepath.ToSlash(rel), "internal/integration/posthog/") {
			return nil // Gateway/transport compatibility adapters, not event producers.
		}
		source, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		for _, capture := range rawCaptureSymbols(t, path, source) {
			got[filepath.ToSlash(rel)] = append(got[filepath.ToSlash(rel)], capture)
		}
		sort.Strings(got[filepath.ToSlash(rel)])
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestRawCaptureInventoryDetectsEventSymbolSubstitution(t *testing.T) {
	before := rawCaptureSymbols(t, "fixture.go", []byte(`package fixture
import phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
func f() { analytics.Capture(userID, phclient.EventOne, nil) }`))
	after := rawCaptureSymbols(t, "fixture.go", []byte(`package fixture
import phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
func f() { analytics.Capture(userID, phclient.EventTwo, nil) }`))
	assert.Equal(t, []string{"Capture:phclient.EventOne"}, before)
	assert.Equal(t, []string{"Capture:phclient.EventTwo"}, after)
	assert.NotEqual(t, before, after)
}

func TestRawCaptureInventoryDetectsQualifierOrLocalAliasSubstitution(t *testing.T) {
	canonical := rawCaptureSymbols(t, "fixture.go", []byte(`package fixture
import phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
func f() { analytics.Capture(userID, phclient.EventEmailChanged, nil) }`))
	otherQualifier := rawCaptureSymbols(t, "fixture.go", []byte(`package fixture
import other "example.com/not-posthog"
func f() { analytics.Capture(userID, other.EventEmailChanged, nil) }`))
	localAlias := rawCaptureSymbols(t, "fixture.go", []byte(`package fixture
func f() { analytics.Capture(userID, EventEmailChanged, nil) }`))
	shadowedImport := rawCaptureSymbols(t, "fixture.go", []byte(`package fixture
import phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
func f() {
	phclient := struct{ EventEmailChanged string }{}
	analytics.Capture(userID, phclient.EventEmailChanged, nil)
}`))
	assert.Equal(t, []string{"Capture:phclient.EventEmailChanged"}, canonical)
	assert.Equal(t, []string{"Capture:<non-posthog-import:other>.EventEmailChanged"}, otherQualifier)
	assert.Equal(t, []string{"Capture:EventEmailChanged"}, localAlias)
	assert.Equal(t, []string{"Capture:<local:phclient>.EventEmailChanged"}, shadowedImport)
	assert.NotEqual(t, canonical, otherQualifier)
	assert.NotEqual(t, canonical, localAlias)
	assert.NotEqual(t, canonical, shadowedImport)
}

func TestHouseholdRawCaptureWrapperCallersArePinned(t *testing.T) {
	path := filepath.Join("..", "..", "services", "household_service.go")
	source, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"phclient.EventHouseholdCreated",
		"phclient.EventHouseholdDisbanded",
		"phclient.EventHouseholdForceDisbanded",
		"phclient.EventHouseholdInviteAccepted",
		"phclient.EventHouseholdInviteSent",
		"phclient.EventHouseholdMemberAutoOffboarded",
		"phclient.EventHouseholdMemberLeft",
		"phclient.EventHouseholdMemberRemoved",
		"phclient.EventHouseholdMembershipPurged",
	}, namedCallArgumentSymbols(t, path, source, "capture", 1))
}

func rawCaptureSymbols(t *testing.T, path string, source []byte) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, source, 0)
	require.NoError(t, err)
	imports := importBindings(t, file)
	var captures []string
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || (selector.Sel.Name != "Capture" && selector.Sel.Name != "CaptureIfConsented") {
			return true
		}
		argument := 1
		if selector.Sel.Name == "CaptureIfConsented" {
			argument = 2
		}
		if len(call.Args) <= argument {
			captures = append(captures, selector.Sel.Name+":<missing>")
			return true
		}
		symbol := eventExpressionSymbol(call.Args[argument], imports)
		captures = append(captures, selector.Sel.Name+":"+symbol)
		return true
	})
	return captures
}

func namedCallArgumentSymbols(t *testing.T, path string, source []byte, method string, argument int) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, source, 0)
	require.NoError(t, err)
	imports := importBindings(t, file)
	var symbols []string
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) <= argument {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != method {
			return true
		}
		symbols = append(symbols, eventExpressionSymbol(call.Args[argument], imports))
		return true
	})
	sort.Strings(symbols)
	return symbols
}

const postHogIntegrationImport = "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"

func importBindings(t *testing.T, file *ast.File) map[string]string {
	t.Helper()
	imports := make(map[string]string)
	for _, spec := range file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		require.NoError(t, err)
		qualifier := filepath.Base(importPath)
		if spec.Name != nil {
			qualifier = spec.Name.Name
		}
		imports[qualifier] = importPath
	}
	return imports
}

func eventExpressionSymbol(expression ast.Expr, imports map[string]string) string {
	switch expression := expression.(type) {
	case *ast.Ident:
		return expression.Name
	case *ast.SelectorExpr:
		qualifier, ok := expression.X.(*ast.Ident)
		if !ok {
			return "<dynamic>"
		}
		switch {
		case qualifier.Obj != nil:
			return "<local:" + qualifier.Name + ">." + expression.Sel.Name
		case imports[qualifier.Name] == postHogIntegrationImport:
			return qualifier.Name + "." + expression.Sel.Name
		case imports[qualifier.Name] != "":
			return "<non-posthog-import:" + qualifier.Name + ">." + expression.Sel.Name
		default:
			return "<unbound:" + qualifier.Name + ">." + expression.Sel.Name
		}
	default:
		return "<dynamic>"
	}
}

func TestOperationalFailuresAreNotPostHogProducers(t *testing.T) {
	backendRoot := filepath.Join("..", "..", "..")
	banned := []string{"EventInvestmentSyncFailed", "EventLiabilitySyncFailed", "EventPaddleCleanupFailed", "EventDeadLetterCreated"}
	err := filepath.WalkDir(backendRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return walkErr
		}
		if filepath.Base(path) == "events_generated.go" {
			return nil
		}
		source, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		for _, symbol := range banned {
			assert.NotContains(t, string(source), symbol, path)
		}
		return nil
	})
	require.NoError(t, err)
}
