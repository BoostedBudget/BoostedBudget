package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// Issue #1497 item 5: the orphaned-Plaid-Item counts on /admin/health.
//
// ⚠ WHAT THESE TESTS DELIBERATELY DO NOT DO: assert that the response HAS a
// plaid_orphaned_items field. That check passes against a hardcoded zero, which is the exact
// species of vacuous gate this branch has spent ten rounds removing. Every assertion below names a
// number derived from a seeded population, and the two operator states are seeded to DIFFERENT
// counts so no shared "unresolved" total can satisfy both.

// MockPlaidOrphanedItemLister mocks the one-verb read the health derivation needs. Narrow on
// purpose: the health surface must not be able to purge a credential or mark a row resolved, and
// keeping those verbs out of the interface makes that a compile-time property.
type MockPlaidOrphanedItemLister struct {
	mock.Mock
}

func (m *MockPlaidOrphanedItemLister) List(ctx context.Context, limit, offset int) ([]domain.PlaidOrphanedItem, int, error) {
	args := m.Called(ctx, limit, offset)
	items, _ := args.Get(0).([]domain.PlaidOrphanedItem)
	return items, args.Int(1), args.Error(2)
}

// orphanFixtureLive is a row AdminState reports as auto_retrying at `now`.
func orphanFixtureLive(id string, now time.Time, attempts int) domain.PlaidOrphanedItem {
	lastRetry := now.Add(-30 * time.Minute)
	return domain.PlaidOrphanedItem{
		ID:                id,
		AccessToken:       "access-live",
		TokenExpiresAt:    now.Add(20 * 24 * time.Hour),
		OrphanedAt:        now.Add(-2 * 24 * time.Hour),
		LastRetryAt:       &lastRetry,
		RetryAttemptCount: attempts,
	}
}

// orphanFixturePurged is a row AdminState reports as manual_intervention_required at `now`: the
// credential is past its retention deadline, so nothing automated can ever finish it.
func orphanFixturePurged(id string, now time.Time, attempts int) domain.PlaidOrphanedItem {
	lastRetry := now.Add(-1 * time.Hour)
	purged := now.Add(-30 * time.Minute)
	return domain.PlaidOrphanedItem{
		ID:                id,
		TokenExpiresAt:    now.Add(-1 * time.Hour),
		TokenPurgedAt:     &purged,
		OrphanedAt:        now.Add(-31 * 24 * time.Hour),
		LastRetryAt:       &lastRetry,
		RetryAttemptCount: attempts,
	}
}

// orphanFixtureResolved is a proved removal: history.
func orphanFixtureResolved(id string, now time.Time) domain.PlaidOrphanedItem {
	resolved := now.Add(-6 * time.Hour)
	return domain.PlaidOrphanedItem{
		ID:             id,
		TokenExpiresAt: now.Add(-6 * time.Hour),
		OrphanedAt:     now.Add(-32 * 24 * time.Hour),
		ResolvedAt:     &resolved,
	}
}

func newOrphanHealthService(lister *MockPlaidOrphanedItemLister) (*systemHealthService, *MockSystemHealthRepository, *MockDeadLetterJobRepository, *MockPool) {
	mockHealthRepo := new(MockSystemHealthRepository)
	mockDeadLetterRepo := new(MockDeadLetterJobRepository)
	mockPool := new(MockPool)
	setupFullHealthMocks(mockHealthRepo, mockDeadLetterRepo, mockPool, 0, 0, 0)

	svc := &systemHealthService{
		healthRepo:     mockHealthRepo,
		deadLetterRepo: mockDeadLetterRepo,
		pool:           mockPool,
	}
	if lister != nil {
		svc.plaidOrphanRepo = lister
	}
	return svc, mockHealthRepo, mockDeadLetterRepo, mockPool
}

