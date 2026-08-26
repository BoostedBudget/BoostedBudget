//go:build integration

package repository

// Integration tests for the ENUMERATING read over the population Issue #1569 T1's notice gate is
// holding back — Issue #1584 (Round 5 gap task G5).
//
// THE DEFECT THIS CLOSES. T1 made the hard-delete sweep require both promised pre-deletion notice
// markers (retentionNoticesDeliveredPredicate), and BOTH notice queries bound their population by
// `retention_expires_at > NOW()` (GetUsersNeedingRetention30dNotice and its 7d sibling). Once a
// cancelled user's clock runs out with a marker still NULL they are in a terminal state: the sweep
// refuses to delete them, and no notice query can ever select them to fix that.
//
// ⚠ WHAT WAS ALREADY TRUE, so nobody re-derives the ticket's premise from its title.
// #1584 states that "no existing query can SELECT these users at all". That was HALF stale when it
// was filed: CountRetentionDeletionsWithheld has counted them, by reason, since T1. What did not
// exist — and what these tests pin — is a read that ENUMERATES them, so an operator has rows to act
// on rather than a number to worry about.
//
// ⚠ SCOPE. This is the enumeration and its admin visibility ONLY. Nothing here sends a notice,
// extends retention_expires_at, or waives a notice: the drain policy (send late / waive / notify
// out-of-band) is a product decision parked until after go-live, and a test that asserted one would
// be asserting a decision nobody has taken.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestRetentionWithheldEnumeration -tags integration -p 1 -count=1 -v

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// collectRetentionWithheldPage walks every page of the enumeration to exhaustion and returns the
// rows keyed by user id. Walking to exhaustion (rather than reading one page) is what makes a
// "not enumerated" assertion honest: a row that merely sorts late is still enumerated eventually.
func collectRetentionWithheld(t *testing.T, ctx context.Context, repo *profileRepository) map[string]RetentionWithheldAccount {
	t.Helper()
	seen := map[string]RetentionWithheldAccount{}
	var cursor *RetentionSweepCursor
	for {
		page, err := repo.GetRetentionDeletionsWithheldPage(ctx, cursor, 200)
		require.NoError(t, err)
		if len(page) == 0 {
			return seen
		}
		for _, row := range page {
			seen[row.UserID] = row
		}
		last := page[len(page)-1]
		cursor = &RetentionSweepCursor{ExpiresAt: last.RetentionExpiresAt, UserID: last.UserID}
		if len(page) < 200 {
			return seen
		}
	}
}

// TestRetentionWithheldEnumeration_Integration_ReturnsOnlyTheTerminalPopulation is the failing-first
// proof for #1584, and it asserts BOTH directions in one run so neither can be satisfied by a query
// that simply returns everything or nothing:
//
//	(a) expired clock, NEITHER marker recorded          → enumerated (the terminal state);
//	(b) expired clock, only the 30d marker recorded     → enumerated (BOTH are promised);
//	(c) expired clock, BOTH markers recorded            → NOT enumerated (the sweep will delete it);
//	(d) clock STILL RUNNING, neither marker recorded    → NOT enumerated (the notice sweep can still
//	                                                      reach this user — the healthy majority);
//	(e) expired clock, neither marker, ACTIVE COMP      → NOT enumerated (noManualOverrideAtAllPredicate
//	                                                      excludes a comped account from the sweep, so
//	                                                      it is not being withheld BY the gate).
//
// (c), (d) and (e) are the mutation-checks: each one reddens a different missing clause.
func TestRetentionWithheldEnumeration_Integration_ReturnsOnlyTheTerminalPopulation(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	sent30 := noticeGateEpoch.Add(-30 * 24 * time.Hour)
	sent7 := noticeGateEpoch.Add(-7 * 24 * time.Hour)

	neither := seedNoticeGateUser(t, ctx, pool, repo, "enum_neither", nil, nil)
	only30 := seedNoticeGateUser(t, ctx, pool, repo, "enum_only30", &sent30, nil)
	warned := seedNoticeGateUser(t, ctx, pool, repo, "enum_warned", &sent30, &sent7)
	pending := createRetentionFixtureUser(t, ctx, pool, repo,
		fmt.Sprintf("enum_pending_%d", time.Now().UnixNano()), time.Now().Add(20*24*time.Hour), nil, nil)

	comp := "premium"
	compEndsAt := time.Now().Add(365 * 24 * time.Hour)
	comped := createRetentionFixtureUser(t, ctx, pool, repo,
		fmt.Sprintf("enum_comped_%d", time.Now().UnixNano()), noticeGateEpoch, &comp, &compEndsAt)

	withheld := collectRetentionWithheld(t, ctx, repo)

	_, hasNeither := withheld[neither.ID]
	assert.True(t, hasNeither,
		"an expired cancelled account warned ZERO times is the terminal notice_impossible_window_passed state — it must be enumerable or nobody can act on it")
	_, hasOnly30 := withheld[only30.ID]
	assert.True(t, hasOnly30,
		"the 7-day final warning is a separate promise; an account that got only the 30-day notice is withheld exactly as hard and must be enumerated too")
	_, hasWarned := withheld[warned.ID]
	assert.False(t, hasWarned,
		"an account that received BOTH promised notices is a live deletion candidate, not a withheld one — enumerating it would put ordinary work on an operator's exception list")
	_, hasPending := withheld[pending.ID]
	assert.False(t, hasPending,
		"an account whose retention clock is still RUNNING is reachable by the notice sweep; enumerating it would list every ordinary cancellation as an exception")
	_, hasComped := withheld[comped.ID]
	assert.False(t, hasComped,
		"a comped account is excluded from the sweep by noManualOverrideAtAllPredicate, so the notice gate is not what is holding it — enumerating it would misattribute the reason")
}

