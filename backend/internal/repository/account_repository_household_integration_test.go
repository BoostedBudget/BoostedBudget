// account_repository_household_integration_test.go — DB-backed behavior for
// AccountRepository.GetForHouseholdScope, added by #1369 (Phase 5) so household balance
// aggregation never has to fall back to the single-user AccountRepository.GetByUserID
// (Codex adversarial review, P0-2: that would silently drop every non-requester member's
// accounts from the household aggregate).
//
// Run:
//
//	DATABASE_URL="postgres://..." go test ./internal/repository/ -run TestAccountRepository_GetForHouseholdScope -v
package repository_test

import (
	"context"
	"testing"
	"time"

	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// plainPassthroughEncryptionService is never actually invoked by FieldEncryptor for the
// plain (unencrypted) test values these fixtures insert — DecryptString passes through
// any value not prefixed "ENC$" — but NewAccountRepository requires a non-nil
// *FieldEncryptor, so a minimal valid instance is needed regardless.
type plainPassthroughEncryptionService struct{}

func (plainPassthroughEncryptionService) Encrypt(plaintext string) (string, error) { return plaintext, nil }
func (plainPassthroughEncryptionService) Decrypt(ciphertext string) (string, error) { return ciphertext, nil }

func newTestFieldEncryptor(t *testing.T) *appcrypto.FieldEncryptor {
	t.Helper()
	enc, err := appcrypto.NewFieldEncryptor(plainPassthroughEncryptionService{}, []byte("12345678901234567890123456789012"))
	require.NoError(t, err)
	return enc
}

func TestAccountRepository_GetForHouseholdScope_IncludesSharedMemberAccount(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewAccountRepository(pool, newTestFieldEncryptor(t))

	owner, member, hh, ownerAcct, memberAcct := setupTwoMemberSharedHousehold(t, pool)

	accounts, err := repo.GetForHouseholdScope(ctx, owner, &domain.HouseholdScope{
		HouseholdID:   hh,
		MemberUserIDs: []string{owner, member},
	}, time.Now().UTC())
	require.NoError(t, err)

	ids := make(map[string]bool, len(accounts))
	for _, a := range accounts {
		ids[a.ID] = true
		assert.Equal(t, domain.AccountStatusActive, a.Status)
	}
	assert.True(t, ids[ownerAcct.AccountID], "owner's shared account must be included")
	assert.True(t, ids[memberAcct.AccountID], "member's shared account must be included — not just the requester's own")
}

func TestAccountRepository_GetForHouseholdScope_ExcludesHiddenVisibilityShare(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewAccountRepository(pool, newTestFieldEncryptor(t))

	owner := insertTestUser(t, pool)
	member := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)
	memberAcct := setupHouseholdMemberAccount(t, pool, member)
	shareRepo := repository.NewHouseholdShareRepository(pool)
	require.NoError(t, shareRepo.InsertShare(ctx, hh, memberAcct.UserAccountID, nil, domain.ShareVisibilityHidden))

	accounts, err := repo.GetForHouseholdScope(ctx, owner, &domain.HouseholdScope{
		HouseholdID:   hh,
		MemberUserIDs: []string{owner, member},
	}, time.Now().UTC())
	require.NoError(t, err)
	for _, a := range accounts {
		assert.NotEqual(t, memberAcct.AccountID, a.ID, "hidden-visibility share must be excluded")
	}
}

// TestAccountRepository_GetForHouseholdScope_JointAccountWinnerRespectsAsOfCutoff closes a
// gap found by Codex adversarial review during #1369 gap-closure G1: adding the asOf param
// to GetForHouseholdScope was verified only via unit-test mocks and a same-instant
// integration test — nothing proved the underlying SQL actually switches which member's
// copy of a joint account wins the fingerprint dedup as asOf varies. Mirrors
// TestBalanceSnapshotRepository_GetSnapshotsAsOfForHousehold_JointAccountWinnerRespectsAsOfCutoff.
func TestAccountRepository_GetForHouseholdScope_JointAccountWinnerRespectsAsOfCutoff(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewAccountRepository(pool, newTestFieldEncryptor(t))

	owner, member, hh, ownerAcct, memberAcct := setupTwoMemberSharedHousehold(t, pool)
	now := time.Now().UTC()
	fingerprint := "joint-fp-" + hh

	shareRepo := repository.NewHouseholdShareRepository(pool)
	require.NoError(t, shareRepo.RevokeSharesForUser(ctx, hh, owner))
	require.NoError(t, shareRepo.RevokeSharesForUser(ctx, hh, member))
	require.NoError(t, shareRepo.InsertShare(ctx, hh, ownerAcct.UserAccountID, &fingerprint, domain.ShareVisibilityShared))
	require.NoError(t, shareRepo.InsertShare(ctx, hh, memberAcct.UserAccountID, &fingerprint, domain.ShareVisibilityShared))

	// Owner's copy: only has an OLD snapshot (40 days ago) — no recent activity.
	insertBalanceSnapshot(t, pool, owner, ownerAcct.AccountID, ownerAcct.UserAccountID, 100, now.AddDate(0, 0, -40))
	// Member's copy: globally freshest (synced TODAY), but has NO snapshot before 35 days ago.
	insertBalanceSnapshot(t, pool, member, memberAcct.AccountID, memberAcct.UserAccountID, 250, now)

	scope := &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner, member}}

	// asOf = 35 days ago: member's copy has no snapshot before this cutoff, so the winner
	// must resolve to owner's pre-cutoff copy, not silently drop the joint account.
	accounts, err := repo.GetForHouseholdScope(ctx, owner, scope, now.AddDate(0, 0, -35))
	require.NoError(t, err)
	require.Len(t, accounts, 1, "joint account must still resolve to a single account row")
	assert.Equal(t, ownerAcct.AccountID, accounts[0].ID,
		"must resolve to owner's pre-cutoff copy, not member's globally-freshest-but-ineligible-as-of-cutoff copy")

	// asOf = today: member's copy is eligible now and is the freshest overall.
	current, err := repo.GetForHouseholdScope(ctx, owner, scope, now)
	require.NoError(t, err)
	require.Len(t, current, 1)
	assert.Equal(t, memberAcct.AccountID, current[0].ID, "as of today, member's freshest copy must win")
}