// THE CORE ASSERTION FOR ITEM 5: the right numbers for a seeded population, AND the two operator
// states told apart.
func TestGetSystemHealth_PlaidOrphanedItems_CountsTheSeededPopulationByState(t *testing.T) {
	now := time.Now().UTC()
	lister := new(MockPlaidOrphanedItemLister)
	// 2 auto-retrying, 5 manual-intervention, 1 resolved — three distinct totals, so no single
	// number can satisfy more than one of the three assertions below.
	items := []domain.PlaidOrphanedItem{
		orphanFixtureLive("live-1", now, 1),
		orphanFixtureLive("live-2", now, 3),
		orphanFixturePurged("purged-1", now, 11),
		orphanFixturePurged("purged-2", now, 2),
		orphanFixturePurged("purged-3", now, 2),
		orphanFixturePurged("purged-4", now, 2),
		orphanFixturePurged("purged-5", now, 2),
		orphanFixtureResolved("resolved-1", now),
	}
	lister.On("List", mock.Anything, domain.PlaidOrphanedItemHealthScanCap, 0).Return(items, len(items), nil)

	svc, _, _, _ := newOrphanHealthService(lister)

	result, err := svc.GetSystemHealth(context.Background())

	require.NoError(t, err)
	got := result.PlaidOrphanedItems
	assert.True(t, got.Available, "a successful read of a real population must be marked available")
	assert.Equal(t, int64(2), got.AutoRetrying)
	assert.Equal(t, int64(5), got.ManualInterventionRequired,
		"five rows whose credential is past its retention deadline — every one is a Plaid Item a human must remove")
	assert.Equal(t, int64(1), got.Resolved)

	// ⚠ THE SIBLING THAT REFUTES THE TEMPTING SHORTCUT. Counting "unresolved" rows would report 7
	// for both actionable states. The fixture makes 2, 5 and 7 all distinct, so a conflated
	// implementation cannot pass — and this is the two-state conflation the whole feature exists to
	// prevent: auto_retrying means DO NOTHING, manual_intervention_required means ACT AT PLAID.
	assert.NotEqual(t, got.AutoRetrying, got.ManualInterventionRequired)
	assert.NotEqual(t, int64(7), got.AutoRetrying, "auto_retrying must not be the unresolved total")
	assert.NotEqual(t, int64(7), got.ManualInterventionRequired, "manual_intervention_required must not be the unresolved total")

	// Item 4's number reaches the response too: the worst unresolved attempt count, 11, not the 2 of
	// the last row scanned and not the 0 of the resolved one.
	assert.Equal(t, int64(11), got.MaxUnresolvedRetryAttemptCount)

	lister.AssertExpectations(t)
}

// THE FAIL-CLOSED PATH, EXERCISED AGAINST A REAL ERROR. ⚠ This codebase has already shipped a
// health metric whose Available: false path was pinned only by a tautology asserting Go's zero
// value against a struct literal; that test could not have failed. This one drives an actual
// repository error through the real code path and asserts the WHOLE response is still served.
func TestGetSystemHealth_PlaidOrphanedItems_ListError_IsUnavailableNotAConfidentZero(t *testing.T) {
	lister := new(MockPlaidOrphanedItemLister)
	lister.On("List", mock.Anything, domain.PlaidOrphanedItemHealthScanCap, 0).
		Return([]domain.PlaidOrphanedItem(nil), 0, errors.New("decrypt plaid_item_id: cipher: message authentication failed"))

	svc, _, _, _ := newOrphanHealthService(lister)

	result, err := svc.GetSystemHealth(context.Background())

	require.NoError(t, err, "one failed gauge must not fail the whole /admin/health response")
	require.NotNil(t, result)
	assert.False(t, result.PlaidOrphanedItems.Available,
		"a failed read must read as 'we do not know', never as 'no erased user's Plaid Item is still billing'")
	assert.Equal(t, int64(0), result.PlaidOrphanedItems.ManualInterventionRequired)
	// The rest of the response still arrived — partial data over none, this file's existing convention.
	assert.NotEmpty(t, result.Status)

	lister.AssertExpectations(t)
}

