// household_purge_retention_integration_test.go — #1569 Task 15 (T7): the 30-day
// PurgeRemovedMembership job and the household's own record of what it was shown.
//
// Run:
//
//	DATABASE_URL="postgres://..." go test -tags integration -p 1 ./internal/services/ -run PurgeRemovedMembership_Retains -v
package services

import (
	"context"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/require"
)

// purgeRetentionFixture builds an owner + member household where the member has one
// user_account shared into it, then removes the member and backdates both the membership
// removal and the share revocation past the 30-day purge window. It returns the member's
// membership row id (what the purge job is keyed on) and their user_account id.
type purgeRetentionFixture struct {
	*integrationServiceFixture
	householdID  string
	ownerID      string
	memberID     string
	membershipID string
	acctID       string
	accountID    string
	sharedAt     time.Time
	revokedAt    time.Time
}

func setupPurgeRetentionFixture(t *testing.T) *purgeRetentionFixture {
	t.Helper()
	f := setupHouseholdServiceIntegration(t)
	ctx := context.Background()

	owner := insertIntegrationTestUser(t, f.profileRepo, "owner-"+t.Name()+"@example.com", "household_2", "active")
	member := insertIntegrationTestUser(t, f.profileRepo, "member-"+t.Name()+"@example.com", "premium", "trial")
	t.Cleanup(func() {
		f.pool.Exec(context.Background(), `DELETE FROM users WHERE id = ANY($1)`, []string{owner.ID, member.ID}) //nolint:errcheck
	})

	household, err := f.svc.CreateHousehold(ctx, owner.ID, "Purge Retention Household")
	require.NoError(t, err)
	t.Cleanup(func() {
		f.pool.Exec(context.Background(), `DELETE FROM households WHERE id = $1`, household.ID) //nolint:errcheck
	})

	var membershipID string
	require.NoError(t, f.pool.QueryRow(ctx, `
		INSERT INTO household_members (household_id, user_id, role, status) VALUES ($1, $2, 'member', 'active')
		RETURNING id::text
	`, household.ID, member.ID).Scan(&membershipID))

	var acctID string
	require.NoError(t, f.pool.QueryRow(ctx, `
		INSERT INTO user_accounts (user_id, institution_id, institution_id_blind_idx)
		VALUES ($1, 'enc_inst', 'blind_inst_'||gen_random_uuid()::text)
		RETURNING id::text
	`, member.ID).Scan(&acctID))
	t.Cleanup(func() {
		f.pool.Exec(context.Background(), `DELETE FROM user_accounts WHERE id = $1`, acctID) //nolint:errcheck
	})

	// A real accounts row hanging off that user_account, so the production household-scope
	// read below has something to return — without it every assertion about that read would
	// be satisfied by an empty result and prove nothing.
	var linkedItemID, accountID string
	require.NoError(t, f.pool.QueryRow(ctx, `
		INSERT INTO linked_items (user_id, item_id, access_token_encrypted)
		VALUES ($1, 'purge-retention-item-' || gen_random_uuid()::text, 'enc-test-token')
		RETURNING id::text
	`, member.ID).Scan(&linkedItemID))
	require.NoError(t, f.pool.QueryRow(ctx, `
		INSERT INTO accounts (linked_item_id, user_id, user_account_id, plaid_account_id, name, type, status)
		VALUES ($1, $2, $3, 'plaid-purge-retention-' || gen_random_uuid()::text, 'Member Checking', 'depository', 'active')
		RETURNING id::text
	`, linkedItemID, member.ID, acctID).Scan(&accountID))

	shareRepo := repository.NewHouseholdShareRepository(f.pool)
	require.NoError(t, shareRepo.InsertShare(ctx, household.ID, acctID, nil, domain.ShareVisibilityShared))

	// The member leaves: soft-remove + revoke, exactly as production does.
	require.NoError(t, f.svc.RemoveMember(ctx, owner.ID, member.ID))

	// Backdate the whole episode so the purge job's 30-day window has elapsed, with
	// deterministic interval boundaries an as-of read can be asserted against.
	now := time.Now().UTC()
	sharedAt := now.AddDate(0, 0, -90)
	revokedAt := now.AddDate(0, 0, -40)
	_, err = f.pool.Exec(ctx, `
		UPDATE household_account_shares SET shared_at = $1, revoked_at = $2
		WHERE household_id = $3 AND user_account_id = $4
	`, sharedAt, revokedAt, household.ID, acctID)
	require.NoError(t, err)
	_, err = f.pool.Exec(ctx, `UPDATE household_members SET removed_at = $1 WHERE id = $2`, revokedAt, membershipID)
	require.NoError(t, err)

	return &purgeRetentionFixture{
		integrationServiceFixture: f,
		householdID:               household.ID,
		ownerID:                   owner.ID,
		memberID:                  member.ID,
		membershipID:              membershipID,
		acctID:                    acctID,
		accountID:                 accountID,
		sharedAt:                  sharedAt,
		revokedAt:                 revokedAt,
	}
}

