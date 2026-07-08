package services

// household_service_integration_test.go — end-to-end proof of HouseholdService.AcceptInvite
// against a real Postgres instance: real encryption round-trip, real constraints, real
// FOR UPDATE locking. Complements the pgxmock-based unit tests in household_service_test.go
// (which verify orchestration/branching) and the repository-layer concurrency proof in
// internal/repository/household_accept_concurrency_integration_test.go.
//
// Per project rule: pgxmock never catches constraint/type errors — this is the real-DB
// counterpart (see memory feedback_pgxmock_integration_coverage.md).
//
// Run:
//
//	DATABASE_URL="postgres://..." go test ./internal/services/ -run TestHouseholdServiceIntegration -v

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/require"
)

func setupHouseholdServiceIntegrationDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

// insertIntegrationTestUser creates a real user row via the real ProfileRepository
// (so email encryption/blind-indexing round-trips exactly as production does). Callers
// are responsible for cleanup (see the DELETE FROM users t.Cleanup in the test body).
func insertIntegrationTestUser(t *testing.T, profileRepo repository.ProfileRepository, email, tier, status string) *domain.User {
	t.Helper()
	user := &domain.User{
		ClerkUserID:        "clerk_" + email,
		Email:              email,
		EmailVerified:      true,
		SubscriptionTier:   tier,
		SubscriptionStatus: status,
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	require.NoError(t, profileRepo.CreateUser(context.Background(), user))
	return user
}

func TestHouseholdServiceIntegration_AcceptInvite_FullHappyPath(t *testing.T) {
	pool := setupHouseholdServiceIntegrationDB(t)
	ctx := context.Background()
	enc := newTestFieldEncryptor(t)

	profileRepo := repository.NewProfileRepository(pool, enc)
	householdRepo := repository.NewHouseholdRepository(pool)
	inviteRepo := repository.NewHouseholdInviteRepository(pool, enc)
	shareRepo := repository.NewHouseholdShareRepository(pool)
	entRepo := repository.NewEntitlementRepository(pool)
	tierConfig := testTierConfig(t)
	entSvc := NewHouseholdEntitlementService(entRepo, profileRepo, householdRepo, tierConfig)
	emailSvc := NewEmailServiceMock("", "", "https://app.example.com")

	svc := NewHouseholdService(pool, householdRepo, inviteRepo, shareRepo, entRepo, entSvc, profileRepo, tierConfig, emailSvc, enc)

	owner := insertIntegrationTestUser(t, profileRepo, "owner-"+t.Name()+"@example.com", "household_2", "active")
	invitee := insertIntegrationTestUser(t, profileRepo, "invitee-"+t.Name()+"@example.com", "premium", "trial")
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM users WHERE id = ANY($1)`, []string{owner.ID, invitee.ID}) //nolint:errcheck
	})

	household, err := svc.CreateHousehold(ctx, owner.ID, "Integration Test Household")
	require.NoError(t, err)
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM households WHERE id = $1`, household.ID) //nolint:errcheck
	})

	// Give the invitee one account so the default-share fan-out has something to do.
	var acctID string
	err = pool.QueryRow(ctx, `
		INSERT INTO user_accounts (user_id, institution_id, institution_id_blind_idx)
		VALUES ($1, 'enc_inst', 'blind_inst_'||gen_random_uuid()::text)
		RETURNING id::text
	`, invitee.ID).Scan(&acctID)
	require.NoError(t, err)
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM user_accounts WHERE id = $1`, acctID) //nolint:errcheck
	})

	inviteResult, err := svc.CreateInvite(ctx, owner.ID, invitee.Email, "https://app.example.com")
	require.NoError(t, err)
	rawToken := inviteResult.InviteLink[len("https://app.example.com/households/invites/"):]

	acceptResult, err := svc.AcceptInvite(ctx, invitee.ID, rawToken, true, nil)
	require.NoError(t, err)
	require.Equal(t, household.ID, acceptResult.Household.ID)

	// Membership row exists and is active.
	member, err := householdRepo.GetActiveMember(ctx, household.ID, invitee.ID)
	require.NoError(t, err)
	require.Equal(t, domain.HouseholdRoleMember, member.Role)

	// Consent row was written with a non-empty snapshot.
	var snapshotRaw []byte
	err = pool.QueryRow(ctx, `SELECT snapshot FROM household_share_consents WHERE household_id = $1 AND user_id = $2`,
		household.ID, invitee.ID).Scan(&snapshotRaw)
	require.NoError(t, err)
	var snapshot map[string][]string
	require.NoError(t, json.Unmarshal(snapshotRaw, &snapshot))
	require.Contains(t, snapshot["shared"], acctID)

	// Account share was created as 'shared' (not hidden).
	var visibility string
	err = pool.QueryRow(ctx, `SELECT visibility FROM household_account_shares WHERE household_id = $1 AND user_account_id = $2`,
		household.ID, acctID).Scan(&visibility)
	require.NoError(t, err)
	require.Equal(t, "shared", visibility)

	// Household entitlement was written for the invitee, sourced from the owner.
	entitlement, err := entRepo.GetActiveEntitlement(ctx, invitee.ID)
	require.NoError(t, err)
	require.Equal(t, domain.EntitlementSourceHousehold, entitlement.Source)
	require.Equal(t, owner.ID, *entitlement.PayerUserID)
	require.Equal(t, "household_2", entitlement.Tier)

	// Invite flipped to accepted.
	var inviteStatus string
	var acceptedAt *time.Time
	err = pool.QueryRow(ctx, `SELECT status, accepted_at FROM household_invites WHERE household_id = $1 AND inviter_user_id = $2`,
		household.ID, owner.ID).Scan(&inviteStatus, &acceptedAt)
	require.NoError(t, err)
	require.Equal(t, "accepted", inviteStatus)
	require.NotNil(t, acceptedAt)
}
