//go:build integration

package repository

// Integration tests for the RETENTION-NOTICE GATE on the hard-delete sweep (Issue #1569 T1, defect
// C1) and for the withholding counts that make a gated account observable.
//
// THE DEFECT. apps/web/src/legal/PrivacyPolicy.tsx promises a cancelled user notice at 30 days and
// again at 7 days before their data is destroyed. Nothing enforced that promise:
// GetUsersWithExpiredRetentionPage selected purely on the retention clock and the subscription
// status, with no reference to retention_notice_30d_sent_at / retention_notice_7d_sent_at at all
// (profile_repository.go's own comment above userSelectQuery said so in as many words), and the
// notice half is disabled in every environment — main.go passes no EmailService to
// RetentionCleanupCheckWorker, so canSendRetentionNotice returns false BEFORE the work-claim is
// taken and the markers stay NULL forever. Net: the account was hard-deleted at the end of the
// window having been warned zero times.
//
// ⚠ WHY pgxmock CANNOT COVER THIS. pgxmock matches query text and argument shape; it cannot show
// that a row with a NULL marker is refused by the predicate, which is the entire assertion. The
// unit-level tests in internal/queue/jobs also could not see the defect for a different reason:
// every one of them injects a mock email service, so the markers were always stamped in those
// fixtures and the nil-service production path was never exercised.
//
// THE SAFE DIRECTION TO FAIL IS RETAIN. The gate withholds; it never hastens. A withheld account is
// made visible by CountRetentionDeletionsWithheld (below) plus the Step 2b monitoring pass in
// internal/queue/jobs/retention_cleanup_check.go, because a refusal nobody can see is how an
// account accumulates forever in silence.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestRetentionNoticeGate -tags integration -p 1 -count=1 -v

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

// noticeGateEpoch anchors this file's seeded retention_expires_at values a decade in the past so
// the seeded rows sort ahead of anything another suite leaked, and so nothing here can be confused
// with the notice-window population (which is bounded by retention_expires_at > NOW()).
var noticeGateEpoch = time.Now().Add(-10 * 365 * 24 * time.Hour)

// seedNoticeGateUser creates one expired-retention deletion candidate with the two notice markers
// set exactly as asked. Every other condition of GetUsersWithExpiredRetentionPage's predicate is
// satisfied, so the markers are the ONLY variable across this file's cases.
func seedNoticeGateUser(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repo ProfileRepository,
	tag string,
	sent30d, sent7d *time.Time,
) *domain.User {
	t.Helper()
	suffix := fmt.Sprintf("gate_%s_%d", tag, time.Now().UnixNano())
	user := createRetentionFixtureUser(t, ctx, pool, repo, suffix, noticeGateEpoch, nil, nil)
	_, err := pool.Exec(ctx, `
		UPDATE users
		SET retention_notice_30d_sent_at = $1,
		    retention_notice_7d_sent_at = $2
		WHERE id = $3`, sent30d, sent7d, user.ID)
	require.NoError(t, err)
	user.RetentionNotice30dSentAt = sent30d
	user.RetentionNotice7dSentAt = sent7d
	return user
}

// collectExpiredRetentionCandidateIDs walks every page of the sweep and returns the ids it would
// hand to ExecuteDeletion. Walking to exhaustion (rather than reading one page) is what makes a
// "not returned" assertion honest: a row that merely sorts late is still deleted eventually.
func collectExpiredRetentionCandidateIDs(t *testing.T, ctx context.Context, repo *profileRepository) map[string]bool {
	t.Helper()
	seen := map[string]bool{}
	var cursor *RetentionSweepCursor
	for {
		page, err := repo.GetUsersWithExpiredRetentionPage(ctx, cursor, 200)
		require.NoError(t, err)
		if len(page) == 0 {
			return seen
		}
		for _, u := range page {
			seen[u.ID] = true
		}
		last := page[len(page)-1]
		cursor = &RetentionSweepCursor{ExpiresAt: *last.RetentionExpiresAt, UserID: last.ID}
		if len(page) < 200 {
			return seen
		}
	}
}

