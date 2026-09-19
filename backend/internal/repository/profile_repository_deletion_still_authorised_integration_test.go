//go:build integration

package repository

// Integration tests for DeletionStillAuthorised (Issue #1413 round-13/#1440 follow-up, commits
// 9cfbe5a8..50f1b12d). ExecuteDeletion consults this read-only recheck immediately before each of
// four irreversible external calls (the Plaid leg, each individual Plaid item, the Paddle
// subscription cancel, and the Paddle customer anonymisation) so a CancelDeletion (or a
// resubscription, for the retention kind) that lands DURING that cleanup window is caught before
// the next external call fires, not only afterwards by DeleteUserCascade's own in-transaction
// guard.
//
// pgxmock cannot validate this method at all: its SQL is spliced together from
// deletionClaimPredicateFor(kind) plus DeleteUserCascade's own occurrence pin
// (`deletion_requested_at IS NOT DISTINCT FROM $2`), and the whole point is how that spliced text
// behaves against LIVE row state and NULL semantics — exactly the class of defect pgxmock is blind
// to. Only a real PostgreSQL 17 database can prove it.
//
// Every fixture in this file is seeded through the PRODUCTION writers this method's authorisation
// rests on (CreateUserTx, SetRetentionDates, MarkForDeletion, CancelDeletion,
// UpdateSubscriptionFromPaddle, ClearRetentionDates) — never a hand-crafted row for the state under
// test — because two defects in this same round slipped past a test that asserted a state no
// production writer actually produces.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestDeletionStillAuthorised_Integration -tags integration -v

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// DeletionKindUserRequested
// ---------------------------------------------------------------------------

// TestDeletionStillAuthorised_Integration_UserRequested_Authorised_ReturnsTrue is the assertion
// that makes every other test in this file meaningful (per the task brief): without a proven
// TRUE case, every "returns false" test below would pass just as well against a
// DeletionStillAuthorised that returned false unconditionally.
func TestDeletionStillAuthorised_Integration_UserRequested_Authorised_ReturnsTrue(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	requestedAt := time.Now().Add(-24 * time.Hour) // elapsed grace period
	user := createClaimForDeletionFixtureUser(t, ctx, pool, repo, suffix, false, &requestedAt, nil, nil)

	// Read back the EXACT committed deletion_requested_at value — the same thing ExecuteDeletion
	// would have observed right after ClaimForDeletion, before any external cleanup began.
	expected := readDeletionRequestedAt(t, ctx, pool, user.ID)
	require.NotNil(t, expected)

	authorised, err := repo.DeletionStillAuthorised(ctx, user.ID, DeletionKindUserRequested, expected)
	require.NoError(t, err)
	assert.True(t, authorised, "an outstanding, elapsed deletion request must still authorise the recheck")
}

// TestDeletionStillAuthorised_Integration_UserRequested_Cancelled_StaleObservedValue_ReturnsFalse
// is the exact scenario DeletionStillAuthorised exists to prevent: ExecuteDeletion observed a
// non-NULL deletion_requested_at at claim time (capturing it into expectedDeletionRequestedAt),
// then — mid Paddle/Plaid cleanup — the user calls CancelDeletion. A caller re-checking with the
// STALE value it captured before cancellation must get false.
func TestDeletionStillAuthorised_Integration_UserRequested_Cancelled_StaleObservedValue_ReturnsFalse(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	requestedAt := time.Now().Add(-24 * time.Hour)
	user := createClaimForDeletionFixtureUser(t, ctx, pool, repo, suffix, false, &requestedAt, nil, nil)
	expected := readDeletionRequestedAt(t, ctx, pool, user.ID)

	authorised, err := repo.DeletionStillAuthorised(ctx, user.ID, DeletionKindUserRequested, expected)
	require.NoError(t, err)
	require.True(t, authorised, "sanity check: authorised before cancellation")

	// The real CancelDeletion — the user asked to keep their account, mid-cleanup.
	require.NoError(t, repo.CancelDeletion(ctx, user.ID))

	authorised, err = repo.DeletionStillAuthorised(ctx, user.ID, DeletionKindUserRequested, expected)
	require.NoError(t, err)
	assert.False(t, authorised, "a cancelled deletion request must never re-authorise an "+
		"irreversible external call already in flight — this is the data-loss scenario the "+
		"method exists to prevent")
}

