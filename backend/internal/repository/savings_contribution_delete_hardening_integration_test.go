//go:build integration

package repository_test

// savings_contribution_delete_hardening_integration_test.go — Issue #1569 round 17 task 6.
//
// DeleteContribution is the ONE deliberate, user-facing hard delete of financial history on this
// branch. The ADR (docs/decisions/2026-08-24-financial-history-retained-by-default.md) requires
// four controls of anything in the explicit-destruction bucket: its own endpoint distinct from the
// retaining path, an audit row written in the SAME transaction, a precondition that refuses to run
// on data the user has not named, and an entry in allowedHistoryDeletes. It met two.
//
// These tests pin the two it did not, plus the leg nobody had written down: the DELETE carried no
// `c.deleted_at IS NULL`, so it could hard-destroy a contribution the system had ALREADY
// deliberately retained (DeleteManagedForTransaction tombstones reconciler rows when Plaid reports
// the underlying transaction as `removed` — "the provider is not the user"). Such a row is
// invisible in every product surface, so the user could not have named it in a confirm dialog.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDeleteContribution_RefusesAnAlreadyTombstonedContribution_Integration is finding (5): the
// undocumented leg, and the only one that destroys data the system had already decided to keep.
func TestDeleteContribution_RefusesAnAlreadyTombstonedContribution_Integration(t *testing.T) {
	pool := integrationDB(t)
	ctx := context.Background()

	userID := seedIntegrationUser(t, pool)
	accountID := seedIntegrationAccount(t, pool, userID)
	goalID := seedIntegrationGoal(t, pool, userID, accountID)
	txnID := seedIntegrationTransaction(t, pool, userID, accountID, -100.0)

	repo := repository.NewSavingsContributionRepository(pool)

	// The production boost-match path: a CONFIRMED, managed_by_reconciliation=true row — a record
	// of the user's money moving, not a regenerable proposal (see the managed-delete test's
	// four-writer enumeration).
	require.NoError(t, repo.UpsertFromTransaction(ctx, userID, goalID, txnID, 100.0, "boost_match"))

	var contribID string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT id FROM savings_goal_contributions WHERE savings_goal_id = $1 AND transaction_id = $2
	`, goalID, txnID).Scan(&contribID))

	// Plaid re-states the transaction. Round 12 made this a TOMBSTONE: the row is retained and
	// permanently invisible.
	require.NoError(t, repo.DeleteManagedForTransaction(ctx, txnID))

	var deletedAt *time.Time
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT deleted_at FROM savings_goal_contributions WHERE id = $1`, contribID).Scan(&deletedAt))
	require.NotNil(t, deletedAt, "fixture precondition: the row must be tombstoned before the delete")

	err := repo.DeleteContribution(ctx, userID, goalID, contribID, nil)
	assert.ErrorIs(t, err, domain.ErrContributionNotFound,
		"a tombstoned contribution is invisible in every product surface, so the user cannot have "+
			"named it: the explicit-destruction path must refuse it, not destroy it")

	var stillThere int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM savings_goal_contributions WHERE id = $1`, contribID).Scan(&stillThere))
	assert.Equal(t, 1, stillThere,
		"the row the system deliberately RETAINED must survive the explicit-destruction endpoint")
}

// TestDeleteContribution_RefusesARejectedContribution_Integration is the live-data precondition.
// A 'rejected' row is a STICKY TOMBSTONE (RejectContribution, R3-7): it is retained on purpose so a
// later sync cannot re-propose the same transfer-in, and the frontend filters it out of the ledger
// (GoalContributionsSheet.tsx, `c.status !== 'rejected'`). The endpoint's own description says it
// destroys a contribution "(proposed or confirmed)" — rejected was never in its contract.
func TestDeleteContribution_RefusesARejectedContribution_Integration(t *testing.T) {
	pool := integrationDB(t)
	ctx := context.Background()

	userID := seedIntegrationUser(t, pool)
	accountID := seedIntegrationAccount(t, pool, userID)
	goalID := seedIntegrationGoal(t, pool, userID, accountID)
	txnID := seedIntegrationTransaction(t, pool, userID, accountID, -80.0)

	repo := repository.NewSavingsContributionRepository(pool)

	proposed := &domain.SavingsGoalContribution{
		SavingsGoalID: goalID,
		TransactionID: &txnID,
		Amount:        80.0,
		Source:        "confirmed_transfer",
		Status:        "proposed",
		OccurredOn:    time.Now().Format("2006-01-02"),
	}
	require.NoError(t, repo.Insert(ctx, userID, proposed))

	rejected, err := repo.RejectContribution(ctx, userID, goalID, proposed.ID)
	require.NoError(t, err)
	require.Equal(t, "rejected", rejected.Status)

	err = repo.DeleteContribution(ctx, userID, goalID, proposed.ID, nil)
	assert.ErrorIs(t, err, domain.ErrContributionNotFound,
		"destroying the sticky tombstone would both erase a row the product retains on purpose and "+
			"regress R3-7: the next sync would re-propose the same transfer-in")

	var stillThere int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM savings_goal_contributions WHERE id = $1`, proposed.ID).Scan(&stillThere))
	assert.Equal(t, 1, stillThere, "the sticky tombstone must survive")
}

