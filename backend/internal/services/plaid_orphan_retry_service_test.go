package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/plaid/plaid-go/v23/plaid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// ─── doubles ─────────────────────────────────────────────────────────────────────────────────────

// fakeOrphanRepo records every call so each test can assert what the sweep DID, not merely that it
// returned nil. Hand-written rather than testify/mock: the assertions here are about ordering and
// about which of two mutually exclusive writes happened, and a call log reads better for both.
type fakeOrphanRepo struct {
	pages [][]domain.PlaidOrphanedItem
	// pageErrs[i] is returned instead of pages[i] when non-nil.
	pageErrs []error
	calls    int
	cursors  []*repository.OrphanRetryCursor
	limits   []int

	resolved      []string
	resolvedErr   error
	failures      map[string]string
	failureErr    error
	failureCalls  int
	resolveReturn bool
}

func newFakeOrphanRepo(pages ...[]domain.PlaidOrphanedItem) *fakeOrphanRepo {
	return &fakeOrphanRepo{pages: pages, failures: map[string]string{}, resolveReturn: true}
}

func (f *fakeOrphanRepo) GetRetryablePage(_ context.Context, _ time.Time, after *repository.OrphanRetryCursor, limit int) ([]domain.PlaidOrphanedItem, error) {
	i := f.calls
	f.calls++
	f.cursors = append(f.cursors, after)
	f.limits = append(f.limits, limit)
	if i < len(f.pageErrs) && f.pageErrs[i] != nil {
		return nil, f.pageErrs[i]
	}
	if i >= len(f.pages) {
		return nil, nil
	}
	return f.pages[i], nil
}

func (f *fakeOrphanRepo) MarkResolved(_ context.Context, id string, _ time.Time) (bool, error) {
	if f.resolvedErr != nil {
		return false, f.resolvedErr
	}
	f.resolved = append(f.resolved, id)
	return f.resolveReturn, nil
}

func (f *fakeOrphanRepo) RecordRetryFailure(_ context.Context, id, code string, _ time.Time) error {
	f.failureCalls++
	if f.failureErr != nil {
		return f.failureErr
	}
	f.failures[id] = code
	return nil
}

// fakeRemover stands in for plaidService.RemoveItem, keyed by the token it is handed so a test can
// prove WHICH credential was presented — not merely that some call happened.
type fakeRemover struct {
	errByToken map[string]error
	tokens     []string
}

func (f *fakeRemover) RemoveItem(_ context.Context, accessToken string) error {
	f.tokens = append(f.tokens, accessToken)
	return f.errByToken[accessToken]
}

func plaidAPIError(t *testing.T, code string) error {
	t.Helper()
	return plaid.MakeGenericOpenAPIError(
		[]byte(`{"error_type":"ITEM_ERROR","error_code":"`+code+`","error_message":"x"}`),
		"400 Bad Request",
		nil,
	)
}

func liveOrphan(id, token string) domain.PlaidOrphanedItem {
	return domain.PlaidOrphanedItem{
		ID:             id,
		PlaidItemID:    "plaid-" + id,
		AccessToken:    token,
		OrphanedAt:     time.Now().Add(-time.Hour),
		TokenExpiresAt: time.Now().Add(domain.OrphanedItemTokenRetention),
	}
}

func newRetryService(repo orphanRetryRepo, remover plaidServiceForCleanup) *plaidOrphanRetryService {
	return NewPlaidOrphanRetryService(repo, remover)
}

// ─── the retry itself ────────────────────────────────────────────────────────────────────────────

func TestPlaidOrphanRetryService_Sweep_SuccessResolvesAndPresentsTheRetainedToken(t *testing.T) {
	repo := newFakeOrphanRepo([]domain.PlaidOrphanedItem{liveOrphan("a", "tok-a")})
	remover := &fakeRemover{errByToken: map[string]error{}}

	res, err := newRetryService(repo, remover).Sweep(context.Background(), time.Now(), 10, 100)
	require.NoError(t, err)

	assert.Equal(t, []string{"tok-a"}, remover.tokens,
		"the whole point of retaining the credential is that it is the value presented to Plaid")
	assert.Equal(t, []string{"a"}, repo.resolved)
	assert.Empty(t, repo.failures)
	assert.Equal(t, 1, res.Resolved)
	assert.Equal(t, 1, res.Attempted)
}

// ITEM_NOT_FOUND means the Item is already gone, which is the same outcome as a successful removal —
// the same rule isPlaidItemAlreadyGone already applies on the pre-erasure path. Recording it as a
// failure would keep re-attempting an Item Plaid has forgotten until the deadline.
func TestPlaidOrphanRetryService_Sweep_ItemNotFoundCountsAsRemoved(t *testing.T) {
	repo := newFakeOrphanRepo([]domain.PlaidOrphanedItem{liveOrphan("a", "tok-a")})
	remover := &fakeRemover{errByToken: map[string]error{"tok-a": plaidAPIError(t, "ITEM_NOT_FOUND")}}

	res, err := newRetryService(repo, remover).Sweep(context.Background(), time.Now(), 10, 100)
	require.NoError(t, err)
	assert.Equal(t, []string{"a"}, repo.resolved)
	assert.Empty(t, repo.failures)
	assert.Equal(t, 1, res.Resolved)
}

