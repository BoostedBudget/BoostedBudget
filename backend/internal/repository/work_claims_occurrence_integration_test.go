//go:build integration

package repository

// work_claims_occurrence_integration_test.go — real-Postgres proof that OccurrenceResourceID
// actually produces the composite-key behavior work_claims needs for recurring FLOW actions
// (Issue #1413 P5): a retry of the SAME occurrence collides on the UNIQUE constraint
// (ClaimAlreadyCompleted after Complete), while a genuinely NEW occurrence gets an unclaimed row.
//
// pgxmock (work_claims_repository_test.go) can only match query text and cannot validate the
// ON CONFLICT ... WHERE semantics this depends on — see work_claims_repository_integration_test.go
// for the same rationale applied to the base claim mechanism. This file is the occurrence-key
// layer's counterpart: it proves the KEY CONSTRUCTION behaves correctly against the real unique
// index, not merely that OccurrenceResourceID returns a plausible-looking string (that is
// work_claims_occurrence_test.go's job, as a pure unit test).
//
// Run:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestWorkClaimsOccurrence -tags integration -v

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recurringFlowActions enumerates the five actions Issue #1413 P5's fix applies to — each
// legitimately recurs across a user's lifetime and MUST be keyed through OccurrenceResourceID.
// ActionAccountDeletion is deliberately excluded: it is genuinely terminal and stays keyed on the
// bare userID (see TestWorkClaimsOccurrence_AccountDeletion_StaysKeyedOnBareUserID_StillTerminal
// below).
var recurringFlowActions = []string{
	ActionPlaidDisconnectPastDue,
	ActionPastDueGraceCancel,
	ActionSubscriptionCancelledCleanup,
	ActionRetentionNotice30d,
	ActionRetentionNotice7d,
}

// TestWorkClaimsOccurrence_SameOccurrence_SecondClaimAlreadyCompleted proves, for every recurring
// FLOW action, that a retry of the SAME occurrence (identical userID + discriminator) collides on
// the composite key: after Complete, ClaimOutcome for that exact resourceID reports
// ClaimAlreadyCompleted, not a fresh win.
func TestWorkClaimsOccurrence_SameOccurrence_SecondClaimAlreadyCompleted(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	repo := NewWorkClaimsOutcomeRepository(pool)

	for _, action := range recurringFlowActions {
		t.Run(action, func(t *testing.T) {
			userID := uuid.New().String()
			occurrence := time.Now()
			resourceID, err := OccurrenceResourceID(userID, FormatOccurrenceTimestamp(&occurrence))
			require.NoError(t, err)
			t.Cleanup(func() {
				_, _ = pool.Exec(context.Background(),
					`DELETE FROM work_claims WHERE resource_type = $1 AND resource_id = $2 AND action = $3`,
					ResourceTypeUser, resourceID, action)
			})

			token, claimed, err := repo.Claim(ctx, ResourceTypeUser, resourceID, action, 15*time.Minute)
			require.NoError(t, err)
			require.True(t, claimed, "first claim on a fresh occurrence must win")

			ok, err := repo.Complete(ctx, ResourceTypeUser, resourceID, action, token)
			require.NoError(t, err)
			require.True(t, ok)

			// Same occurrence, retried (e.g. the caller's job retries, or River retries the
			// job) — must be told it's already done, so the caller never re-invokes the external
			// collaborator.
			_, outcome, err := repo.ClaimOutcome(ctx, ResourceTypeUser, resourceID, action, 15*time.Minute)
			require.NoError(t, err)
			assert.Equal(t, ClaimAlreadyCompleted, outcome,
				"a retry of the SAME occurrence must collide and report AlreadyCompleted — this is "+
					"the entire point of putting the occurrence in the key")
		})
	}
}