// TestDeleteContribution_WritesAnAuditRowInTheSameTransaction_Integration is control 2. The HTTP
// layer's claim that the deletion is "still logged" is a slog line outside any transaction; the ADR
// requires the row to commit or roll back WITH the destruction.
func TestDeleteContribution_WritesAnAuditRowInTheSameTransaction_Integration(t *testing.T) {
	pool := integrationDB(t)
	ctx := context.Background()

	userID := seedIntegrationUser(t, pool)
	accountID := seedIntegrationAccount(t, pool, userID)
	goalID := seedIntegrationGoal(t, pool, userID, accountID)

	repo := repository.NewSavingsContributionRepository(pool)

	contrib := &domain.SavingsGoalContribution{
		SavingsGoalID: goalID,
		Amount:        25.0,
		Source:        "manual",
		Status:        "confirmed",
		OccurredOn:    time.Now().Format("2006-01-02"),
	}
	require.NoError(t, repo.Insert(ctx, userID, contrib))
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM audit_logs WHERE resource_id = $1`, contrib.ID)
	})

	require.NoError(t, repo.DeleteContribution(ctx, userID, goalID, contrib.ID, repository.NewAuditLogRepository(pool)))

	var audits int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT count(*) FROM audit_logs
		WHERE resource_type = 'savings_goal_contribution'
		  AND resource_id = $1
		  AND action = 'savings_goal_contribution.history_purged'
	`, contrib.ID).Scan(&audits))
	assert.Equal(t, 1, audits,
		"explicit destruction of financial history must leave an audit row written in the same "+
			"transaction as the delete (ADR § Classification Test, control 2)")
}

// failingAuditLog is an AuditLogRepository whose in-transaction write always fails. It is how the
// SAME-TRANSACTION half of control 2 is proved: if the audit row were written outside the delete's
// transaction (or after it committed), the contribution would be gone despite the audit failure.
type failingAuditLog struct{ err error }

func (f failingAuditLog) Log(context.Context, string, string, string) error { return f.err }
func (f failingAuditLog) LogInTx(context.Context, pgx.Tx, string, string, string) error {
	return f.err
}

// TestDeleteContribution_AuditFailureRollsBackTheDelete_Integration is the other half of control 2:
// the ADR asks for the audit row IN THE SAME TRANSACTION, and "a row exists afterwards" alone would
// also be satisfied by a second, independent write. Nothing may be destroyed without the trail.
func TestDeleteContribution_AuditFailureRollsBackTheDelete_Integration(t *testing.T) {
	pool := integrationDB(t)
	ctx := context.Background()

	userID := seedIntegrationUser(t, pool)
	accountID := seedIntegrationAccount(t, pool, userID)
	goalID := seedIntegrationGoal(t, pool, userID, accountID)

	repo := repository.NewSavingsContributionRepository(pool)
	contrib := &domain.SavingsGoalContribution{
		SavingsGoalID: goalID,
		Amount:        40.0,
		Source:        "manual",
		Status:        "confirmed",
		OccurredOn:    time.Now().Format("2006-01-02"),
	}
	require.NoError(t, repo.Insert(ctx, userID, contrib))

	err := repo.DeleteContribution(ctx, userID, goalID, contrib.ID,
		failingAuditLog{err: errors.New("audit sink down")})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "audit sink down")

	var stillThere int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM savings_goal_contributions WHERE id = $1`, contrib.ID).Scan(&stillThere))
	assert.Equal(t, 1, stillThere,
		"the delete and its audit row share one transaction: if the trail cannot be written, "+
			"nothing is destroyed")
}

// TestDeleteContribution_StillDestroysTheRowTheUserNamed_Integration is the control case. The three
// refusals above must not have turned the endpoint into a no-op: a live, visible, confirmed
// contribution on a live goal is still destroyed, irrecoverably. That is the feature.
func TestDeleteContribution_StillDestroysTheRowTheUserNamed_Integration(t *testing.T) {
	pool := integrationDB(t)
	ctx := context.Background()

	userID := seedIntegrationUser(t, pool)
	accountID := seedIntegrationAccount(t, pool, userID)
	goalID := seedIntegrationGoal(t, pool, userID, accountID)

	repo := repository.NewSavingsContributionRepository(pool)
	for _, status := range []string{"confirmed", "proposed"} {
		contrib := &domain.SavingsGoalContribution{
			SavingsGoalID: goalID,
			Amount:        12.0,
			Source:        "manual",
			Status:        status,
			OccurredOn:    time.Now().Format("2006-01-02"),
		}
		require.NoError(t, repo.Insert(ctx, userID, contrib), status)
		require.NoError(t, repo.DeleteContribution(ctx, userID, goalID, contrib.ID, nil), status)

		var stillThere int
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT count(*) FROM savings_goal_contributions WHERE id = $1`, contrib.ID).Scan(&stillThere))
		assert.Equal(t, 0, stillThere,
			"a %s row the user named on a live goal is still destroyed — the endpoint's whole purpose", status)
	}
}