func (f *purgeRetentionFixture) countShareRows(t *testing.T) int {
	t.Helper()
	var n int
	require.NoError(t, f.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM household_account_shares WHERE household_id = $1 AND user_account_id = $2`,
		f.householdID, f.acctID).Scan(&n))
	return n
}

// TestHouseholdServiceIntegration_PurgeRemovedMembership_RetainsRevokedShareGrants is the
// ruling of #1569 T7 in executable form: the 30-day purge must NOT destroy the household's
// record of what it was shown and when. See PurgeRemovedMembership's own comment for the
// privacy-vs-retention reasoning.
func TestHouseholdServiceIntegration_PurgeRemovedMembership_RetainsRevokedShareGrants(t *testing.T) {
	f := setupPurgeRetentionFixture(t)
	ctx := context.Background()

	require.Equal(t, 1, f.countShareRows(t), "fixture precondition: one grant row exists before the purge")

	require.NoError(t, f.svc.PurgeRemovedMembership(ctx, f.householdID, f.membershipID))

	require.Equal(t, 1, f.countShareRows(t),
		"the purge must retain the revoked grant — it is the household's own audit of what it was "+
			"legitimately shown between shared_at and revoked_at, not the departing member's data")

	var revokedAt *time.Time
	require.NoError(t, f.pool.QueryRow(ctx,
		`SELECT revoked_at FROM household_account_shares WHERE household_id = $1 AND user_account_id = $2`,
		f.householdID, f.acctID).Scan(&revokedAt))
	require.NotNil(t, revokedAt, "the retained grant must stay CLOSED — retention must not resurrect visibility")
}

// TestHouseholdServiceIntegration_PurgeRemovedMembership_AsOfReadSurvivesThePurge is why the
// retention matters: with the grant destroyed, a point-in-time household read for a date
// inside the shared window has no basis to answer from, and silently reports that nothing
// was ever shared.
func TestHouseholdServiceIntegration_PurgeRemovedMembership_AsOfReadSurvivesThePurge(t *testing.T) {
	f := setupPurgeRetentionFixture(t)
	ctx := context.Background()

	require.NoError(t, f.svc.PurgeRemovedMembership(ctx, f.householdID, f.membershipID))

	acctRepo := repository.NewAccountRepository(f.pool, newTestFieldEncryptor(t))
	scope := &domain.HouseholdScope{HouseholdID: f.householdID, MemberUserIDs: []string{f.ownerID}}

	inside := f.revokedAt.AddDate(0, 0, -10)

	accounts, err := acctRepo.GetForHouseholdScopeActiveNow(ctx, f.ownerID, scope, inside)
	require.NoError(t, err)
	require.Len(t, accounts, 1, "an as-of read INSIDE the shared window must still resolve the departed member's account 30+ days after the purge ran")
	require.Equal(t, f.accountID, accounts[0].ID)

	after, err := acctRepo.GetForHouseholdScopeActiveNow(ctx, f.ownerID, scope, time.Now().UTC())
	require.NoError(t, err)
	require.Empty(t, after, "and nothing after the departure — retention must not widen what the household can see today")
}

// TestHouseholdServiceIntegration_PurgeRemovedMembership_GDPRErasureStillDestroysGrants is the
// counterweight: retention is the household's audit, but the DEPARTING member's GDPR erasure
// must still destroy it. The grant hangs off user_accounts, so the cascade reaches it.
func TestHouseholdServiceIntegration_PurgeRemovedMembership_GDPRErasureStillDestroysGrants(t *testing.T) {
	f := setupPurgeRetentionFixture(t)
	ctx := context.Background()

	require.NoError(t, f.svc.PurgeRemovedMembership(ctx, f.householdID, f.membershipID))
	require.Equal(t, 1, f.countShareRows(t), "precondition: the purge retained the grant")

	// Retention-triggered erasure (deletion_requested_at stays NULL, so expected is nil) —
	// the same arming every other DeleteUserCascade integration test in this package uses.
	// The retention branch re-verifies subscription_status IN ('inactive','cancelled')
	// (profile_repository.go:2430), so the fixture must be a genuine retention candidate.
	_, err := f.pool.Exec(ctx, `UPDATE users SET subscription_status = 'cancelled' WHERE id = $1`, f.memberID)
	require.NoError(t, err)
	// DeleteUserCascade refuses while any linked_items row still needs Plaid-side cleanup
	// (uncleanedLinkedItemExistsClause — `plaid_removed_at IS NULL`). The fixture's item is
	// only there to hang an accounts row off; mark it cleaned.
	_, err = f.pool.Exec(ctx, `UPDATE linked_items SET plaid_removed_at = now() WHERE user_id = $1`, f.memberID)
	require.NoError(t, err)
	armElapsedRetentionClock(t, ctx, f.pool, f.profileRepo, f.memberID)

	deleted, _, err := f.profileRepo.DeleteUserCascade(ctx, f.memberID, nil)
	require.NoError(t, err)
	require.True(t, deleted, "DeleteUserCascade must have claimed and erased the member")

	require.Equal(t, 0, f.countShareRows(t),
		"GDPR erasure must still destroy everything — retention is scoped to a household's audit of "+
			"its own past, never to surviving an erasure request")
}