// An ordinary failure records the code and leaves everything else alone — no purge, no resolution.
func TestPlaidOrphanRetryService_Sweep_FailureRecordsTheCodeAndKeepsTheRecordOutstanding(t *testing.T) {
	repo := newFakeOrphanRepo([]domain.PlaidOrphanedItem{liveOrphan("a", "tok-a")})
	remover := &fakeRemover{errByToken: map[string]error{"tok-a": plaidAPIError(t, "INTERNAL_SERVER_ERROR")}}

	res, err := newRetryService(repo, remover).Sweep(context.Background(), time.Now(), 10, 100)
	require.NoError(t, err)
	assert.Empty(t, repo.resolved)
	assert.Equal(t, map[string]string{"a": "INTERNAL_SERVER_ERROR"}, repo.failures)
	assert.Equal(t, 1, res.Failed)
}

// A non-Plaid error (network, context, anything without an error_code) must still be recorded, and
// with a code that does not pretend to be Plaid's.
func TestPlaidOrphanRetryService_Sweep_NonPlaidErrorGetsALocalSentinelNotAnEmptyCode(t *testing.T) {
	repo := newFakeOrphanRepo([]domain.PlaidOrphanedItem{liveOrphan("a", "tok-a")})
	remover := &fakeRemover{errByToken: map[string]error{"tok-a": errors.New("dial tcp: connection refused")}}

	_, err := newRetryService(repo, remover).Sweep(context.Background(), time.Now(), 10, 100)
	require.NoError(t, err)
	assert.Equal(t, orphanRetryTransportErrorCode, repo.failures["a"],
		"an empty code in the column would read as 'never attempted', which is a different fact")
}

// THE ROTATED-KEY CASE. A token that will not decrypt can never succeed, so it must not be sent to
// Plaid at all — and it must be recorded, so the record stops claiming to be auto-retrying instead of
// silently rotting behind a green label for thirty days.
func TestPlaidOrphanRetryService_Sweep_UndecryptableTokenIsRecordedAndNeverSentToPlaid(t *testing.T) {
	row := liveOrphan("a", "")
	row.TokenUndecryptable = true
	repo := newFakeOrphanRepo([]domain.PlaidOrphanedItem{row})
	remover := &fakeRemover{errByToken: map[string]error{}}

	res, err := newRetryService(repo, remover).Sweep(context.Background(), time.Now(), 10, 100)
	require.NoError(t, err)

	assert.Empty(t, remover.tokens, "there is nothing usable to present; calling Plaid with '' would be a lie")
	assert.Equal(t, domain.OrphanTokenUndecryptableCode, repo.failures["a"])
	assert.Empty(t, repo.resolved)
	assert.Equal(t, 1, res.Undecryptable)
}

// A row the repository handed over with no token at all (defensive: the SQL predicate excludes them)
// must be skipped rather than sent as an empty credential.
func TestPlaidOrphanRetryService_Sweep_EmptyTokenIsSkippedNotSent(t *testing.T) {
	repo := newFakeOrphanRepo([]domain.PlaidOrphanedItem{liveOrphan("a", "")})
	remover := &fakeRemover{errByToken: map[string]error{}}

	res, err := newRetryService(repo, remover).Sweep(context.Background(), time.Now(), 10, 100)
	require.NoError(t, err)
	assert.Empty(t, remover.tokens)
	assert.Equal(t, 1, res.Skipped)
}

// ─── bounding ────────────────────────────────────────────────────────────────────────────────────

// Keyset pagination is actually driven: the second page's cursor is derived from the last row of the
// first, so a row that could not be processed does not occupy the oldest slot of every later page.
func TestPlaidOrphanRetryService_Sweep_AdvancesTheKeysetCursorAcrossPages(t *testing.T) {
	first := liveOrphan("a", "tok-a")
	second := liveOrphan("b", "tok-b")
	repo := newFakeOrphanRepo(
		[]domain.PlaidOrphanedItem{first},
		[]domain.PlaidOrphanedItem{second},
	)
	remover := &fakeRemover{errByToken: map[string]error{}}

	res, err := newRetryService(repo, remover).Sweep(context.Background(), time.Now(), 1, 100)
	require.NoError(t, err)
	assert.Equal(t, 2, res.Attempted)

	require.GreaterOrEqual(t, len(repo.cursors), 2)
	assert.Nil(t, repo.cursors[0], "the first page starts from the beginning")
	require.NotNil(t, repo.cursors[1])
	assert.Equal(t, "a", repo.cursors[1].ID, "the cursor must advance past the last row of page 1")
	assert.Equal(t, first.OrphanedAt, repo.cursors[1].OrphanedAt)
}