// TestDeleteContribution_StillDeletesAnArchivedRow_Integration is the reviewer's five-line ask
// from #1625's final review.
//
// DeleteContribution's predicate is BLESSED, fragment by fragment, by
// TestEveryAllowlistedHistoryDeleteStillCarriesItsPredicate (G2's second half) — but that guard
// checks the fragments the entry NAMES are still present. It says nothing about fragments ADDED.
// So someone tidying `AND c.archived_at IS NULL` in "for consistency" with the SumConfirmed reads
// would keep every guard green while quietly making a closed period's contributions
// undeletable — a row the user explicitly asked to destroy, silently retained.
//
// Archived means "belongs to a CLOSED contributions period", not "protected". The user's explicit
// destruction verb outranks the period boundary; that is what makes DeleteContribution explicit
// destruction rather than side-effect destruction (see the ADR's classification test).
func TestDeleteContribution_StillDeletesAnArchivedRow_Integration(t *testing.T) {
	pool := integrationDB(t)
	ctx := context.Background()

	userID := seedIntegrationUser(t, pool)
	accountID := seedIntegrationAccount(t, pool, userID)
	goalID := seedIntegrationGoal(t, pool, userID, accountID)

	repo := repository.NewSavingsContributionRepository(pool)
	contrib := &domain.SavingsGoalContribution{
		SavingsGoalID: goalID,
		Amount:        50.0,
		Source:        "manual",
		Status:        "confirmed",
		OccurredOn:    time.Now().Format("2006-01-02"),
	}
	require.NoError(t, repo.Insert(ctx, userID, contrib))

	_, err := pool.Exec(ctx,
		`UPDATE savings_goal_contributions SET archived_at = now() WHERE id = $1`, contrib.ID)
	require.NoError(t, err, "archive the row (what a progress_basis round-trip does)")

	// Falsifiability: the row really is archived, so the assertion below is about archived rows.
	var archived *time.Time
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT archived_at FROM savings_goal_contributions WHERE id = $1`, contrib.ID).Scan(&archived))
	require.NotNil(t, archived, "fixture failed to archive the row")

	require.NoError(t, repo.DeleteContribution(ctx, userID, goalID, contrib.ID, nil),
		"an archived contribution the user explicitly asked to delete must still be deletable")

	var stillThere int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM savings_goal_contributions WHERE id = $1`, contrib.ID).Scan(&stillThere))
	assert.Equal(t, 0, stillThere, "the row the user named must be gone")
}

// spyAuditLog records WHICH audit method was used and, from inside LogInTx, whether the caller's
// transaction can already see the contribution as gone.
type spyAuditLog struct {
	calledLog     bool
	calledLogInTx bool
	rowGoneInTx   bool
	scanErr       error
	contribID     string
}

func (s *spyAuditLog) Log(context.Context, string, string, string) error {
	s.calledLog = true
	return nil
}

func (s *spyAuditLog) LogInTx(ctx context.Context, tx pgx.Tx, _, _, _ string) error {
	s.calledLogInTx = true
	var n int
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM savings_goal_contributions WHERE id = $1`, s.contribID).Scan(&n); err != nil {
		s.scanErr = err
		return nil
	}
	s.rowGoneInTx = n == 0
	return nil
}

// TestDeleteContribution_AuditRowSharesTheDeletesTransaction_Integration is the mutant-killing half
// of control 2. TestDeleteContribution_AuditFailureRollsBackTheDelete_Integration on its own does
// NOT distinguish LogInTx from Log: a failing sink aborts the method either way, so the delete
// rolls back either way — swapping LogInTx for Log leaves that test green (measured, #1569 round 17
// task 6). This one refuses the swap directly: the audit MUST go through the delete's own
// transaction handle, and from inside it the destroyed row must already be invisible.
func TestDeleteContribution_AuditRowSharesTheDeletesTransaction_Integration(t *testing.T) {
	pool := integrationDB(t)
	ctx := context.Background()

	userID := seedIntegrationUser(t, pool)
	accountID := seedIntegrationAccount(t, pool, userID)
	goalID := seedIntegrationGoal(t, pool, userID, accountID)

	repo := repository.NewSavingsContributionRepository(pool)
	contrib := &domain.SavingsGoalContribution{
		SavingsGoalID: goalID,
		Amount:        60.0,
		Source:        "manual",
		Status:        "confirmed",
		OccurredOn:    time.Now().Format("2006-01-02"),
	}
	require.NoError(t, repo.Insert(ctx, userID, contrib))

	spy := &spyAuditLog{contribID: contrib.ID}
	require.NoError(t, repo.DeleteContribution(ctx, userID, goalID, contrib.ID, spy))

	require.NoError(t, spy.scanErr)
	assert.True(t, spy.calledLogInTx, "the audit entry must be written with LogInTx")
	assert.False(t, spy.calledLog, "Log() writes on a SEPARATE connection — that is not control 2")
	assert.True(t, spy.rowGoneInTx,
		"the transaction handed to LogInTx must be the one that performed the delete: from inside "+
			"it the row is already gone, which no other connection could yet observe")
}