// TestDeletionStillAuthorised_Integration_UserRequested_Cancelled_CurrentNullValue_ReturnsFalse
// isolates the KIND PREDICATE's own enforcement (userRequestedDeletionClaimPredicate's
// `deletion_requested_at IS NOT NULL` / `deletion_scheduled_at <= NOW()` clauses) from the
// occurrence pin: it passes the CURRENT (post-cancel, NULL) value as expectedDeletionRequestedAt,
// so the pin matches (NULL IS NOT DISTINCT FROM NULL) and only the kind predicate can deny.
func TestDeletionStillAuthorised_Integration_UserRequested_Cancelled_CurrentNullValue_ReturnsFalse(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	requestedAt := time.Now().Add(-24 * time.Hour)
	user := createClaimForDeletionFixtureUser(t, ctx, pool, repo, suffix, false, &requestedAt, nil, nil)

	require.NoError(t, repo.CancelDeletion(ctx, user.ID))

	// Pass nil — the row's CURRENT (post-cancel) value, matching what a fresh read would see.
	authorised, err := repo.DeletionStillAuthorised(ctx, user.ID, DeletionKindUserRequested, nil)
	require.NoError(t, err)
	assert.False(t, authorised, "the user-requested predicate itself must reject a NULL "+
		"deletion_requested_at, independent of the occurrence pin (which matches here)")
}

// TestDeletionStillAuthorised_Integration_UserRequested_OccurrencePin_ReRequested_ReturnsFalse is
// the cancel-then-re-request proof: a job/recheck holding the FIRST episode's
// deletion_requested_at must not be fooled by a SECOND episode whose own deletion_scheduled_at has
// also already elapsed. Both episodes independently satisfy the kind predicate; only the
// occurrence pin (`IS NOT DISTINCT FROM`) distinguishes them.
func TestDeletionStillAuthorised_Integration_UserRequested_OccurrencePin_ReRequested_ReturnsFalse(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	requestedAt := time.Now().Add(-24 * time.Hour)
	user := createClaimForDeletionFixtureUser(t, ctx, pool, repo, suffix, false, &requestedAt, nil, nil)
	firstEpisode := readDeletionRequestedAt(t, ctx, pool, user.ID)

	authorised, err := repo.DeletionStillAuthorised(ctx, user.ID, DeletionKindUserRequested, firstEpisode)
	require.NoError(t, err)
	require.True(t, authorised, "sanity check: episode 1 authorised before cancellation")

	// Cancel episode 1, then re-request with a schedule that is ALSO already elapsed (unlike the
	// legacy trial-service path (deleted in #1652), which scheduled 30 days out) — this is deliberate: it makes the
	// kind predicate ALONE authorise episode 2, isolating the pin as the only thing that can catch
	// the substitution.
	require.NoError(t, repo.CancelDeletion(ctx, user.ID))
	require.NoError(t, repo.MarkForDeletion(ctx, user.ID, time.Now().Add(-1*time.Hour)))
	secondEpisode := readDeletionRequestedAt(t, ctx, pool, user.ID)
	require.NotNil(t, secondEpisode)
	require.False(t, firstEpisode.Equal(*secondEpisode), "the two episodes must carry distinct timestamps")

	// Sanity: episode 2 alone, observed correctly, IS authorised — proves the kind predicate would
	// have said yes, so only the pin below is doing the work.
	authorised, err = repo.DeletionStillAuthorised(ctx, user.ID, DeletionKindUserRequested, secondEpisode)
	require.NoError(t, err)
	require.True(t, authorised, "sanity check: episode 2, observed correctly, is independently authorised")

	// The actual proof: a caller still holding episode 1's stale value must be refused.
	authorised, err = repo.DeletionStillAuthorised(ctx, user.ID, DeletionKindUserRequested, firstEpisode)
	require.NoError(t, err)
	assert.False(t, authorised, "a stale caller holding episode 1's deletion_requested_at must not "+
		"be re-authorised by episode 2 — a cancel-then-re-request must not be mistaken for the "+
		"original episode")
}

// ---------------------------------------------------------------------------
// DeletionKindRetention
// ---------------------------------------------------------------------------

