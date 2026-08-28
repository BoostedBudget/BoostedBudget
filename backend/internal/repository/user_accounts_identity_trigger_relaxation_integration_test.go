//go:build integration

// user_accounts_identity_trigger_relaxation_integration_test.go — #1569 round 12 task 1b.
//
// Migration 000177 installed trg_user_accounts_identity_immutable using
// `NEW.col IS DISTINCT FROM OLD.col`, which treats a NULL -> value transition as a "change" and
// rejects it exactly like a value -> different-value transition. Two independent round-12 findings
// (the repaired migration-idempotency gate turning RED on 000177's own backfill replay, and a
// from-the-other-direction audit of FindByInstitutionAndMask) converged on the same root cause: the
// trigger is stricter than the feature's own intended semantic, which is "immutable ONCE SET" —
// i.e. NULL -> value must be ALLOWED (nothing has been fixed yet; there is nothing to protect), and
// value -> different-value must stay REJECTED (the frozen-identity guarantee this trigger exists
// for). Migration 000179 replaces the trigger function to implement that semantic.
//
// This file proves BOTH directions against real Postgres, per the round-12 brief: a fix that only
// proves the newly-allowed transition, without re-proving the retained prohibition, has silently
// removed a guarantee. It also proves the previously-undocumented "vice versa" edge case the brief
// asked to be reasoned about explicitly: once identity_type is set (non-NULL), identity_subtype can
// no longer be changed EVEN IF identity_subtype's OWN old value is still NULL — a genuinely,
// permanently subtype-less identity (e.g. type='other') must not become a back door that lets a
// later write silently attach a fabricated subtype after mint.
//
// Run:
//
//	DATABASE_URL="postgres://devuser:devpass@localhost:5434/boostedbudget?sslmode=disable" \
//	  go test ./internal/repository/ -run TestUserAccountsIdentityTrigger -v -tags integration
package repository_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// updateIdentity attempts `UPDATE user_accounts SET identity_type = $2, identity_subtype = $3
// WHERE id = $1` and returns the error (nil on success).
func updateIdentity(t *testing.T, pool *pgxpool.Pool, id string, identityType, identitySubtype *string) error {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		UPDATE user_accounts SET identity_type = $2, identity_subtype = $3 WHERE id = $1
	`, id, identityType, identitySubtype)
	return err
}

// idTriggerStrp is a local *string helper (data_export_encrypted_columns_integration_test.go
// already defines `strp` in this package, so this file needs its own name).
func idTriggerStrp(s string) *string { return &s }

// TestUserAccountsIdentityTrigger_AllowsFirstTimeClaim proves the FIX: a row whose identity_type
// and identity_subtype are both still NULL (never minted, or a legacy orphan per migration
// 000177's header) can have both columns set for the first time. Before migration 000179 this was
// rejected by `IS DISTINCT FROM`, which treats NULL -> value as a change; this is the specific
// defect round 12 found (the migration-idempotency gate turning RED on 000177's own backfill
// replay, and FindByInstitutionAndMask's orphan wildcard being permanently unrepairable).
func TestUserAccountsIdentityTrigger_AllowsFirstTimeClaim(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	userID := insertTestUser(t, pool)
	id := insertTestUserAccount(t, pool, userID)

	err := updateIdentity(t, pool, id, idTriggerStrp("depository"), idTriggerStrp("checking"))
	assert.NoError(t, err, "NULL -> value must be ALLOWED — this is the first-time identity claim "+
		"the fix exists to permit")
}

// TestUserAccountsIdentityTrigger_StillRejectsChangeOfAnAlreadySetType proves the RETAINED
// guarantee: once identity_type is non-NULL, changing it to a DIFFERENT value is still rejected.
// A fix that only proves the newly-allowed transition without re-proving this would have silently
// dropped the frozen-identity invariant instead of narrowing it.
func TestUserAccountsIdentityTrigger_StillRejectsChangeOfAnAlreadySetType(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	userID := insertTestUser(t, pool)
	id := insertTestUserAccount(t, pool, userID)

	require.NoError(t, updateIdentity(t, pool, id, idTriggerStrp("depository"), idTriggerStrp("checking")))

	err := updateIdentity(t, pool, id, idTriggerStrp("investment"), idTriggerStrp("brokerage"))
	require.Error(t, err, "value -> different value must STILL be rejected once identity_type is set")
	assert.Contains(t, err.Error(), "frozen")
}

// TestUserAccountsIdentityTrigger_StillRejectsChangeOfAnAlreadySetSubtype is the subtype-only
// mirror of the above: identity_type held constant, only identity_subtype changes.
func TestUserAccountsIdentityTrigger_StillRejectsChangeOfAnAlreadySetSubtype(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	userID := insertTestUser(t, pool)
	id := insertTestUserAccount(t, pool, userID)

	require.NoError(t, updateIdentity(t, pool, id, idTriggerStrp("depository"), idTriggerStrp("checking")))

	err := updateIdentity(t, pool, id, idTriggerStrp("depository"), idTriggerStrp("savings"))
	require.Error(t, err, "identity_subtype must STILL be immutable once identity_type is set")
	assert.Contains(t, err.Error(), "frozen")
}

// TestUserAccountsIdentityTrigger_RejectsLateSubtypeClaimOnceTypeIsSet is the "vice versa" edge
// case the round-12 brief explicitly asked to be reasoned about: a row minted with a genuinely,
// permanently NULL subtype (identity_type='other', identity_subtype=NULL — a real, valid mint, not
// an unclaimed orphan) must NOT allow a later write to attach a subtype. Gating solely on
// "identity_subtype's own old value is NULL" would wrongly treat this as still-claimable and open a
// back door; the correct gate is whether the ROW (identity_type) has already been minted.
func TestUserAccountsIdentityTrigger_RejectsLateSubtypeClaimOnceTypeIsSet(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	userID := insertTestUser(t, pool)
	id := insertTestUserAccount(t, pool, userID)

	// Mint with a genuinely, permanently NULL subtype.
	require.NoError(t, updateIdentity(t, pool, id, idTriggerStrp("other"), nil))

	err := updateIdentity(t, pool, id, idTriggerStrp("other"), idTriggerStrp("fabricated"))
	require.Error(t, err, "a subtype that was genuinely NULL at mint must not become a back door "+
		"for a later write to attach a fabricated value")
	assert.Contains(t, err.Error(), "frozen")
}

// TestUserAccountsIdentityTrigger_AllowsNoopRewrite proves a same-value UPDATE (no actual change)
// is never blocked, on an already-minted row — a defensive sanity check, not a new requirement.
func TestUserAccountsIdentityTrigger_AllowsNoopRewrite(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	userID := insertTestUser(t, pool)
	id := insertTestUserAccount(t, pool, userID)

	require.NoError(t, updateIdentity(t, pool, id, idTriggerStrp("depository"), idTriggerStrp("checking")))

	err := updateIdentity(t, pool, id, idTriggerStrp("depository"), idTriggerStrp("checking"))
	assert.NoError(t, err, "rewriting the SAME value must not be rejected")
}