// TestRetentionNoticeGate_Integration_UnwarnedUserIsNotADeletionCandidate is the failing-first
// proof for Issue #1569 defect C1, and it asserts BOTH directions in one run so neither can be
// satisfied by a query that simply returns nothing:
//
//	(a) an expired user with both markers NULL is NOT returned;
//	(b) an expired user with only the 30d marker is NOT returned;
//	(c) an expired user with only the 7d marker is NOT returned;
//	(d) an expired user with BOTH markers recorded IS still returned.
//
// (d) is what makes this a gate rather than a switch-off, and it is the assertion that reddens if
// the predicate is written as a blanket refusal.
func TestRetentionNoticeGate_Integration_UnwarnedUserIsNotADeletionCandidate(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	sent30 := noticeGateEpoch.Add(-30 * 24 * time.Hour)
	sent7 := noticeGateEpoch.Add(-7 * 24 * time.Hour)

	neither := seedNoticeGateUser(t, ctx, pool, repo, "neither", nil, nil)
	only30 := seedNoticeGateUser(t, ctx, pool, repo, "only30", &sent30, nil)
	only7 := seedNoticeGateUser(t, ctx, pool, repo, "only7", nil, &sent7)
	both := seedNoticeGateUser(t, ctx, pool, repo, "both", &sent30, &sent7)

	candidates := collectExpiredRetentionCandidateIDs(t, ctx, repo)

	assert.False(t, candidates[neither.ID],
		"a cancelled user warned ZERO times must never be a hard-delete candidate — PrivacyPolicy.tsx promises notice at 30 and 7 days")
	assert.False(t, candidates[only30.ID],
		"the 7-day final warning is a separate promise; a user who got only the 30-day notice has not received it")
	assert.False(t, candidates[only7.ID],
		"the 30-day notice is a separate promise; the gate must require BOTH markers, not either")
	assert.True(t, candidates[both.ID],
		"a user who received both promised notices must still be deleted — otherwise this is not a gate, it is a permanent halt")
}

// TestRetentionNoticeGate_Integration_ClaimRefusesUnwarnedUser proves the gate reaches the CLAIM as
// well as the candidate query. The two are spliced from one constant precisely because drift
// between them was #1440 T0-retention: the candidate query required conditions the claim did not,
// so a row whose state changed in between was still authorised for the irreversible act.
//
// The markers are safe to re-check at claim time because nothing can clear them while the row
// remains a candidate: the only two writers that NULL them (ClearRetentionDates on resubscribe, and
// the admin re-arm in admin_user_repository.go) clear retention_expires_at in the same statement,
// which expiredRetentionClockPredicate already refuses.
func TestRetentionNoticeGate_Integration_ClaimRefusesUnwarnedUser(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	sent30 := noticeGateEpoch.Add(-30 * 24 * time.Hour)
	sent7 := noticeGateEpoch.Add(-7 * 24 * time.Hour)

	unwarned := seedNoticeGateUser(t, ctx, pool, repo, "claim_unwarned", nil, nil)
	warned := seedNoticeGateUser(t, ctx, pool, repo, "claim_warned", &sent30, &sent7)

	claimedUnwarned, err := repo.ClaimForDeletion(ctx, unwarned.ID, DeletionKindRetention)
	require.NoError(t, err)
	assert.False(t, claimedUnwarned,
		"the claim that authorises an irreversible hard delete must refuse a user who was never warned")

	claimedWarned, err := repo.ClaimForDeletion(ctx, warned.ID, DeletionKindRetention)
	require.NoError(t, err)
	assert.True(t, claimedWarned,
		"a fully-warned expired user must still be claimable — this is the direction that proves the claim gate is not a blanket refusal")
}

