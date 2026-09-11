//go:build integration

package repository

// #1631 (M4) — the DURABLE backfill obligation on user_accounts.
//
// As of #1631 the investment sync stops PERSISTING investment_transactions for a chain whose owner
// set exclude_investment_sync, which turns clearing the flag into a request for data we do not
// have. The sync therefore owes that chain one full-lookback re-fetch, and the obligation has to
// survive a crash between the flag write and the re-fetch — otherwise it is lost forever, because
// the transition is true -> false and a retry of a write that already landed is false -> false.
//
// WHY THESE TESTS MUST BE INTEGRATION TESTS. The transition is decided by a CASE expression
// evaluated by Postgres against the row's CURRENT value, inside the same UPDATE that writes the
// flag. pgxmock asserts the SQL string and then hands back rows the test author invented, so it can
// express neither the pre-update value nor the CASE's result — exactly the reasoning recorded on
// user_account_hidden_source_integration_test.go for the hidden_source CASE.
//
// See docs/decisions/2026-08-24-financial-history-retained-by-default.md
// § "Non-ingestion is not destruction" for why the ingestion filter is permitted at all, and for
// the availability cost it carries.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type backfillObligationFixture struct {
	pool   *pgxpool.Pool
	userID uuid.UUID
	repo   UserAccountRepository
	prefix string
}

func newBackfillObligationFixture(t *testing.T) (*backfillObligationFixture, context.Context) {
	t.Helper()
	pool := connectTestDB(t)
	ctx := context.Background()

	userID := uuid.New()
	prefix := "bfobl-" + userID.String()

	// context.Background() rather than t.Context(): a cancelled context makes the DELETEs silently
	// no-op and leaks fixture rows into the shared database, where a global-scan test in another
	// package picks them up and fails on a mismatched entity.
	t.Cleanup(func() {
		cctx := context.Background()
		for _, stmt := range []string{
			`DELETE FROM user_accounts WHERE user_id = $1`,
			`DELETE FROM users WHERE id = $1`,
		} {
			if _, err := pool.Exec(cctx, stmt, userID); err != nil {
				t.Errorf("cleanup %q: %v", stmt, err)
			}
		}
	})

	_, err := pool.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+prefix, prefix+"@example.com")
	require.NoError(t, err)

	return &backfillObligationFixture{
		pool:   pool,
		userID: userID,
		repo:   NewUserAccountRepository(pool, newTestFieldEncryptor(t)),
		prefix: prefix,
	}, ctx
}

// newChain inserts a chain in a given exclusion state, with no obligation outstanding.
func (f *backfillObligationFixture) newChain(t *testing.T, ctx context.Context, excluded bool) string {
	t.Helper()
	id := uuid.New()
	_, err := f.pool.Exec(ctx, `
		INSERT INTO user_accounts (id, user_id, institution_id, primary_type, exclude_investment_sync)
		VALUES ($1, $2, $3, 'investment', $4)`,
		id, f.userID, f.prefix+"-inst-"+id.String()[:8], excluded)
	require.NoError(t, err)
	return id.String()
}

// markObligation writes the marker directly, for tests about consuming one rather than creating one.
func (f *backfillObligationFixture) markObligation(t *testing.T, ctx context.Context, id string, at time.Time) {
	t.Helper()
	_, err := f.pool.Exec(ctx,
		`UPDATE user_accounts SET investment_backfill_requested_at = $2 WHERE id = $1`, id, at)
	require.NoError(t, err)
}

func (f *backfillObligationFixture) obligation(t *testing.T, ctx context.Context, id string) *time.Time {
	t.Helper()
	var at *time.Time
	require.NoError(t, f.pool.QueryRow(ctx,
		`SELECT investment_backfill_requested_at FROM user_accounts WHERE id = $1`, id).Scan(&at))
	return at
}

// load reads the chain back through the repository so an Update round-trip starts from real state
// (the encrypted PII columns in particular).
func (f *backfillObligationFixture) load(t *testing.T, ctx context.Context, id string) *domain.UserAccount {
	t.Helper()
	ua, err := f.repo.GetByIDAndUser(ctx, id, f.userID.String())
	require.NoError(t, err)
	return ua
}

// ─────────────────────────────────────────────────────────────────────────────
// Recording the obligation — the true -> false transition, and nothing else
// ─────────────────────────────────────────────────────────────────────────────