// TestRetentionWithheldEnumeration_Integration_AgreesWithTheCountAndIsDisjointFromTheSweep pins the
// two relationships that make the enumeration trustworthy as an operator surface:
//
//   - it enumerates EXACTLY the population CountRetentionDeletionsWithheld reports. The two are
//     spliced from one predicate constant (retentionWithheldPopulationPredicate) precisely so a
//     dashboard's "N withheld" and the list beneath it can never disagree — the drift class that
//     produced #1440 T0-retention;
//   - it is DISJOINT from the sweep's own candidate set, and their union covers every seeded
//     expired account. An expired account is either being deleted or being withheld; there is no
//     third bucket, and no account may be in both.
func TestRetentionWithheldEnumeration_Integration_AgreesWithTheCountAndIsDisjointFromTheSweep(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	sent30 := noticeGateEpoch.Add(-30 * 24 * time.Hour)
	sent7 := noticeGateEpoch.Add(-7 * 24 * time.Hour)

	before, err := repo.CountRetentionDeletionsWithheld(ctx)
	require.NoError(t, err)
	withheldBefore := collectRetentionWithheld(t, ctx, repo)

	a := seedNoticeGateUser(t, ctx, pool, repo, "parity_a", nil, nil)
	b := seedNoticeGateUser(t, ctx, pool, repo, "parity_b", &sent30, nil)
	c := seedNoticeGateUser(t, ctx, pool, repo, "parity_c", nil, &sent7)
	deletable := seedNoticeGateUser(t, ctx, pool, repo, "parity_deletable", &sent30, &sent7)

	after, err := repo.CountRetentionDeletionsWithheld(ctx)
	require.NoError(t, err)
	withheldAfter := collectRetentionWithheld(t, ctx, repo)

	assert.Equal(t, after.WithheldNoticeMissing-before.WithheldNoticeMissing,
		len(withheldAfter)-len(withheldBefore),
		"the enumeration and the count must move by the same amount — an operator list that disagrees with the number above it is worse than either alone")

	candidates := collectExpiredRetentionCandidateIDs(t, ctx, repo)
	for _, seeded := range []*domain.User{a, b, c, deletable} {
		_, enumerated := withheldAfter[seeded.ID]
		assert.NotEqual(t, enumerated, candidates[seeded.ID],
			"expired account %s must be EITHER a deletion candidate OR enumerated as withheld, never both and never neither", seeded.ID)
	}
	assert.True(t, candidates[deletable.ID],
		"the fully-warned account must still be a live deletion candidate — otherwise this test's disjointness assertion is satisfied by an empty sweep")
}