// The per-sweep cap stops the run, and the remainder is deferred rather than dropped.
func TestPlaidOrphanRetryService_Sweep_StopsAtItsPerSweepCap(t *testing.T) {
	repo := newFakeOrphanRepo(
		[]domain.PlaidOrphanedItem{liveOrphan("a", "tok-a"), liveOrphan("b", "tok-b")},
		[]domain.PlaidOrphanedItem{liveOrphan("c", "tok-c")},
	)
	remover := &fakeRemover{errByToken: map[string]error{}}

	res, err := newRetryService(repo, remover).Sweep(context.Background(), time.Now(), 2, 1)
	require.NoError(t, err)
	assert.Equal(t, 1, res.Attempted, "the cap bounds the TOTAL attempts across every page of one run")
	assert.True(t, res.CapHit)
	assert.Equal(t, []string{"tok-a"}, remover.tokens)
}

// ─── failure handling ────────────────────────────────────────────────────────────────────────────

// A page query that fails aborts the run with an error, because a sweep that cannot read its work list
// has not done its work and must not report success. Contrast the per-row failures above, which are
// recorded and stepped over.
func TestPlaidOrphanRetryService_Sweep_PageQueryFailureIsReturned(t *testing.T) {
	repo := newFakeOrphanRepo()
	repo.pageErrs = []error{errors.New("connection reset")}
	remover := &fakeRemover{errByToken: map[string]error{}}

	_, err := newRetryService(repo, remover).Sweep(context.Background(), time.Now(), 10, 100)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection reset")
}

// A recording write that fails must not be swallowed into a green run: the row's attempt was made and
// the outcome is now unrecorded, which is exactly the "work happened, mark did not" half of this
// branch's recurring defect. It is counted and surfaced, and the sweep continues so one bad row cannot
// stop the rest.
func TestPlaidOrphanRetryService_Sweep_RecordingFailuresAreCountedNotSwallowed(t *testing.T) {
	repo := newFakeOrphanRepo([]domain.PlaidOrphanedItem{liveOrphan("a", "tok-a"), liveOrphan("b", "tok-b")})
	repo.failureErr = errors.New("db down")
	remover := &fakeRemover{errByToken: map[string]error{
		"tok-a": plaidAPIError(t, "INTERNAL_SERVER_ERROR"),
		"tok-b": plaidAPIError(t, "INTERNAL_SERVER_ERROR"),
	}}

	res, err := newRetryService(repo, remover).Sweep(context.Background(), time.Now(), 10, 100)
	require.NoError(t, err, "one unrecordable row must not abort the sweep")
	assert.Equal(t, 2, res.RecordErrors)
	assert.Equal(t, 2, repo.failureCalls, "the second row is still attempted")
}

// The other half of the same defect shape: the Item IS gone from Plaid and the resolution could not be
// written. The row still holds its credential, still reads as outstanding, and will be retried — which
// is safe (a second /item/remove answers ITEM_NOT_FOUND, i.e. success) — but the run must not report a
// resolution it failed to persist.
func TestPlaidOrphanRetryService_Sweep_UnrecordableResolutionIsNotCountedAsResolved(t *testing.T) {
	repo := newFakeOrphanRepo([]domain.PlaidOrphanedItem{liveOrphan("a", "tok-a")})
	repo.resolvedErr = errors.New("db down")
	remover := &fakeRemover{errByToken: map[string]error{}}

	res, err := newRetryService(repo, remover).Sweep(context.Background(), time.Now(), 10, 100)
	require.NoError(t, err)
	assert.Equal(t, 0, res.Resolved, "nothing was recorded, so nothing may be claimed")
	assert.Equal(t, 1, res.RecordErrors)
	assert.Empty(t, repo.failures, "a SUCCESSFUL removal must never be recorded as a failure")
}

// The query limit handed to the repository is min(pageSize, remaining-under-the-cap), so the cap cannot
// be overshot by a page's worth of work even if the repository honours its LIMIT exactly.
func TestPlaidOrphanRetryService_Sweep_NarrowsTheQueryLimitToTheRemainingBudget(t *testing.T) {
	repo := newFakeOrphanRepo([]domain.PlaidOrphanedItem{liveOrphan("a", "tok-a")})
	remover := &fakeRemover{errByToken: map[string]error{}}

	_, err := newRetryService(repo, remover).Sweep(context.Background(), time.Now(), 500, 3)
	require.NoError(t, err)
	require.NotEmpty(t, repo.limits)
	assert.Equal(t, 3, repo.limits[0], "pageSize 500 with a budget of 3 must ask for 3, not 500")
}

// A nil Plaid client is the "Plaid not configured" deployment. The sweep must refuse rather than
// panic — and must not report having retried anything.
func TestPlaidOrphanRetryService_Sweep_RefusesWithoutAPlaidClient(t *testing.T) {
	repo := newFakeOrphanRepo([]domain.PlaidOrphanedItem{liveOrphan("a", "tok-a")})

	_, err := NewPlaidOrphanRetryService(repo, nil).Sweep(context.Background(), time.Now(), 10, 100)
	require.Error(t, err)
	assert.Equal(t, 0, repo.calls, "it must not even read the work list it cannot act on")
}