// TestBackfillObligation_UnTogglingRecordsTheObligation is the milestone's recoverability
// guarantee. It is asserted through Update — the ONE statement that writes the flag — because the
// whole design rests on the two writes being the same statement: any second write could be lost by
// a crash in between, and the transition would never be observable again.
func TestBackfillObligation_UnTogglingRecordsTheObligation(t *testing.T) {
	f, ctx := newBackfillObligationFixture(t)

	id := f.newChain(t, ctx, true)
	require.Nil(t, f.obligation(t, ctx, id), "precondition: nothing owed yet")

	ua := f.load(t, ctx, id)
	ua.ExcludeInvestmentSync = false
	require.NoError(t, f.repo.Update(ctx, ua))

	at := f.obligation(t, ctx, id)
	require.NotNil(t, at,
		"clearing exclude_investment_sync must record the backfill obligation IN THE SAME "+
			"statement; without it the user has asked for history the sync never stored and "+
			"nothing will ever go and fetch it")
	assert.WithinDuration(t, time.Now(), *at, time.Minute)
}

// TestBackfillObligation_SettingTheFlagRecordsNothing — going the other way owes nothing. The rows
// for the newly excluded period are simply never stored.
func TestBackfillObligation_SettingTheFlagRecordsNothing(t *testing.T) {
	f, ctx := newBackfillObligationFixture(t)

	id := f.newChain(t, ctx, false)

	ua := f.load(t, ctx, id)
	ua.ExcludeInvestmentSync = true
	require.NoError(t, f.repo.Update(ctx, ua))

	assert.Nil(t, f.obligation(t, ctx, id))
}

// TestBackfillObligation_NoTransitionRecordsNothing covers both no-op writes. An UPDATE that leaves
// the flag where it was is the overwhelmingly common case (any nickname or colour change goes
// through the same statement), and recording an obligation for one would order a full-lookback
// re-fetch of every chain on every customization edit.
func TestBackfillObligation_NoTransitionRecordsNothing(t *testing.T) {
	f, ctx := newBackfillObligationFixture(t)

	for _, tc := range []struct {
		name     string
		excluded bool
	}{
		{"stays excluded", true},
		{"stays included", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := f.newChain(t, ctx, tc.excluded)

			ua := f.load(t, ctx, id)
			nickname := "renamed"
			ua.Nickname = &nickname
			require.NoError(t, f.repo.Update(ctx, ua))

			assert.Nil(t, f.obligation(t, ctx, id))
		})
	}
}

// TestBackfillObligation_AnUnrelatedEditDoesNotDischargeIt is the other half of the CASE's ELSE
// branch: an obligation already recorded and not yet met must survive every write that is not the
// sync discharging it. A colour change is not a backfill.
func TestBackfillObligation_AnUnrelatedEditDoesNotDischargeIt(t *testing.T) {
	f, ctx := newBackfillObligationFixture(t)

	id := f.newChain(t, ctx, false)
	owedAt := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Millisecond)
	f.markObligation(t, ctx, id, owedAt)

	ua := f.load(t, ctx, id)
	colour := "#123456"
	ua.CustomColor = &colour
	require.NoError(t, f.repo.Update(ctx, ua))

	at := f.obligation(t, ctx, id)
	require.NotNil(t, at, "an unrelated customization edit must not discharge an owed backfill")
	assert.WithinDuration(t, owedAt, *at, time.Millisecond,
		"and it must not refresh the marker's timestamp either — the timestamp is what the sync "+
			"compares against when it decides whether its run covered the obligation")
}

// ─────────────────────────────────────────────────────────────────────────────
// Consuming and discharging it
// ─────────────────────────────────────────────────────────────────────────────

// TestBackfillObligation_PendingReturnsOnlyOwingChains — the sync passes every included chain on a
// linked item and must get back only the ones that owe something, since a hit widens the Plaid
// window to two years for the whole item.
func TestBackfillObligation_PendingReturnsOnlyOwingChains(t *testing.T) {
	f, ctx := newBackfillObligationFixture(t)

	owing := f.newChain(t, ctx, false)
	quiet := f.newChain(t, ctx, false)
	owedAt := time.Now().Add(-time.Hour).UTC().Truncate(time.Millisecond)
	f.markObligation(t, ctx, owing, owedAt)

	pending, err := f.repo.PendingInvestmentBackfills(ctx, []string{owing, quiet}, f.userID.String())
	require.NoError(t, err)

	require.Len(t, pending, 1)
	require.Contains(t, pending, owing)
	assert.WithinDuration(t, owedAt, pending[owing], time.Millisecond)
	assert.NotContains(t, pending, quiet,
		"a chain that owes nothing must be ABSENT, not present with a zero time — a caller "+
			"iterating the map would otherwise order a two-year re-fetch for every account it asked about")
}

