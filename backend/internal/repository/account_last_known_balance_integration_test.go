//go:build integration

package repository

// Issue #1591 — the OTHER half of "a closed account carries no current balance".
//
// #1569 task 21 item D established that a terminal account presents NULL for current_balance, and
// TestAccountRepository_TerminalAccount_CarriesNoCurrentBalance_Integration pins it. That left the
// account with nothing at all, and #1569 T7 then removed a UI caption ("Last known balance: … as
// of Jun 4, 2025") because there was never a figure to date.
//
// This is the figure. It comes from `balance_snapshots`, which carries `snapshot_at`, so it can be
// stated as a dated historical claim instead of an orphaned current one. The full three-decision
// note — which reads carry it, why an as-of read must not, and the no-snapshot rule — lives on
// lastKnownBalanceLateral in account_liveness_sql.go.
//
// WHY INTEGRATION AND NOT pgxmock. pgxmock asserts the SQL STRING; it cannot tell a LATERAL that
// picks the newest row from one that picks the oldest, cannot see that `a.status <> 'active'`
// keeps an active account out, and cannot distinguish a NULL from a 0.00. Every assertion below
// is about what Postgres computes.
//
// FIXTURES GO THROUGH PRODUCTION WRITE PATHS. Accounts are created with CreateBatch and closed
// with UpdateStatus (which stamps removed_at and fires the status-history trigger); snapshots are
// written with balanceSnapshotRepository.Create. A hand-rolled INSERT can build a row production
// cannot produce — a `removed` account still carrying a balance is exactly the shape two frontend
// tests asserted against this round — and a test against an impossible row proves nothing.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

