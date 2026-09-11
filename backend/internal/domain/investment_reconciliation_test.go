package domain

import "testing"

// TestClassifyPositionReconciliationMember_CleanMatchIsNeitherDriftNorSuppressed is the RED test
// for the tolerance band: a delta inside PositionReconciliationQuantityTolerance is float64
// summation noise, not a real disagreement, and must not appear anywhere in the report.
func TestClassifyPositionReconciliationMember_CleanMatchIsNeitherDriftNorSuppressed(t *testing.T) {
	isDrift, isSuppressed := ClassifyPositionReconciliationMember(0.0, true)
	if isDrift || isSuppressed {
		t.Fatalf("exact match must be neither a drift nor suppressed, got isDrift=%v isSuppressed=%v", isDrift, isSuppressed)
	}

	isDrift, isSuppressed = ClassifyPositionReconciliationMember(0.0005, true)
	if isDrift || isSuppressed {
		t.Fatalf("a delta inside the tolerance band must be neither a drift nor suppressed, got isDrift=%v isSuppressed=%v", isDrift, isSuppressed)
	}
}

// TestClassifyPositionReconciliationMember_ExactlyAtToleranceIsNotADrift is the boundary-strictness
// pin round 3's integration-test additions (0.0005 inside / 0.002 outside) could not reach: neither
// value equals PositionReconciliationQuantityTolerance itself, so a regression from the intended
// strict `delta > tolerance` to a non-strict `delta >= tolerance` would pass every existing test in
// this file. This is the ONLY place in the tree that pins Go's `>` strictness at the exact boundary,
// and deliberately so: it passes the tolerance constant itself as delta, with no subtraction in
// between, so there is no floating-point rounding to accidentally save (or break) the assertion.
//
// ⚠ #1631 M8b round 4 first tried to ALSO pin this boundary from the integration-test side, seeding
// holding 100.001 / buy 100 (a delta of exactly the tolerance) and asserting agreement between the
// SQL and Go paths. Round 5 found that assertion wrong and removed it: Postgres computes
// `100.00100000 - 100.00000000` as EXACT NUMERIC arithmetic (holdings.quantity and
// investment_transactions.quantity are DECIMAL(18,8), migration 000051) and correctly says "not a
// drift" via strict `>`, but scanning those same values into float64 before this Go function runs
// produces `100.001 - 100 = 0.0010000000000047748` — strictly GREATER than the float64 tolerance —
// so the Go path says "a drift". The two production paths PROVABLY DISAGREE within roughly 1e-15 of
// the tolerance value, and no seeded integration-test row can prove agreement there, because there
// is none to prove. That is a genuine, narrow divergence in production code between NUMERIC and
// float64 arithmetic, not a test gap — see PositionReconciliationQuantityTolerance's doc comment for
// the same note at the source, and the header comment on
// TestPositionReconciliationSummary_AgreesWithPerUserReportAcrossUsers_Integration
// (investment_position_reconciliation_integration_test.go) for why that test cannot and does not
// attempt to cover this boundary. This test's job is narrower and achievable: pin that the Go
// function ITSELF, in isolation, treats the comparison as strict `>`.
func TestClassifyPositionReconciliationMember_ExactlyAtToleranceIsNotADrift(t *testing.T) {
	isDrift, isSuppressed := ClassifyPositionReconciliationMember(PositionReconciliationQuantityTolerance, true)
	if isDrift {
		t.Fatal("a delta of EXACTLY the tolerance must not be a drift — the comparison is strict `>`, not `>=`")
	}
	if isSuppressed {
		t.Fatal("a delta of exactly the tolerance is not a mismatch at all (see the isDrift check above), so it must not be suppressed either")
	}
}

// TestClassifyPositionReconciliationMember_RealDriftWithOpeningBuyIsReported pins the "the ledger
// saw this position open" case: a mismatch beyond tolerance, with at least one buy recorded, is
// exactly the drift this diagnostic exists to surface.
func TestClassifyPositionReconciliationMember_RealDriftWithOpeningBuyIsReported(t *testing.T) {
	isDrift, isSuppressed := ClassifyPositionReconciliationMember(5.0, true)
	if !isDrift {
		t.Fatal("a mismatch beyond tolerance with a ledger buy present must be reported as a drift")
	}
	if isSuppressed {
		t.Fatal("a reported drift must not also be counted as suppressed")
	}

	// Sells overshooting the ledger's own buys still count — negative deltas are not special-cased.
	isDrift, isSuppressed = ClassifyPositionReconciliationMember(-5.0, true)
	if !isDrift || isSuppressed {
		t.Fatalf("a negative mismatch with a ledger buy present must be reported as a drift, got isDrift=%v isSuppressed=%v", isDrift, isSuppressed)
	}
}

// TestClassifyPositionReconciliationMember_PreWindowPositionIsSuppressedNotReported is the test
// that pins the whole point of the file-level lookback decision: a position with NO buy in the
// ledger (only sells/reinvestments, or nothing at all) whose quantity does not reconcile is the
// expected shape of "this position predates Plaid's 2-year lookback", and must be counted
// separately rather than reported as a drift — the noise the brief warned against.
func TestClassifyPositionReconciliationMember_PreWindowPositionIsSuppressedNotReported(t *testing.T) {
	isDrift, isSuppressed := ClassifyPositionReconciliationMember(100.0, false)
	if isDrift {
		t.Fatal("a position with no ledger buy must never be reported as a drift — its opening trade is outside the visible window by construction")
	}
	if !isSuppressed {
		t.Fatal("a pre-window mismatch must be counted as suppressed so the report stays honest about what it could not evaluate")
	}
}

// TestClassifyPositionReconciliationMember_NoLedgerBuyButCleanMatchIsNeither guards against
// conflating "no buy in the ledger" with "always suppressed": if the delta happens to be within
// tolerance anyway (e.g. a position untouched since before the window and never touched since),
// there is nothing to report or suppress.
func TestClassifyPositionReconciliationMember_NoLedgerBuyButCleanMatchIsNeither(t *testing.T) {
	isDrift, isSuppressed := ClassifyPositionReconciliationMember(0.0, false)
	if isDrift || isSuppressed {
		t.Fatalf("a clean match must never be reported, even with no ledger buy, got isDrift=%v isSuppressed=%v", isDrift, isSuppressed)
	}
}