// TestBackfillObligation_PendingWithNoIDsIsNotAQuery guards the degenerate input the sync produces
// routinely: an item all of whose accounts are excluded (or carry no chain id) contributes no ids.
// `id = ANY('{}')` would be a pointless round trip, and a hand-built IN () would be a syntax error.
func TestBackfillObligation_PendingWithNoIDsIsNotAQuery(t *testing.T) {
	f, ctx := newBackfillObligationFixture(t)

	pending, err := f.repo.PendingInvestmentBackfills(ctx, nil, f.userID.String())
	require.NoError(t, err)
	assert.Empty(t, pending)

	assert.NoError(t, f.repo.ClearInvestmentBackfillRequests(ctx, nil, f.userID.String(), time.Now()))
}

// TestBackfillObligation_ClearDischargesTheMarkerItObserved is the ordinary success path: the sync
// re-fetched the full window and stored everything, so the debt is paid.
func TestBackfillObligation_ClearDischargesTheMarkerItObserved(t *testing.T) {
	f, ctx := newBackfillObligationFixture(t)

	id := f.newChain(t, ctx, false)
	owedAt := time.Now().Add(-time.Hour).UTC()
	f.markObligation(t, ctx, id, owedAt)

	require.NoError(t, f.repo.ClearInvestmentBackfillRequests(ctx, []string{id}, f.userID.String(), owedAt))

	assert.Nil(t, f.obligation(t, ctx, id))
}

// TestBackfillObligation_ClearLeavesAMarkerNewerThanTheOneObserved is the race the notAfter bound
// exists for, and the reason it is a parameter rather than "now". If the owner re-excluded and
// re-included the chain WHILE the sync was in flight, the new marker covers a window that run did
// not fetch. Clearing it would discard a backfill nobody will ever redo — the same silent-loss
// shape the whole obligation mechanism exists to prevent.
func TestBackfillObligation_ClearLeavesAMarkerNewerThanTheOneObserved(t *testing.T) {
	f, ctx := newBackfillObligationFixture(t)

	id := f.newChain(t, ctx, false)
	observed := time.Now().Add(-time.Hour).UTC()
	requestedAgainMidRun := time.Now().UTC().Truncate(time.Millisecond)
	f.markObligation(t, ctx, id, requestedAgainMidRun)

	require.NoError(t, f.repo.ClearInvestmentBackfillRequests(ctx, []string{id}, f.userID.String(), observed))

	at := f.obligation(t, ctx, id)
	require.NotNil(t, at, "a marker recorded after the sync chose its window must survive the clear")
	assert.WithinDuration(t, requestedAgainMidRun, *at, time.Millisecond)
}

// TestBackfillObligation_ClearIsIdempotent — the sync may clear twice (a retried job, an overlapping
// webhook), and a second clear must be a silent no-op rather than an error. Zero rows affected is
// the ORDINARY outcome here, which is why the implementation deliberately does not treat it as
// ErrUserAccountNotFound.
func TestBackfillObligation_ClearIsIdempotent(t *testing.T) {
	f, ctx := newBackfillObligationFixture(t)

	id := f.newChain(t, ctx, false)
	owedAt := time.Now().UTC()
	f.markObligation(t, ctx, id, owedAt)

	require.NoError(t, f.repo.ClearInvestmentBackfillRequests(ctx, []string{id}, f.userID.String(), owedAt))
	require.NoError(t, f.repo.ClearInvestmentBackfillRequests(ctx, []string{id}, f.userID.String(), owedAt),
		"clearing an already-discharged obligation must not be an error")

	assert.Nil(t, f.obligation(t, ctx, id))
}

// TestBackfillObligation_ARoundTripIsIdempotent walks the whole cycle twice — exclude, re-include,
// discharge — and asserts the second cycle behaves exactly like the first. The obligation is a
// single nullable column rather than an append-only log precisely so that repeating the cycle
// cannot accumulate duplicate work.
func TestBackfillObligation_ARoundTripIsIdempotent(t *testing.T) {
	f, ctx := newBackfillObligationFixture(t)

	id := f.newChain(t, ctx, false)

	for cycle := 1; cycle <= 2; cycle++ {
		ua := f.load(t, ctx, id)
		ua.ExcludeInvestmentSync = true
		require.NoError(t, f.repo.Update(ctx, ua))
		require.Nil(t, f.obligation(t, ctx, id), "cycle %d: excluding owes nothing", cycle)

		ua = f.load(t, ctx, id)
		ua.ExcludeInvestmentSync = false
		require.NoError(t, f.repo.Update(ctx, ua))

		pending, err := f.repo.PendingInvestmentBackfills(ctx, []string{id}, f.userID.String())
		require.NoError(t, err)
		require.Len(t, pending, 1, "cycle %d: exactly one obligation, never two", cycle)

		require.NoError(t, f.repo.ClearInvestmentBackfillRequests(ctx, []string{id}, f.userID.String(), pending[id]))
		require.Nil(t, f.obligation(t, ctx, id), "cycle %d: discharged", cycle)
	}
}