func TestAccountRepository_LastKnownBalance_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	repo := NewAccountRepository(tx, newTestFieldEncryptor(t))
	snapshots := NewBalanceSnapshotRepository(tx)

	userID := uuid.New()
	linkedItemID := uuid.New()
	userAccountID := uuid.New()
	prefix := "lastknown-" + userID.String()

	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+prefix, prefix+"@example.com")
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')`, linkedItemID, userID, prefix+"-item")
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `
		INSERT INTO user_accounts (id, user_id, institution_id, primary_type)
		VALUES ($1, $2, $3, 'depository')`, userAccountID, userID, prefix+"-inst")
	require.NoError(t, err)

	uaID := userAccountID.String()
	f := func(v float64) *float64 { return &v }

	// Four accounts, one per case the ruling has to keep apart. All start ACTIVE with a real
	// balance, because that is the only state CreateBatch can produce — which is the point.
	const (
		closedWithHistory = "closed-with-history"
		closedNoHistory   = "closed-no-history"
		closedZero        = "closed-zero-balance"
		stillActive       = "still-active"
		// #1591 fix round: a snapshot written AFTER the terminal transition. Nothing serialises the
		// two writes — transactionSyncService reads the account list, the user disconnects, and the
		// sync's snapshot pass then writes a row dated after the closure. The caption says "before
		// it closed"; the figure would be from after.
		closedThenSnapshotted = "closed-then-snapshotted"
		// The same race on an account with no earlier snapshot at all: the ONLY row on record is
		// post-closure, so there is nothing legitimate to show and both columns must stay NULL.
		closedOnlyAfter = "closed-only-after"
		// The boundary itself: a snapshot stamped in the SAME instant as the transition. Without
		// this case `<` and `<=` are indistinguishable and the bound could tighten by a tick
		// unnoticed.
		closedAtTheInstant = "closed-at-the-instant"
	)
	// IDs are assigned HERE rather than read back from the slice afterwards: CreateBatch encrypts
	// `Name` in place (encryptAccountPIIFields mutates the element), so the post-call slice keys on
	// ciphertext. Reads decrypt again, which is why the assertions below can still look accounts up
	// by their plaintext names.
	byName := map[string]string{
		closedWithHistory:     uuid.New().String(),
		closedNoHistory:       uuid.New().String(),
		closedZero:            uuid.New().String(),
		stillActive:           uuid.New().String(),
		closedThenSnapshotted: uuid.New().String(),
		closedOnlyAfter:       uuid.New().String(),
		closedAtTheInstant:    uuid.New().String(),
	}
	accounts := []domain.Account{
		{ID: byName[closedWithHistory], UserID: userID.String(), LinkedItemID: linkedItemID.String(), UserAccountID: &uaID,
			PlaidAccountID: prefix + "-1", Name: closedWithHistory, Type: "depository",
			CurrentBalance: f(4321.00), AvailableBalance: f(4300.00), CurrencyCode: "USD"},
		{ID: byName[closedNoHistory], UserID: userID.String(), LinkedItemID: linkedItemID.String(), UserAccountID: &uaID,
			PlaidAccountID: prefix + "-2", Name: closedNoHistory, Type: "depository",
			CurrentBalance: f(999.00), AvailableBalance: f(999.00), CurrencyCode: "USD"},
		{ID: byName[closedZero], UserID: userID.String(), LinkedItemID: linkedItemID.String(), UserAccountID: &uaID,
			PlaidAccountID: prefix + "-3", Name: closedZero, Type: "depository",
			CurrentBalance: f(0), AvailableBalance: f(0), CurrencyCode: "USD"},
		{ID: byName[stillActive], UserID: userID.String(), LinkedItemID: linkedItemID.String(), UserAccountID: &uaID,
			PlaidAccountID: prefix + "-4", Name: stillActive, Type: "depository",
			CurrentBalance: f(777.00), AvailableBalance: f(777.00), CurrencyCode: "USD"},
		{ID: byName[closedThenSnapshotted], UserID: userID.String(), LinkedItemID: linkedItemID.String(), UserAccountID: &uaID,
			PlaidAccountID: prefix + "-5", Name: closedThenSnapshotted, Type: "depository",
			CurrentBalance: f(2000.00), AvailableBalance: f(2000.00), CurrencyCode: "USD"},
		{ID: byName[closedOnlyAfter], UserID: userID.String(), LinkedItemID: linkedItemID.String(), UserAccountID: &uaID,
			PlaidAccountID: prefix + "-6", Name: closedOnlyAfter, Type: "depository",
			CurrentBalance: f(1500.00), AvailableBalance: f(1500.00), CurrencyCode: "USD"},
		{ID: byName[closedAtTheInstant], UserID: userID.String(), LinkedItemID: linkedItemID.String(), UserAccountID: &uaID,
			PlaidAccountID: prefix + "-7", Name: closedAtTheInstant, Type: "depository",
			CurrentBalance: f(3333.33), AvailableBalance: f(3333.33), CurrencyCode: "USD"},
	}
	require.NoError(t, repo.CreateBatch(ctx, userID.String(), accounts))

	now := time.Now().UTC()
	snap := func(accountID string, amount *float64, at time.Time) {
		t.Helper()
		require.NoError(t, snapshots.Create(ctx, &domain.BalanceSnapshot{
			AccountID: accountID, UserAccountID: &uaID, UserID: userID.String(),
			CurrentBalance: amount, AvailableBalance: amount,
			CurrencyCode: "USD", SnapshotAt: at, Source: domain.SnapshotSourcePlaidWebhook,
		}))
	}

	// TWO snapshots on the first account, written OLDEST LAST. If the lateral's ORDER BY were
	// dropped or reversed, `LIMIT 1` would return the 30-day-old row and this test would say so —
	// insertion order deliberately does not agree with recency, so a query that just takes "some
	// row" cannot pass by luck.
	snap(byName[closedWithHistory], f(4321.00), now.AddDate(0, 0, -12))
	snap(byName[closedWithHistory], f(1111.11), now.AddDate(0, 0, -30))
	// A genuine recorded ZERO. This is the row that makes NULL-vs-zero load-bearing: an account
	// emptied before it was closed really did hold $0.00 on a real date, and "we never recorded
	// one" must not render the same way.
	snap(byName[closedZero], f(0), now.AddDate(0, 0, -3))
	// The ACTIVE account has history too — so "active accounts carry no last-known balance" is
	// proved by the status predicate, not by an absent fixture.
	snap(byName[stillActive], f(555.55), now.AddDate(0, 0, -2))
	// The last balance anyone confirmed while the account was still live.
	snap(byName[closedThenSnapshotted], f(2000.00), now.AddDate(0, 0, -10))
	// closedNoHistory and closedOnlyAfter get nothing before closure, deliberately.

	// Close five of the six through the production transition, which stamps removed_at and fires
	// the account_status_history trigger — the interval the lateral now reads its bound from.
	for _, name := range []string{closedWithHistory, closedNoHistory, closedZero, closedThenSnapshotted, closedOnlyAfter, closedAtTheInstant} {
		require.NoError(t, repo.UpdateStatus(ctx, byName[name], userID.String(), domain.AccountStatusRemoved, nil))
	}

	// ── THE RACE, after the fact. An in-flight sync holding a stale account list writes a snapshot
	// for an account that has since been disconnected. Postgres accepts it; nothing in the write
	// path knows the account closed a moment ago. These rows are real and must be retained (#1569)
	// — they simply are not "the last known balance BEFORE it closed".
	// Dated ONE MILLISECOND after the recorded transition, read back from the interval the trigger
	// actually wrote. A hand-picked "an hour from now" would prove only that a far-future row is
	// excluded; the boundary is where a `<` / `<=` slip or a missing bound would hide.
	closedAt := func(accountID string) time.Time {
		t.Helper()
		var at time.Time
		require.NoError(t, tx.QueryRow(ctx, `
			SELECT valid_from FROM account_status_history
			WHERE account_id = $1 AND valid_to IS NULL`, accountID).Scan(&at))
		return at
	}
	snap(byName[closedThenSnapshotted], f(8888.88), closedAt(byName[closedThenSnapshotted]).Add(time.Millisecond))
	snap(byName[closedOnlyAfter], f(7777.77), closedAt(byName[closedOnlyAfter]).Add(time.Millisecond))
	// Exactly ON the boundary: kept, per Decision 4's `<=`.
	snap(byName[closedAtTheInstant], f(3333.33), closedAt(byName[closedAtTheInstant]))

	get := func(t *testing.T, opts AccountFilterOptions) map[string]domain.Account {
		t.Helper()
		opts.AllStatuses = true
		rows, err := repo.GetAccountsWithInstitutionFiltered(ctx, userID.String(), opts)
		require.NoError(t, err)
		out := map[string]domain.Account{}
		for _, a := range rows {
			out[a.Name] = a
		}
		require.Len(t, out, 7, "fixture: all seven accounts must come back, or the assertions below are vacuous")
		return out
	}

	t.Run("opted in", func(t *testing.T) {
		got := get(t, AccountFilterOptions{IncludeLastKnownBalance: true})

		// ── Precondition: the CURRENT-side ruling still holds. Without this the whole test could
		// pass against a build that quietly reintroduced accounts.current_balance for closed rows,
		// which is the exact regression #1569 task 21 forbids.
		for _, name := range []string{closedWithHistory, closedNoHistory, closedZero, closedThenSnapshotted, closedOnlyAfter, closedAtTheInstant} {
			require.Nil(t, got[name].CurrentBalance, "%s: a closed account still carries NO current balance", name)
		}
		require.NotNil(t, got[stillActive].CurrentBalance, "fixture: the active account must still have one")

		// ── Case 1: closed, history on record → the NEWEST snapshot, with its instant.
		a := got[closedWithHistory]
		require.NotNil(t, a.LastKnownBalance, "a closed account with snapshots must carry a last-known balance")
		assert.InDelta(t, 4321.00, *a.LastKnownBalance, 0.001,
			"must be the NEWEST snapshot (4321.00 at -12d), not the oldest (1111.11 at -30d)")
		require.NotNil(t, a.LastKnownBalanceAt, "the amount is meaningless without its instant")
		assert.WithinDuration(t, now.AddDate(0, 0, -12), *a.LastKnownBalanceAt, time.Minute)

		// ── Case 2: closed, NO history → BOTH absent. Not zero, not a date with nothing to label.
		b := got[closedNoHistory]
		assert.Nil(t, b.LastKnownBalance, "no snapshot on record must be NULL, never 0 — see Decision 3")
		assert.Nil(t, b.LastKnownBalanceAt, "and no instant either; a lone date is the caption T7 removed")

		// ── Case 3: closed, a real recorded ZERO → present, and zero. The pair that would collapse
		// if anyone COALESCEd the NULL case to 0.
		c := got[closedZero]
		require.NotNil(t, c.LastKnownBalance, "a recorded 0.00 is a FACT and must survive as one")
		assert.InDelta(t, 0, *c.LastKnownBalance, 0.001)
		require.NotNil(t, c.LastKnownBalanceAt)

		// ── Case 4: ACTIVE → absent, despite having snapshots. A last-known balance is a
		// closed-account affordance; on a live account it would compete with the real one.
		d := got[stillActive]
		assert.Nil(t, d.LastKnownBalance, "an ACTIVE account has a current balance and must carry no last-known one")
		assert.Nil(t, d.LastKnownBalanceAt)

		// ── Case 5: a snapshot written AFTER the terminal transition is not a last-known balance.
		// "Last known balance … as of <date>" claims a figure from before the account closed. The
		// newest row on this account is dated one millisecond after the closure, written by a
		// sync that had not noticed yet. Taking it would print a post-closure number under a
		// pre-closure caption — the exact unlabelled-blend defect the whole #1591 ruling exists to
		// refuse. The row itself is RETAINED (#1569); it is simply not the answer to this question.
		e := got[closedThenSnapshotted]
		require.NotNil(t, e.LastKnownBalance, "the pre-closure snapshot is still on record and must be shown")
		assert.InDelta(t, 2000.00, *e.LastKnownBalance, 0.001,
			"must be the newest snapshot taken BEFORE closure (2000.00 at -10d), not the post-closure 8888.88")
		require.NotNil(t, e.LastKnownBalanceAt)
		assert.WithinDuration(t, now.AddDate(0, 0, -10), *e.LastKnownBalanceAt, time.Minute)

		// ── Case 6: only a post-closure snapshot exists → BOTH absent, via Decision 3's existing
		// both-or-neither NULL path. Not a new empty state, and emphatically not the 7777.77.
		g := got[closedOnlyAfter]
		assert.Nil(t, g.LastKnownBalance,
			"a snapshot dated after the closure is not a last-known balance; both columns stay NULL")
		assert.Nil(t, g.LastKnownBalanceAt)

		// ── Case 7: a snapshot stamped in the SAME instant as the transition is kept. The interval
		// is half-open, but a measurement simultaneous with the closure is a measurement of the
		// account as it closed. This is the only case that can tell `<=` from `<`.
		h := got[closedAtTheInstant]
		require.NotNil(t, h.LastKnownBalance, "a snapshot AT the transition instant is a pre-closure fact")
		assert.InDelta(t, 3333.33, *h.LastKnownBalance, 0.001)
		require.NotNil(t, h.LastKnownBalanceAt)
	})

	t.Run("not opted in", func(t *testing.T) {
		// The opt-in is real, not decorative: the three callers that share this read and never
		// render an account to a person must see exactly what they saw before #1591.
		got := get(t, AccountFilterOptions{})
		assert.Nil(t, got[closedWithHistory].LastKnownBalance,
			"IncludeLastKnownBalance defaults to false and must actually withhold the field")
		assert.Nil(t, got[closedWithHistory].LastKnownBalanceAt)
		require.Nil(t, got[closedWithHistory].CurrentBalance, "and the current-side ruling is unaffected either way")
	})

	t.Run("refused on an as-of read", func(t *testing.T) {
		// Decision 2(b): "the newest snapshot on record" is not an as-of answer. Refused loudly,
		// because an option that is silently ignored is indistinguishable from one that works.
		asOf := now.AddDate(0, 0, -20)
		_, err := repo.GetAccountsWithInstitutionFiltered(ctx, userID.String(), AccountFilterOptions{
			AsOf: &asOf, IncludeLastKnownBalance: true,
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrAccountFilterLastKnownAsOfConflict),
			"combining AsOf with IncludeLastKnownBalance must be refused, got %v", err)
	})
}