// TestDeletionStillAuthorised_Integration_Retention_Authorised_ReturnsTrue is the retention-kind
// counterpart to the user-requested TRUE case above: an ordinary, uncomped, never-requested user
// whose retention clock has expired must still be authorised. This also exercises the NULL-vs-NULL
// occurrence pin: the retention path's expectedDeletionRequestedAt is always nil (it never sets
// the column), and `NULL IS NOT DISTINCT FROM NULL` must be TRUE, not the "unknown" a naive `=`
// comparison would produce.
func TestDeletionStillAuthorised_Integration_Retention_Authorised_ReturnsTrue(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := createClaimForDeletionFixtureUser(t, ctx, pool, repo, suffix, false, nil, nil, nil)

	authorised, err := repo.DeletionStillAuthorised(ctx, user.ID, DeletionKindRetention, nil)
	require.NoError(t, err)
	assert.True(t, authorised, "an uncomped user with an expired retention clock and no deletion "+
		"request must still be authorised")
}

// TestDeletionStillAuthorised_Integration_Retention_OutstandingRequest_ReturnsFalse is the
// retention-kind analogue of "revoked": the user requests their OWN deletion (via the real
// MarkForDeletion) between ExecuteDeletion's claim and this recheck. The retention authorisation
// must not proceed — that user is entitled to their own grace period and cancellation window,
// exactly as retentionDeletionClaimPredicate's own `deletion_requested_at IS NULL` clause and the
// occurrence pin (nil no longer matches the now-non-NULL column) both independently require.
func TestDeletionStillAuthorised_Integration_Retention_OutstandingRequest_ReturnsFalse(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := createClaimForDeletionFixtureUser(t, ctx, pool, repo, suffix, false, nil, nil, nil)

	authorised, err := repo.DeletionStillAuthorised(ctx, user.ID, DeletionKindRetention, nil)
	require.NoError(t, err)
	require.True(t, authorised, "sanity check: authorised before the user requested deletion")

	require.NoError(t, repo.MarkForDeletion(ctx, user.ID, time.Now().Add(30*24*time.Hour)))

	authorised, err = repo.DeletionStillAuthorised(ctx, user.ID, DeletionKindRetention, nil)
	require.NoError(t, err)
	assert.False(t, authorised, "a user who has requested their own deletion must not be swept up "+
		"by the retention authorisation mid-cleanup")
}

// TestDeletionStillAuthorised_Integration_Retention_OverrideGranted_ReturnsFalse proves the
// retention kind's "revoked" case via the mechanism that actually revokes it for this kind: an
// admin grants a manual tier override (comp) between the claim and this recheck.
// noManualOverrideAtAllPredicate ("live or lapsed") must block it.
func TestDeletionStillAuthorised_Integration_Retention_OverrideGranted_ReturnsFalse(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := createClaimForDeletionFixtureUser(t, ctx, pool, repo, suffix, false, nil, nil, nil)

	authorised, err := repo.DeletionStillAuthorised(ctx, user.ID, DeletionKindRetention, nil)
	require.NoError(t, err)
	require.True(t, authorised, "sanity check: authorised before the override was granted")

	// Simulates an admin granting a comp during the external-cleanup window — the same race
	// ClaimForDeletion's own ActiveOverride test closes at claim time; this proves the SAME race is
	// closed again for the recheck between external calls.
	tier := "premium"
	overrideEndsAt := time.Now().Add(24 * time.Hour)
	_, err = pool.Exec(ctx, `UPDATE users SET manual_tier_override = $1, manual_tier_ends_at = $2 WHERE id = $3`,
		tier, overrideEndsAt, user.ID)
	require.NoError(t, err)

	authorised, err = repo.DeletionStillAuthorised(ctx, user.ID, DeletionKindRetention, nil)
	require.NoError(t, err)
	assert.False(t, authorised, "a manual tier override granted mid-cleanup must revoke the "+
		"retention authorisation")
}

// TestDeletionStillAuthorised_Integration_Retention_RetentionClockCleared_ReturnsFalse proves the
// #1440 T0-retention condition this recheck also re-verifies: expiredRetentionClockPredicate's
// `retention_expires_at IS NOT NULL AND retention_expires_at <= NOW()`. Cleared through the real
// production writer, ClearRetentionDates (the same call UpdateSubscriptionFromPaddle's
// reactivation path triggers).
func TestDeletionStillAuthorised_Integration_Retention_RetentionClockCleared_ReturnsFalse(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := createClaimForDeletionFixtureUser(t, ctx, pool, repo, suffix, false, nil, nil, nil)

	authorised, err := repo.DeletionStillAuthorised(ctx, user.ID, DeletionKindRetention, nil)
	require.NoError(t, err)
	require.True(t, authorised, "sanity check: authorised while the retention clock is expired")

	require.NoError(t, repo.ClearRetentionDates(ctx, user.ID))

	authorised, err = repo.DeletionStillAuthorised(ctx, user.ID, DeletionKindRetention, nil)
	require.NoError(t, err)
	assert.False(t, authorised, "a cleared retention clock must not authorise a retention-kind "+
		"hard delete — this is the #1440 T0-retention scenario")
}