// TestRetentionWithheldEnumeration_Integration_ReportsAnAddresslessAccountTheCountCannotSee is the
// FOURTH withholding reason, surfaced for the first time.
//
// ⚠ WHY THE COUNT CANNOT DO THIS AND THIS READ CAN. CountRetentionDeletionsWithheld's doc comment
// explains, correctly, that no SQL predicate can single out a withheld user with no address on
// file: users.email holds CIPHERTEXT and FieldEncryptor.EncryptString returns "ENC$"+ct even for
// the empty string, so an equals-empty-string test over that column matches nothing; and
// email_blind_idx — the one column that could discriminate — carries migration 000061's FULL
// UNIQUE index, capping that population at
// one row database-wide and making it worthless as a Prometheus label. All of that stays true.
// This read is different in KIND, not in cleverness: it goes through queryUsers, which DECRYPTS
// email for every row it returns, so it observes the address post-decryption at exactly the point
// canSendRetentionNotice does (queue/jobs/retention_cleanup_check.go: `user.Email == ""`). It
// reports the state per row rather than as a labelled aggregate, so the UNIQUE-index cardinality
// argument does not apply to it either.
//
// ⚠ THE FIXTURE GOES THROUGH THE PRODUCTION WRITE PATH, DELIBERATELY. An earlier version of the
// no-address test on this branch wrote a bare empty string straight into users.email; that row
// shape cannot occur in production, the predicate under test matched nothing, and the test passed
// anyway (see
// TestRetentionNoticeGate_Integration_WithholdingCountsSeparatePendingFromImpossible's own note).
// Here the user is created by CreateUserTx with Email: "", so the column holds a REAL
// EncryptString("") ciphertext — the exact bytes production would store for an addressless user —
// and the empty string is recovered only by actually decrypting it.
func TestRetentionWithheldEnumeration_Integration_ReportsAnAddresslessAccountTheCountCannotSee(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	// ⚠ unique_users_email_blind is a FULL unique index on email_blind_idx, and BlindIndex("") is
	// one fixed value, so at most one addressless row can exist in the whole database at a time.
	// Clear any leaked one before seeding, or this fixture fails on a duplicate rather than on its
	// own assertion.
	_, err := pool.Exec(ctx, `DELETE FROM users WHERE email_blind_idx = $1`, enc.BlindIndex(""))
	require.NoError(t, err)

	addressless := &domain.User{
		ClerkUserID:        fmt.Sprintf("clerk_retention_addressless_%d", time.Now().UnixNano()),
		Email:              "",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "cancelled",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, addressless))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() { pool.Exec(ctx, "DELETE FROM users WHERE id = $1", addressless.ID) }) //nolint:errcheck

	// Proves the fixture is the production shape and not the dead one: the stored column is
	// ciphertext, NOT a bare ''. If this ever reads '' the fixture has bypassed encryption again
	// and every assertion below is meaningless.
	var storedEmail string
	require.NoError(t, pool.QueryRow(ctx, `SELECT email FROM users WHERE id = $1`, addressless.ID).Scan(&storedEmail))
	require.NotEqual(t, "", storedEmail,
		"users.email must hold EncryptString(\"\") ciphertext — a bare '' means this fixture bypassed the encryption path, which is how a dead predicate shipped green on this branch before")

	_, err = pool.Exec(ctx, `UPDATE users SET retention_expires_at = $1 WHERE id = $2`, noticeGateEpoch, addressless.ID)
	require.NoError(t, err)

	addressed := seedNoticeGateUser(t, ctx, pool, repo, "addressed", nil, nil)

	withheld := collectRetentionWithheld(t, ctx, repo)

	addresslessRow, found := withheld[addressless.ID]
	require.True(t, found, "an addressless withheld account is still withheld and must be enumerated")
	assert.False(t, addresslessRow.HasEmailOnFile,
		"the enumeration decrypts email, so it can report the one withholding reason CountRetentionDeletionsWithheld provably cannot — an account with no address on file")

	addressedRow, found := withheld[addressed.ID]
	require.True(t, found, "an ordinary withheld account must be enumerated")
	assert.True(t, addressedRow.HasEmailOnFile,
		"an account WITH an address must report true — otherwise HasEmailOnFile is a constant false and reports nothing at all")
}

// seedWithheldForPagination creates n withheld accounts sharing one expiry so the keyset cursor is
// exercised against ties on retention_expires_at, which is exactly where a cursor that keys on the
// timestamp alone loses rows or loops.
func seedWithheldForPagination(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repo ProfileRepository, n int) map[string]bool {
	t.Helper()
	ids := map[string]bool{}
	for i := 0; i < n; i++ {
		u := seedNoticeGateUser(t, ctx, pool, repo, fmt.Sprintf("page_%d", i), nil, nil)
		_, err := pool.Exec(ctx, `UPDATE users SET retention_expires_at = $1 WHERE id = $2`, noticeGateEpoch, u.ID)
		require.NoError(t, err)
		ids[u.ID] = true
	}
	return ids
}

// TestRetentionWithheldEnumeration_Integration_KeysetPaginationCoversTiedExpiries proves the page
// walk reaches every row when several share one retention_expires_at. The tie is the whole point:
// ORDER BY retention_expires_at alone with a `> $1` cursor silently drops every row after the first
// of each tied group, and the population this read exists for is a BACKLOG — rows seeded in bulk by
// one event, therefore very likely to tie.
func TestRetentionWithheldEnumeration_Integration_KeysetPaginationCoversTiedExpiries(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	seeded := seedWithheldForPagination(t, ctx, pool, repo, 5)

	var cursor *RetentionSweepCursor
	seen := map[string]bool{}
	pages := 0
	for {
		page, err := repo.GetRetentionDeletionsWithheldPage(ctx, cursor, 2)
		require.NoError(t, err)
		pages++
		require.LessOrEqual(t, pages, 100, "page walk failed to terminate — the cursor is not advancing")
		if len(page) == 0 {
			break
		}
		for _, row := range page {
			assert.False(t, seen[row.UserID], "row %s was returned on two different pages — the cursor is not strict", row.UserID)
			seen[row.UserID] = true
		}
		last := page[len(page)-1]
		cursor = &RetentionSweepCursor{ExpiresAt: last.RetentionExpiresAt, UserID: last.UserID}
		if len(page) < 2 {
			break
		}
	}

	for id := range seeded {
		assert.True(t, seen[id], "withheld account %s was never returned by the paged walk despite sharing an expiry with four others", id)
	}
}
