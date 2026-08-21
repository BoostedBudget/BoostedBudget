//go:build integration

package repository

// #1566 criterion (e) — the uniqueness question on user_identities.clerk_user_id_blind_idx, and the
// non-determinism it exposes in GetByClerkID.
//
// THE SETUP, all three facts verified rather than assumed:
//
//  1. `clerk_user_id_blind_idx` carries a NON-unique index (migration
//     000053_add_encryption_support.up.sql:114), while the equivalent column on `users` was made
//     UNIQUE by 000061_fix_encryption_constraints.up.sql:27.
//  2. The inherited `UNIQUE` on the encrypted clerk_user_id column (migration
//     000008_create_user_identities_table.up.sql:15) is INERT against duplicate plaintexts:
//     encryption is AES-GCM with a random nonce (services/encryption_service.go), so the same
//     plaintext yields a different ciphertext every time. BlindIndex, by contrast, is a
//     deterministic HMAC-SHA256 (crypto/field_encryptor.go:130).
//  3. No PRODUCTION path can create two rows sharing a blind index. This is a correction: point 3
//     used to read "two identity rows for one user with DIFFERENT providers is the designed case …
//     and both rows carry the same clerk_user_id, hence the same blind index". That is false in its
//     own terms. `provider` is DERIVED FROM the Clerk ID — domain.NewUserIdentity calls
//     ParseExternalUserID (domain/user_identity.go:43) — and is the only non-test construction site
//     (services/registration_service.go:184), with the only inserts at registration_service.go:205
//     and :223, one per registration. Two rows sharing a blind index therefore share the plaintext
//     Clerk ID, hence the derived provider, hence violate
//     `unique_user_provider UNIQUE(user_id, provider)`
//     (000008_create_user_identities_table.up.sql:26). Different providers mean different Clerk IDs
//     mean different blind indices.
//
//     What CAN produce duplicates is state written outside this code:
//     000008_create_user_identities_table.up.sql:47-65 backfills one identity per pre-existing user,
//     and fixtures insert rows directly (apps/web/scripts/seed-e2e-data.sql). Legacy, backfilled, or
//     manually-inserted rows are the genuine risk, which is why the tests below model duplicates
//     with raw SQL rather than through the repository API.
//
// So multiple rows CAN share a blind index — as corrupt or legacy state, not by design. GetByClerkID
// used QueryRow with no ORDER BY and no LIMIT, which means the row it returned was whichever Postgres
// happened to hand back first.
//
// WHY #1566's SUGGESTED FIX IS THE WRONG INSTRUMENT, and this is the substance of criterion (e). The
// CONCLUSION survives the correction above; the reason does not. A bare UNIQUE index on
// clerk_user_id_blind_idx would not foreclose a designed feature — there is no such feature — it
// would forbid NOTHING REACHABLE, since no production path can produce a second row for one Clerk
// ID. So it hardens nothing, while adding a constraint that a backfill or a fixture could trip. The
// invariant that actually matters is CROSS-ROW ("all rows sharing a blind index belong to one user"),
// which no single-column unique index can express. Today it holds by construction, because
// `unique_users_clerk_id_blind` on the `users` table blocks the only reachable creation path
// (registration), so there is no live exposure. What IS live, and is fixed here, is that the row
// chosen among duplicate rows was arbitrary.
//
// Every consumer reads only identity.UserID today — middleware/clerk.go, huma/middleware/clerk.go,
// operations/mcp_audit.go:97, registration_service.go, identity_resolver_cached.go — and those rows
// share a user_id, which is why this was latent rather than a live bug. Provider, Email, IsPrimary
// and ID were nonetheless being returned at random, and the cached resolver memoises the result.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// newIdentityDeterminismTx sets up a rolled-back transaction with a user, mirroring the harness the
// null-email tests in this package use.
func newIdentityDeterminismTx(t *testing.T, ctx context.Context, suffix string) (pgx.Tx, UserIdentityRepository, *domain.User) {
	t.Helper()
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })

	profileRepo := NewProfileRepository(tx, enc)
	repo := NewUserIdentityRepository(tx, enc)

	user := &domain.User{
		ClerkUserID:        "clerk_determinism_user_" + suffix,
		Email:              "determinism-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	require.NoError(t, profileRepo.CreateUserTx(ctx, tx, user))

	return tx, repo, user
}

