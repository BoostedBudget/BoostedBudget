package operations

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// stubOrphanRepo implements only what the handler calls. The other four methods panic deliberately:
// this endpoint is READ-ONLY, and a handler that reached for MarkResolved or PurgeExpiredTokens would
// fail loudly here rather than quietly gaining a write path on an admin GET.
type stubOrphanRepo struct {
	records []domain.PlaidOrphanedItem
	total   int
	err     error
	limit   int
	offset  int
}

func (s *stubOrphanRepo) List(_ context.Context, limit, offset int) ([]domain.PlaidOrphanedItem, int, error) {
	s.limit, s.offset = limit, offset
	return s.records, s.total, s.err
}

func (s *stubOrphanRepo) PurgeExpiredTokens(context.Context, time.Time, int) (int, error) {
	panic("the admin listing must never purge")
}

func (s *stubOrphanRepo) GetRetryablePage(context.Context, time.Time, *repository.OrphanRetryCursor, int) ([]domain.PlaidOrphanedItem, error) {
	panic("the admin listing must never read the retry work list")
}

func (s *stubOrphanRepo) MarkResolved(context.Context, string, time.Time) (bool, error) {
	panic("the admin listing must never resolve")
}

func (s *stubOrphanRepo) RecordRetryFailure(context.Context, string, string, time.Time) error {
	panic("the admin listing must never write")
}

// THE POINT OF THE ENDPOINT: the two states are reported separately, because they call for opposite
// human action and conflating them is how a still-billing Item silently rots behind a green label.
func TestListPlaidOrphanedItems_SeparatesTheTwoOperatorStates(t *testing.T) {
	now := time.Now().UTC()
	resolvedAt := now.Add(-time.Hour)

	repo := &stubOrphanRepo{total: 3, records: []domain.PlaidOrphanedItem{
		{ // live credential, freshly orphaned: something is working on it
			ID: "retrying", PlaidItemID: "item-1", AccessToken: "tok",
			OrphanedAt: now.Add(-time.Hour), TokenExpiresAt: now.Add(domain.OrphanedItemTokenRetention),
		},
		{ // token purged at its deadline: an operator must finish this in Plaid
			ID: "manual", PlaidItemID: "item-2", InstitutionName: "Still Billing Bank",
			OrphanedAt: now.Add(-40 * 24 * time.Hour), TokenExpiresAt: now.Add(-10 * 24 * time.Hour),
			TokenPurgedAt: &resolvedAt, LastErrorCode: "INVALID_ACCESS_TOKEN",
		},
		{ // proved removed: history
			ID: "done", PlaidItemID: "item-3",
			OrphanedAt: now.Add(-20 * 24 * time.Hour), TokenExpiresAt: now.Add(10 * 24 * time.Hour),
			ResolvedAt: &resolvedAt,
		},
	}}

	h := &AdminHandler{plaidOrphanedItemRepo: repo}
	out, err := h.ListPlaidOrphanedItems(context.Background(), &ListPlaidOrphanedItemsInput{Limit: 50})
	require.NoError(t, err)

	assert.Equal(t, 1, out.Body.AutoRetrying)
	assert.Equal(t, 1, out.Body.ManualInterventionRequired)
	assert.Equal(t, 1, out.Body.Resolved)
	assert.Equal(t, 3, out.Body.Total)
	require.Len(t, out.Body.Items, 3)
	assert.Equal(t, domain.OrphanedItemAutoRetrying, out.Body.Items[0].AdminState)
	assert.Equal(t, domain.OrphanedItemManualInterventionRequired, out.Body.Items[1].AdminState)
	assert.Equal(t, "item-2", out.Body.Items[1].PlaidItemID,
		"the Plaid Item id is the one value an operator needs to finish the job in Plaid's dashboard")
	assert.Equal(t, domain.OrphanedItemResolved, out.Body.Items[2].AdminState)
	assert.NotEmpty(t, out.Body.AsOf, "the instant the labels were computed for must be reported")
}

// The credential must not leave the process through this endpoint. Asserted on the marshalled body,
// because that is what actually crosses the wire.
func TestListPlaidOrphanedItems_NeverReturnsTheRetainedCredential(t *testing.T) {
	now := time.Now().UTC()
	repo := &stubOrphanRepo{total: 1, records: []domain.PlaidOrphanedItem{{
		ID: "a", PlaidItemID: "item-1", AccessToken: "access-sandbox-SUPER-SECRET",
		OrphanedAt: now, TokenExpiresAt: now.Add(domain.OrphanedItemTokenRetention),
	}}}

	h := &AdminHandler{plaidOrphanedItemRepo: repo}
	out, err := h.ListPlaidOrphanedItems(context.Background(), &ListPlaidOrphanedItemsInput{Limit: 50})
	require.NoError(t, err)

	encoded, err := json.Marshal(out.Body)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "SUPER-SECRET")
	assert.True(t, out.Body.Items[0].TokenRetained, "reported as a boolean, never as the value")
}

// Pagination is passed through rather than reinterpreted — a handler that silently substituted its own
// bounds would make the OpenAPI contract's minimum/maximum decorative.
func TestListPlaidOrphanedItems_PassesPaginationThrough(t *testing.T) {
	repo := &stubOrphanRepo{}
	h := &AdminHandler{plaidOrphanedItemRepo: repo}

	_, err := h.ListPlaidOrphanedItems(context.Background(), &ListPlaidOrphanedItemsInput{Limit: 7, Offset: 21})
	require.NoError(t, err)
	assert.Equal(t, 7, repo.limit)
	assert.Equal(t, 21, repo.offset)
}

// An empty table must serialise as [] rather than null, so a client can iterate without a nil check.
func TestListPlaidOrphanedItems_EmptyTableReturnsAnEmptyArray(t *testing.T) {
	h := &AdminHandler{plaidOrphanedItemRepo: &stubOrphanRepo{}}
	out, err := h.ListPlaidOrphanedItems(context.Background(), &ListPlaidOrphanedItemsInput{Limit: 50})
	require.NoError(t, err)

	encoded, err := json.Marshal(out.Body)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"items":[]`)
}

func TestListPlaidOrphanedItems_UnwiredRepoIs503(t *testing.T) {
	h := &AdminHandler{}
	_, err := h.ListPlaidOrphanedItems(context.Background(), &ListPlaidOrphanedItemsInput{Limit: 50})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not available")
}

func TestListPlaidOrphanedItems_RepositoryErrorIsMapped(t *testing.T) {
	h := &AdminHandler{plaidOrphanedItemRepo: &stubOrphanRepo{err: errors.New("connection reset")}}
	_, err := h.ListPlaidOrphanedItems(context.Background(), &ListPlaidOrphanedItemsInput{Limit: 50})
	require.Error(t, err)
}