// TestWorkClaimsOccurrence_NewOccurrence_AfterCompletedEarlierOne_ClaimWon is THE regression test
// this whole task exists to fix, proven at the repository layer for every recurring FLOW action:
// a genuinely NEW occurrence (different discriminator) for the SAME user+action must still be
// claimable, even though an EARLIER occurrence already completed. A bare-userID key would report
// this as ClaimAlreadyCompleted forever — the exact permanent-block bug.
func TestWorkClaimsOccurrence_NewOccurrence_AfterCompletedEarlierOne_ClaimWon(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	repo := NewWorkClaimsRepository(pool)

	for _, action := range recurringFlowActions {
		t.Run(action, func(t *testing.T) {
			userID := uuid.New().String()
			firstOccurrence := time.Now().Add(-48 * time.Hour)
			secondOccurrence := time.Now()

			firstResourceID, err := OccurrenceResourceID(userID, FormatOccurrenceTimestamp(&firstOccurrence))
			require.NoError(t, err)
			secondResourceID, err := OccurrenceResourceID(userID, FormatOccurrenceTimestamp(&secondOccurrence))
			require.NoError(t, err)
			require.NotEqual(t, firstResourceID, secondResourceID,
				"test setup sanity: the two occurrences must actually be different discriminators")

			t.Cleanup(func() {
				for _, rid := range []string{firstResourceID, secondResourceID} {
					_, _ = pool.Exec(context.Background(),
						`DELETE FROM work_claims WHERE resource_type = $1 AND resource_id = $2 AND action = $3`,
						ResourceTypeUser, rid, action)
				}
			})

			// First occurrence completes fully.
			firstToken, claimed, err := repo.Claim(ctx, ResourceTypeUser, firstResourceID, action, 15*time.Minute)
			require.NoError(t, err)
			require.True(t, claimed)
			ok, err := repo.Complete(ctx, ResourceTypeUser, firstResourceID, action, firstToken)
			require.NoError(t, err)
			require.True(t, ok)

			// THE REGRESSION THIS WHOLE TASK IS ABOUT: a genuinely NEW occurrence for the SAME
			// user and action must still be claimable.
			secondToken, claimed, err := repo.Claim(ctx, ResourceTypeUser, secondResourceID, action, 15*time.Minute)
			require.NoError(t, err)
			assert.True(t, claimed,
				"a NEW occurrence (different discriminator) must be claimable even though an "+
					"EARLIER occurrence for the same user+action already completed")
			assert.NotEmpty(t, secondToken)
		})
	}
}

// TestWorkClaimsOccurrence_AccountDeletion_StaysKeyedOnBareUserID_StillTerminal pins the ONE
// deliberate exception: ActionAccountDeletion is NOT built through OccurrenceResourceID (see
// deletion_service.go:330/452, which pass the bare userID directly) because it is genuinely
// terminal — a "new occurrence" concept does not apply to a once-ever GDPR deletion. Once
// completed, it must remain permanently un-reclaimable, unlike the five recurring actions above.
func TestWorkClaimsOccurrence_AccountDeletion_StaysKeyedOnBareUserID_StillTerminal(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	repo := NewWorkClaimsRepository(pool)

	userID := uuid.New().String()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM work_claims WHERE resource_type = $1 AND resource_id = $2 AND action = $3`,
			ResourceTypeUser, userID, ActionAccountDeletion)
	})

	token, claimed, err := repo.Claim(ctx, ResourceTypeUser, userID, ActionAccountDeletion, 15*time.Minute)
	require.NoError(t, err)
	require.True(t, claimed)
	ok, err := repo.Complete(ctx, ResourceTypeUser, userID, ActionAccountDeletion, token)
	require.NoError(t, err)
	require.True(t, ok)

	_, reclaimed, err := repo.Claim(ctx, ResourceTypeUser, userID, ActionAccountDeletion, 15*time.Minute)
	require.NoError(t, err)
	assert.False(t, reclaimed,
		"account_deletion must remain permanently terminal after completion — it is a genuinely "+
			"once-ever action, deliberately unchanged by Issue #1413 P5")
}