// TestRetentionNoticeGate_Integration_WithholdingCountsSeparatePendingFromImpossible is the
// observability half, and the reason it is not optional: gating on a notice that can never be sent
// retains an account forever with nothing anywhere reporting it.
//
// ⚠ THE DISTINCTION THIS TEST PINS. `retention_notice_*_sent_at IS NULL` means two completely
// different things depending on the clock, and an alert that conflates them fires on every healthy
// account and is worthless:
//
//   - retention_expires_at > NOW() — the clock is still running. GetUsersNeedingRetention30dNotice
//     and its 7d sibling both bound their population by exactly this, so these users are still
//     selectable by the notice sweep. NOT withheld, no alert. This is the healthy majority.
//   - retention_expires_at <= NOW() — the clock has run out. Those same two queries can never
//     select this user again (their bound is the opposite one), so the missing notice will never be
//     sent by any existing path and the gate holds the account indefinitely. THIS is the alert.
//
// ⚠ WHAT THIS TEST DELIBERATELY NO LONGER ASSERTS. Its first version seeded a user with
// `email = ''` written straight to the column and asserted a separate no-address count. That row
// shape CANNOT OCCUR IN PRODUCTION: users.email holds ciphertext, and
// FieldEncryptor.EncryptString returns "ENC$"+ct even for the empty string, so the only way to
// produce a bare '' is to bypass the encryption path — which is what the test did. It passed while
// validating a state the system cannot reach. The no-address reason was removed rather than
// re-tested through the real path, because email_blind_idx (the one column that could discriminate)
// carries a UNIQUE index, capping that population at one row database-wide. See
// CountRetentionDeletionsWithheld's doc comment.
func TestRetentionNoticeGate_Integration_WithholdingCountsSeparatePendingFromImpossible(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	sent30 := noticeGateEpoch.Add(-30 * 24 * time.Hour)
	sent7 := noticeGateEpoch.Add(-7 * 24 * time.Hour)

	before, err := repo.CountRetentionDeletionsWithheld(ctx)
	require.NoError(t, err)

	// Withheld: expired clock, neither notice recorded.
	withheldA := seedNoticeGateUser(t, ctx, pool, repo, "withheld_a", nil, nil)
	// Withheld on the OTHER marker only, so the count cannot be satisfied by testing just one.
	withheldB := seedNoticeGateUser(t, ctx, pool, repo, "withheld_b", &sent30, nil)
	// NOT withheld: fully warned, so the gate lets it through and it is nobody's alert.
	_ = seedNoticeGateUser(t, ctx, pool, repo, "warned", &sent30, &sent7)
	// NOT withheld: the clock is still running, so the notice sweep can still reach this user.
	pending := createRetentionFixtureUser(t, ctx, pool, repo,
		fmt.Sprintf("gate_pending_%d", time.Now().UnixNano()), time.Now().Add(20*24*time.Hour), nil, nil)

	after, err := repo.CountRetentionDeletionsWithheld(ctx)
	require.NoError(t, err)

	assert.Equal(t, 2, after.WithheldNoticeMissing-before.WithheldNoticeMissing,
		"both expired users missing a notice must be counted as withheld: %s (neither) and %s (7d only)", withheldA.ID, withheldB.ID)
	assert.Equal(t, 1, after.NoticePending-before.NoticePending,
		"a user whose clock is still running (%s) is healthy — counting it as withheld would fire the alert on every ordinary cancellation", pending.ID)

	// ⚠ THE ASSERTION THAT REFUTES THE CONFLATION: the two populations are disjoint and neither is
	// a superset of the other, so a single "markers are NULL" count cannot satisfy both deltas.
	assert.NotEqual(t,
		after.WithheldNoticeMissing-before.WithheldNoticeMissing,
		after.NoticePending-before.NoticePending,
		"withheld and pending must be separately derived, not one number reported twice")
}