// TestAccountRepository_LastKnownBalance_AcrossARelink_Integration is the case the fixture above
// structurally cannot reach: it closes every account exactly ONCE and never restores one, so a
// second live period never exists (#1569 round 6, G-D).
//
// ⚠ THE HOLE. Decision 4 bounds `snapshot_at` only from ABOVE — at the valid_from of the OPEN
// non-active interval, i.e. the MOST RECENT closure. After active → removed → active → removed,
// everything before the SECOND removal satisfies that bound, including the post-first-closure race
// row Decision 4 exists specifically to exclude. If the second live period recorded no snapshot of
// its own, that row — a measurement taken while the account was NOT live — became "the last known
// balance before it closed". Exactly the blend the three-decision note was written to prevent.
//
// ⚠ THE FIX IS NOT THE OBVIOUS ONE, and this test is why. The narrow repair is a lower bound at the
// last ACTIVE interval's valid_from. It kills the race row — and it also kills every legitimate
// measurement taken during an EARLIER live period, which on a re-linked-and-re-closed account is
// usually the only history there is. On the one surface whose thesis is that history is retained,
// answering "we have nothing" when a real, dated, retained figure exists is the wrong direction.
// So the bound is per-INTERVAL instead: a snapshot is admissible unless it falls strictly inside
// some non-active interval. `notDuringAClosedInterval` in account_liveness_sql.go carries the
// argument; the two cases below are what separate the two candidate fixes, and the second one
// FAILS under the narrow repair.
//
// EVERY TRANSITION GOES THROUGH THE PRODUCTION STATUS PATH — MarkRemovedByProvider and
// RestoreProviderRemovedAccount, the only pair that can legitimately produce active → removed →
// active. A hand-set `status` column would bypass record_account_status_change entirely and leave
// account_status_history with no second interval at all, making the boundary this test is about
// untestable while still going green.
func TestAccountRepository_LastKnownBalance_AcrossARelink_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	repo := NewAccountRepository(tx, newTestFieldEncryptor(t))
	snapshots := NewBalanceSnapshotRepository(tx)

	userID := uuid.New()
	linkedItemID := uuid.New()
	userAccountID := uuid.New()
	prefix := "relink-lkb-" + userID.String()

	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+prefix, prefix+"@example.com")
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')`, linkedItemID, userID, prefix+"-item")
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `
		INSERT INTO user_accounts (id, user_id, institution_id, primary_type)
		VALUES ($1, $2, $3, 'depository')`, userAccountID, userID, prefix+"-inst")
	require.NoError(t, err)

	uaID := userAccountID.String()
	f := func(v float64) *float64 { return &v }

	const (
		// Nothing at all recorded during the FIRST live period, so the only candidate the old
		// upper bound admits is the race row. This is the defect in its purest form.
		raceOnly = "relink-race-only"
		// A real measurement during the first live period, THEN the race row. Separates the two
		// candidate fixes: the narrow lower bound answers NULL here, discarding retained history
		// that is genuinely a pre-closure figure; the per-interval bound answers 1200.00.
		realThenRace = "relink-real-then-race"
	)
	byName := map[string]string{
		raceOnly:     uuid.New().String(),
		realThenRace: uuid.New().String(),
	}
	require.NoError(t, repo.CreateBatch(ctx, userID.String(), []domain.Account{
		{ID: byName[raceOnly], UserID: userID.String(), LinkedItemID: linkedItemID.String(), UserAccountID: &uaID,
			PlaidAccountID: prefix + "-1", Name: raceOnly, Type: "depository",
			CurrentBalance: f(600.00), AvailableBalance: f(600.00), CurrencyCode: "USD"},
		{ID: byName[realThenRace], UserID: userID.String(), LinkedItemID: linkedItemID.String(), UserAccountID: &uaID,
			PlaidAccountID: prefix + "-2", Name: realThenRace, Type: "depository",
			CurrentBalance: f(1200.00), AvailableBalance: f(1200.00), CurrencyCode: "USD"},
	}))

	snap := func(accountID string, amount *float64, at time.Time) {
		t.Helper()
		require.NoError(t, snapshots.Create(ctx, &domain.BalanceSnapshot{
			AccountID: accountID, UserAccountID: &uaID, UserID: userID.String(),
			CurrentBalance: amount, AvailableBalance: amount,
			CurrencyCode: "USD", SnapshotAt: at, Source: domain.SnapshotSourcePlaidWebhook,
		}))
	}
	openIntervalStart := func(accountID string) time.Time {
		t.Helper()
		var at time.Time
		require.NoError(t, tx.QueryRow(ctx, `
			SELECT valid_from FROM account_status_history
			WHERE account_id = $1 AND valid_to IS NULL`, accountID).Scan(&at))
		return at
	}

	// ── FIRST LIVE PERIOD. Only realThenRace records anything.
	firstLiveSnapshotAt := time.Now().UTC().AddDate(0, 0, -20)
	snap(byName[realThenRace], f(1200.00), firstLiveSnapshotAt)

	// ── FIRST CLOSURE. The provider stopped reporting the account, which is the ONLY cause
	// RestoreProviderRemovedAccount will later undo.
	for _, name := range []string{raceOnly, realThenRace} {
		require.NoError(t, repo.MarkRemovedByProvider(ctx, byName[name], userID.String()))
	}

	// ── THE RACE, one millisecond after the first closure: an in-flight sync holding a stale
	// account list. Retained (#1569), and not a live-account measurement.
	for _, name := range []string{raceOnly, realThenRace} {
		snap(byName[name], f(9999.99), openIntervalStart(byName[name]).Add(time.Millisecond))
	}

	// ── RE-LINK. The bank started reporting the account again.
	for _, name := range []string{raceOnly, realThenRace} {
		require.NoError(t, repo.RestoreProviderRemovedAccount(ctx, byName[name], userID.String()))
	}
	// ── SECOND LIVE PERIOD records NOTHING. That is the whole point: with a snapshot here the
	// ORDER BY would pick it and the missing bound would stay hidden.

	// ── SECOND CLOSURE.
	for _, name := range []string{raceOnly, realThenRace} {
		require.NoError(t, repo.MarkRemovedByProvider(ctx, byName[name], userID.String()))
	}

	// Fixture precondition: the history really does have four intervals per account, or the shapes
	// above never happened and every assertion below is vacuous.
	for _, name := range []string{raceOnly, realThenRace} {
		var intervals int
		require.NoError(t, tx.QueryRow(ctx,
			`SELECT COUNT(*) FROM account_status_history WHERE account_id = $1`,
			byName[name]).Scan(&intervals))
		require.Equal(t, 4, intervals,
			"%s: expected created/removed/active/removed intervals; the transitions did not happen", name)
	}

	rows, err := repo.GetAccountsWithInstitutionFiltered(ctx, userID.String(),
		AccountFilterOptions{AllStatuses: true, IncludeLastKnownBalance: true})
	require.NoError(t, err)
	got := map[string]domain.Account{}
	for _, a := range rows {
		got[a.Name] = a
	}
	require.Len(t, got, 2, "fixture: both accounts must come back")

	t.Run("a post-closure race row is not resurrected by a later re-link", func(t *testing.T) {
		a := got[raceOnly]
		require.Nil(t, a.CurrentBalance, "precondition: a closed account still carries no current balance")
		assert.Nil(t, a.LastKnownBalance,
			"9999.99 was measured while the account was NOT live; a second live period does not make it a "+
				"pre-closure figure, and nothing else was ever recorded")
		assert.Nil(t, a.LastKnownBalanceAt, "both or neither — a lone date is the caption T7 removed")
	})

	t.Run("a genuine first-period measurement survives the re-link", func(t *testing.T) {
		// ⚠ This is the case a naive lower bound at the last ACTIVE interval's valid_from would
		// get WRONG, answering NULL and throwing away retained history that is a real, dated,
		// pre-closure figure. Two candidate fixes, one observable difference.
		b := got[realThenRace]
		require.NotNil(t, b.LastKnownBalance,
			"1200.00 was measured while the account was live and is still on record")
		assert.InDelta(t, 1200.00, *b.LastKnownBalance, 0.001,
			"must be the live-period figure, not the 9999.99 race row")
		require.NotNil(t, b.LastKnownBalanceAt)
		assert.WithinDuration(t, firstLiveSnapshotAt, *b.LastKnownBalanceAt, time.Minute)
	})
}