// TestGetByClerkID_MultipleIdentitiesSameBlindIndex_ReturnsPrimaryDeterministically pins that the
// lookup resolves to the PRIMARY identity rather than to whichever row Postgres returns first.
//
// The non-primary row is inserted FIRST on purpose. Without an ORDER BY, a sequential scan hands
// back insertion order, so the pre-fix query returns the non-primary row and this test fails
// concretely rather than flakily. That is a property of the plan rather than of the contract, so it
// is a falsification aid, not the thing being asserted — the assertion is simply "the primary wins,
// every time".
func TestGetByClerkID_MultipleIdentitiesSameBlindIndex_ReturnsPrimaryDeterministically(t *testing.T) {
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	tx, repo, user := newIdentityDeterminismTx(t, ctx, suffix)

	clerkID := "clerk_shared_" + suffix

	// Inserted first, deliberately: the SECONDARY provider identity.
	secondary := &domain.UserIdentity{
		UserID:         user.ID,
		ClerkUserID:    clerkID,
		Provider:       "apple",
		ProviderUserID: "apple_" + suffix,
		Email:          "apple-" + suffix + "@example.com",
		IsPrimary:      false,
	}
	require.NoError(t, repo.Create(ctx, secondary))

	// The PRIMARY row must be inserted with raw SQL, and the reason is a finding in its own right.
	//
	// repo.Create() cannot produce this state IN TESTS. `user_identities_auth0_user_id_key` is
	// UNIQUE(clerk_user_id), and the test harness wires plainPassthroughEncryptionService
	// (test_helpers_test.go), so clerk_user_id is stored as PLAINTEXT and the second insert of the
	// same Clerk ID is rejected. My first attempt at this test died exactly there.
	//
	// In PRODUCTION the same insert succeeds: encryptionService uses AES-GCM with a random nonce
	// (services/encryption_service.go), so the same plaintext yields a different ciphertext each
	// time and the UNIQUE constraint cannot see the duplicate — while clerk_user_id_blind_idx, a
	// deterministic HMAC, is IDENTICAL for both rows. That is the production state modelled below:
	// distinct ciphertexts, one shared blind index.
	//
	// ⚠ So the test environment is STRICTER than production here, and that gap hides a whole class
	// of duplicate from every test that goes through the repository API. Worth knowing before
	// trusting any other "the UNIQUE prevents that" argument about an encrypted column.
	var sharedBlindIdx string
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT clerk_user_id_blind_idx FROM user_identities WHERE id = $1`, secondary.ID,
	).Scan(&sharedBlindIdx))
	require.NotEmpty(t, sharedBlindIdx, "the blind index must be populated for this test to mean anything")

	primaryID := uuid.NewString()
	_, err := tx.Exec(ctx, `
		INSERT INTO user_identities (
			id, user_id, clerk_user_id, provider, provider_user_id,
			email, email_verified, is_primary,
			created_at, updated_at, last_used_at, clerk_user_id_blind_idx
		) VALUES ($1, $2, $3, 'google-oauth2', $4, $5, true, true, now(), now(), now(), $6)
	`, primaryID, user.ID,
		// A DIFFERENT stored value for the same logical Clerk ID — what a random nonce produces.
		"ENC$distinct-ciphertext-for-the-same-clerk-id-"+suffix,
		"googleoauth_"+suffix, "googleoauth-"+suffix+"@example.com",
		sharedBlindIdx)
	require.NoError(t, err, "modelling the production shape: two rows, distinct ciphertexts, one blind index")

	primary := &domain.UserIdentity{ID: primaryID, IsPrimary: true, Provider: "google-oauth2"}

	// The premise, asserted rather than assumed: both rows really do share one blind index. If a
	// future migration makes that impossible, this fails here and the rest of the test is moot.
	var shared int
	require.NoError(t, tx.QueryRow(ctx, `
		SELECT COUNT(DISTINCT clerk_user_id_blind_idx) FROM user_identities WHERE user_id = $1
	`, user.ID).Scan(&shared))
	require.Equal(t, 1, shared,
		"the two identities must share ONE blind index for this test to mean anything; if they no "+
			"longer do, the non-determinism it guards against has been removed by other means")

	var rows int
	require.NoError(t, tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM user_identities WHERE user_id = $1
	`, user.ID).Scan(&rows))
	require.Equal(t, 2, rows, "both provider identities must exist")

	// Repeated because the defect is non-determinism: one call agreeing proves little.
	for i := 0; i < 5; i++ {
		got, err := repo.GetByClerkID(ctx, clerkID)
		require.NoError(t, err)
		assert.Equal(t, primary.ID, got.ID,
			"call %d returned identity %q (provider %q, is_primary=%v) instead of the PRIMARY %q. "+
				"Several rows share this blind index by design (one per provider), so without an "+
				"ORDER BY the row returned is whichever Postgres hands back first.",
			i+1, got.ID, got.Provider, got.IsPrimary, primary.ID)
		assert.True(t, got.IsPrimary, "call %d did not return the primary identity", i+1)
		assert.Equal(t, "google-oauth2", got.Provider, "call %d returned the wrong provider", i+1)
	}
}