// A GENUINELY EMPTY TABLE IS A REAL ZERO and must be distinguishable from the failure above. Without
// this sibling, Available could be hardcoded false and the test above would still pass.
func TestGetSystemHealth_PlaidOrphanedItems_EmptyTableIsAnAvailableZero(t *testing.T) {
	lister := new(MockPlaidOrphanedItemLister)
	lister.On("List", mock.Anything, domain.PlaidOrphanedItemHealthScanCap, 0).
		Return([]domain.PlaidOrphanedItem(nil), 0, nil)

	svc, _, _, _ := newOrphanHealthService(lister)

	result, err := svc.GetSystemHealth(context.Background())

	require.NoError(t, err)
	assert.True(t, result.PlaidOrphanedItems.Available,
		"an empty table is a verified zero, and reporting it as unavailable would be as misleading as the reverse")
	assert.Equal(t, int64(0), result.PlaidOrphanedItems.ManualInterventionRequired)
	assert.Equal(t, int64(0), result.PlaidOrphanedItems.AutoRetrying)
	assert.Equal(t, int64(0), result.PlaidOrphanedItems.Resolved)
}

// THE SIZE-ARGUMENT TRIPWIRE. Migrations 000134/000135 argue this table holds single digits of rows,
// which is what licenses deriving the state in Go over a full read. Nothing prunes the table, so the
// argument is about the arrival RATE, not a bound. If the table ever outgrows the cap, the reader
// must REFUSE to report a number rather than report a partial scan as a total — the remedy at that
// point is server-side totals by state, not a bigger cap.
func TestGetSystemHealth_PlaidOrphanedItems_TablePastTheScanCap_RefusesToReportAPartialScan(t *testing.T) {
	now := time.Now().UTC()
	lister := new(MockPlaidOrphanedItemLister)
	page := []domain.PlaidOrphanedItem{orphanFixturePurged("purged-1", now, 1)}
	// total exceeds the cap: the page in hand is a truncated view of the table.
	lister.On("List", mock.Anything, domain.PlaidOrphanedItemHealthScanCap, 0).
		Return(page, domain.PlaidOrphanedItemHealthScanCap+1, nil)

	svc, _, _, _ := newOrphanHealthService(lister)

	result, err := svc.GetSystemHealth(context.Background())

	require.NoError(t, err)
	assert.False(t, result.PlaidOrphanedItems.Available,
		"a truncated scan must not be published as a total")
	assert.Equal(t, int64(0), result.PlaidOrphanedItems.ManualInterventionRequired,
		"and it must not leak the partial count either — 1 of >500 rows is a more convincing lie than 0")
}

// A DERIVATION AT THE CAP EXACTLY IS STILL COMPLETE. The boundary matters: `>= cap` would refuse a
// read that actually saw every row, turning the tripwire into a false alarm one row early.
func TestGetSystemHealth_PlaidOrphanedItems_TableExactlyAtTheScanCap_IsStillAvailable(t *testing.T) {
	now := time.Now().UTC()
	items := make([]domain.PlaidOrphanedItem, 0, domain.PlaidOrphanedItemHealthScanCap)
	for i := 0; i < domain.PlaidOrphanedItemHealthScanCap; i++ {
		items = append(items, orphanFixturePurged("purged", now, 1))
	}
	lister := new(MockPlaidOrphanedItemLister)
	lister.On("List", mock.Anything, domain.PlaidOrphanedItemHealthScanCap, 0).
		Return(items, domain.PlaidOrphanedItemHealthScanCap, nil)

	svc, _, _, _ := newOrphanHealthService(lister)

	result, err := svc.GetSystemHealth(context.Background())

	require.NoError(t, err)
	assert.True(t, result.PlaidOrphanedItems.Available, "the cap is inclusive: a read of exactly cap rows saw the whole table")
	assert.Equal(t, int64(domain.PlaidOrphanedItemHealthScanCap), result.PlaidOrphanedItems.ManualInterventionRequired)
}

// An unwired repository is "unknown", not "zero" — mirrors jobHealthRepo's own nil-safety, and must
// not panic (this constructor shape is reachable from older tests and from a deployment that has not
// wired the repo yet).
func TestGetSystemHealth_PlaidOrphanedItems_NilRepo_IsUnavailableAndDoesNotPanic(t *testing.T) {
	svc, _, _, _ := newOrphanHealthService(nil)

	result, err := svc.GetSystemHealth(context.Background())

	require.NoError(t, err)
	assert.False(t, result.PlaidOrphanedItems.Available)
	assert.Equal(t, domain.PlaidOrphanedItemMetrics{}, result.PlaidOrphanedItems)
}