// TestDeletionStillAuthorised_Integration_Retention_SubscriptionReactivated_ReturnsFalse is the
// sibling half of expiredRetentionClockPredicate: a resubscription flips subscription_status back
// to 'active' via the real production writer, UpdateSubscriptionFromPaddle — the exact webhook
// path #1440 T0-retention's doc comment describes. Deliberately does NOT also clear
// retention_expires_at (unlike the test above), isolating the STATUS half of the predicate: a
// stale expired clock that survived a best-effort ClearRetentionDates failure must still not
// authorise deletion of an active, paying subscriber.
func TestDeletionStillAuthorised_Integration_Retention_SubscriptionReactivated_ReturnsFalse(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := createClaimForDeletionFixtureUser(t, ctx, pool, repo, suffix, false, nil, nil, nil)

	authorised, err := repo.DeletionStillAuthorised(ctx, user.ID, DeletionKindRetention, nil)
	require.NoError(t, err)
	require.True(t, authorised, "sanity check: authorised while still cancelled")

	require.NoError(t, repo.UpdateSubscriptionFromPaddle(ctx, user.ID, "premium", "active", nil, nil, nil))

	authorised, err = repo.DeletionStillAuthorised(ctx, user.ID, DeletionKindRetention, nil)
	require.NoError(t, err)
	assert.False(t, authorised, "an active subscriber must not be authorised for a retention-kind "+
		"hard delete even if a stale retention_expires_at survives — "+
		"expiredRetentionClockPredicate's status clause must independently block this")
}

// ---------------------------------------------------------------------------
// Kind dispatch / defensive cases
// ---------------------------------------------------------------------------

// TestDeletionStillAuthorised_Integration_UnknownKind_FailsClosed proves deletionClaimPredicateFor
// is consulted here too, and fails CLOSED on an unrecognised kind — an ERROR, never a silent
// false-as-authorised or (worse) true.
func TestDeletionStillAuthorised_Integration_UnknownKind_FailsClosed(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := createClaimForDeletionFixtureUser(t, ctx, pool, repo, suffix, false, nil, nil, nil)

	for _, kind := range []DeletionClaimKind{"", "retention_but_typoed"} {
		authorised, err := repo.DeletionStillAuthorised(ctx, user.ID, kind, nil)
		require.Error(t, err, "an unrecognised kind %q must be an error", string(kind))
		assert.False(t, authorised, "an unrecognised kind %q must never read as authorised", string(kind))
	}
}

// TestDeletionStillAuthorised_Integration_MissingUser_ReturnsFalse proves a non-existent user ID
// simply reports unauthorised rather than erroring — DeleteUserCascade's own guard behaves the
// same way for a row that has vanished.
func TestDeletionStillAuthorised_Integration_MissingUser_ReturnsFalse(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	authorised, err := repo.DeletionStillAuthorised(ctx, "00000000-0000-0000-0000-000000000000", DeletionKindRetention, nil)
	require.NoError(t, err)
	assert.False(t, authorised)
}

// TestDeletionStillAuthorised_Integration_AlreadyDeleted_ReturnsFalse proves is_deleted = TRUE
// excludes the row defensively, mirroring ClaimForDeletion's own AlreadyDeleted test.
func TestDeletionStillAuthorised_Integration_AlreadyDeleted_ReturnsFalse(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := createClaimForDeletionFixtureUser(t, ctx, pool, repo, suffix, true, nil, nil, nil)

	authorised, err := repo.DeletionStillAuthorised(ctx, user.ID, DeletionKindRetention, nil)
	require.NoError(t, err)
	assert.False(t, authorised, "an already-deleted user must never be reported as re-authorised")
}

// readDeletionRequestedAt reads the row's CURRENT deletion_requested_at, the same way a caller of
// DeletionStillAuthorised would have captured it (from ClaimForDeletion's own transaction, in
// production) before proceeding to external cleanup.
func readDeletionRequestedAt(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID string) *time.Time {
	t.Helper()
	var requestedAt *time.Time
	require.NoError(t, pool.QueryRow(ctx, "SELECT deletion_requested_at FROM users WHERE id = $1", userID).Scan(&requestedAt))
	return requestedAt
}