// ─── Issue #1497 item 2: the operator-attested count must reach /admin/health ─────────────────────
//
// ⚠ WHY THIS IS ASSERTED HERE AND NOT ASSUMED. This surface's counts come from a hand-written switch
// in domain.SummarisePlaidOrphanedItems, so a fourth state added to the domain does not fail to build
// if nobody adds it here — the attested rows would simply be counted as nothing at all, and
// /admin/health's own numbers would stop accounting for the table. This test is what notices.

// orphanFixtureAttested is a row an operator has CLAIMED to have removed at Plaid. Its credential is
// past its deadline, so without the attestation it would be manual_intervention_required — which is
// what the control assertion below depends on.
func orphanFixtureAttested(id string, now time.Time, attempts int) domain.PlaidOrphanedItem {
	item := orphanFixturePurged(id, now, attempts)
	attested := now.Add(-3 * time.Hour)
	item.OperatorResolvedAt = &attested
	item.OperatorResolvedBy = "user_2attestingAdmin"
	item.OperatorResolutionNote = "Removed by hand in Plaid's dashboard."
	return item
}

func TestGetSystemHealth_PlaidOrphanedItems_ReportsTheOperatorAttestedCountSeparately(t *testing.T) {
	now := time.Now().UTC()

	// The control first: the same rows WITHOUT their attestation must be manual-intervention work, or
	// the assertions below would prove nothing about the attestation.
	control := domain.SummarisePlaidOrphanedItems([]domain.PlaidOrphanedItem{
		orphanFixturePurged("a", now, 1), orphanFixturePurged("b", now, 1), orphanFixturePurged("c", now, 1),
	}, now)
	require.Equal(t, int64(3), control.ManualInterventionRequired)
	require.Equal(t, int64(0), control.OperatorAttested)

	lister := new(MockPlaidOrphanedItemLister)
	// 3 attested, 2 manual, 1 auto-retrying, 4 resolved — four distinct totals, so no single number
	// and no pair of collapsed buckets can satisfy all four assertions.
	items := []domain.PlaidOrphanedItem{
		orphanFixtureAttested("attested-1", now, 14),
		orphanFixtureAttested("attested-2", now, 2),
		orphanFixtureAttested("attested-3", now, 2),
		orphanFixturePurged("purged-1", now, 3),
		orphanFixturePurged("purged-2", now, 3),
		orphanFixtureLive("live-1", now, 1),
		orphanFixtureResolved("resolved-1", now),
		orphanFixtureResolved("resolved-2", now),
		orphanFixtureResolved("resolved-3", now),
		orphanFixtureResolved("resolved-4", now),
	}
	lister.On("List", mock.Anything, domain.PlaidOrphanedItemHealthScanCap, 0).Return(items, len(items), nil)

	svc, _, _, _ := newOrphanHealthService(lister)

	result, err := svc.GetSystemHealth(context.Background())
	require.NoError(t, err)

	got := result.PlaidOrphanedItems
	assert.True(t, got.Available)
	assert.Equal(t, int64(3), got.OperatorAttested,
		"three unverified operator claims must be reported as their own number")
	assert.Equal(t, int64(2), got.ManualInterventionRequired,
		"and must not still be counted as work a human has to do")
	assert.Equal(t, int64(4), got.Resolved,
		"⚠ nor summed into the proved-removal count: one number is what Plaid told us, the other is what a colleague told us")
	assert.Equal(t, int64(1), got.AutoRetrying)

	// The four state counts must still account for the whole table.
	assert.Equal(t, int64(len(items)),
		got.AutoRetrying+got.ManualInterventionRequired+got.Resolved+got.OperatorAttested,
		"a state nobody counted is a state nobody sees")

	// The attested row's attempt history still drives item 4's gauge (14), deliberately: the sweep
	// keeps retrying it, so an unverified claim must not silence the number that could expose it.
	assert.Equal(t, int64(14), got.MaxUnresolvedRetryAttemptCount)

	lister.AssertExpectations(t)
}
