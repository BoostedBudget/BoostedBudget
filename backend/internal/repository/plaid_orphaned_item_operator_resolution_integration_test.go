//go:build integration

package repository

// Issue #1497 item 2 — the OPERATOR ATTESTATION, against a real PostgreSQL 17.
//
// WHY INTEGRATION AND NOT pgxmock. Every claim this file makes is a claim about what one UPDATE did
// and did NOT touch — that the attestation left access_token_encrypted, token_purged_at and
// resolved_at exactly as they were, and that the retry sweep's own selection still returns the row.
// pgxmock matches query text and argument shape, so it would happily pass an UPDATE that also nulled
// the credential, one that also stamped resolved_at, and one whose first-wins predicate was missing.
// Two of the constraints asserted here exist only in the database (migration 000137).
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestPlaidOrphanedItem -tags integration -v -p 1

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// attestationState reads, straight from the row, every column an attestation must and must not have
// moved. One helper so no test can assert half of it.
type attestationState struct {
	operatorResolvedAt   *time.Time
	operatorResolvedBy   *string
	operatorNote         *string
	resolvedAt           *time.Time
	accessTokenEncrypted *string
	tokenPurgedAt        *time.Time
	retryAttemptCount    int
}

func readAttestationState(t *testing.T, ctx context.Context, repo *plaidOrphanedItemRepository, id string) attestationState {
	t.Helper()
	var s attestationState
	require.NoError(t, repo.db.QueryRow(ctx, `
		SELECT operator_resolved_at, operator_resolved_by, operator_resolution_note,
		       resolved_at, access_token_encrypted, token_purged_at, retry_attempt_count
		FROM plaid_orphaned_items WHERE id = $1`, id).
		Scan(&s.operatorResolvedAt, &s.operatorResolvedBy, &s.operatorNote,
			&s.resolvedAt, &s.accessTokenEncrypted, &s.tokenPurgedAt, &s.retryAttemptCount))
	return s
}

// ─── RED 1: the attestation records the operator fact and NOTHING ELSE ───────────────────────────
//
// THE HEADLINE TEST FOR ITEM 2. An operator's claim is not a proof, so it must not produce any of the
// artefacts a proof produces: no resolved_at, no purge, no token_purged_at stamp. If it did, the admin
// surface and the compliance readers could no longer tell a verified removal from an unverified one —
// which is the single defect this design was chosen to avoid.
func TestPlaidOrphanedItemRepository_Integration_RecordOperatorResolution_LeavesResolvedAtAndTheCredentialUntouched(t *testing.T) {
	repo, ctx := newOrphanRepo(t)
	asOf := time.Now().UTC()

	id := seedOrphanRow(t, ctx, repo, orphanFixture{
		plaidItemID: "item-attest-" + uuid.NewString(), accessToken: "tok-attest",
		orphanedAt: asOf, tokenExpiresAt: asOf.Add(domain.OrphanedItemTokenRetention),
		attemptCount: 4,
	})

	before := readAttestationState(t, ctx, repo, id)
	require.NotNil(t, before.accessTokenEncrypted, "the fixture must actually hold a credential, or this test proves nothing")
	require.Nil(t, before.resolvedAt)
	require.Nil(t, before.operatorResolvedAt)

	require.NoError(t, repo.RecordOperatorResolution(ctx, id,
		"user_2attestingAdmin", "Removed the Item by hand in Plaid's dashboard.", asOf))

	after := readAttestationState(t, ctx, repo, id)

	// The fact IS recorded, attributably.
	require.NotNil(t, after.operatorResolvedAt)
	assert.WithinDuration(t, asOf, after.operatorResolvedAt.UTC(), time.Second)
	require.NotNil(t, after.operatorResolvedBy)
	assert.Equal(t, "user_2attestingAdmin", *after.operatorResolvedBy,
		"an attestation nobody is named for cannot be checked later, which is its only mitigation")
	require.NotNil(t, after.operatorNote)
	assert.Equal(t, "Removed the Item by hand in Plaid's dashboard.", *after.operatorNote)

	// And NOTHING a proof would have moved has moved.
	assert.Nil(t, after.resolvedAt,
		"a human's claim must never be laundered into resolved_at, which means Plaid itself answered")
	require.NotNil(t, after.accessTokenEncrypted,
		"an attestation must NOT purge the credential: it proves nothing, and the credential is the only "+
			"means by which the claim could ever be confirmed by a retry")
	assert.Equal(t, *before.accessTokenEncrypted, *after.accessTokenEncrypted)
	assert.Nil(t, after.tokenPurgedAt,
		"and it must not claim a purge happened either — token_purged_at is what every 'the credential is gone' claim reads")
	assert.Equal(t, before.retryAttemptCount, after.retryAttemptCount,
		"an operator acting at Plaid is not an automated attempt and must not be counted as one")
}

// ─── RED 2 (SIBLING, MANDATORY): the PROVED path still does all of it ────────────────────────────
//
// ⚠ WHY THIS TEST IS NOT OPTIONAL. Every assertion above is of the form "X did not happen", and a
// resolution path that was broken outright — one that stamped nothing and purged nothing — would
// satisfy all of them. This is the positive control: on the very same row shape, and even on a row
// that has ALREADY been attested, MarkResolved must still stamp resolved_at and still purge. It is
// also the SQL-level half of the coexistence pin (the domain-level half asserts the state ordering).
func TestPlaidOrphanedItemRepository_Integration_MarkResolved_StillProvesAndPurges_EvenOnAnAttestedRow(t *testing.T) {
	repo, ctx := newOrphanRepo(t)
	asOf := time.Now().UTC()

	id := seedOrphanRow(t, ctx, repo, orphanFixture{
		plaidItemID: "item-both-" + uuid.NewString(), accessToken: "tok-both",
		orphanedAt: asOf, tokenExpiresAt: asOf.Add(domain.OrphanedItemTokenRetention),
	})
	require.NoError(t, repo.RecordOperatorResolution(ctx, id,
		"user_2attestingAdmin", "I think I removed this one already.", asOf.Add(-time.Hour)))

	updated, err := repo.MarkResolved(ctx, id, asOf)
	require.NoError(t, err)
	assert.True(t, updated,
		"an attestation must not make the row invisible to the sweep's resolution path — first-wins is on "+
			"resolved_at, and an attested row has never been resolved")

	after := readAttestationState(t, ctx, repo, id)
	require.NotNil(t, after.resolvedAt, "the proved-removal path must still record the proof")
	assert.Nil(t, after.accessTokenEncrypted, "and must still purge the credential in the same statement")
	assert.NotNil(t, after.tokenPurgedAt)
	assert.Equal(t, 1, after.retryAttemptCount)

	// The claim SURVIVES the proof rather than being overwritten by it: who attested, and when, stays
	// answerable — the row now carries both facts, at their true strengths.
	require.NotNil(t, after.operatorResolvedAt)
	assert.WithinDuration(t, asOf.Add(-time.Hour), after.operatorResolvedAt.UTC(), time.Second)

	// The state derivation over the row reports the STRONGER fact. This is the coexistence pin.
	rows, _, err := repo.List(ctx, 500, 0)
	require.NoError(t, err)
	var found bool
	for _, r := range rows {
		if r.ID != id {
			continue
		}
		found = true
		assert.Equal(t, domain.OrphanedItemResolved, r.AdminState(asOf),
			"proof beats claim: a row Plaid confirmed removed must not report as an unverified human assertion")
	}
	require.True(t, found, "the row must still be visible on the admin surface — a resolved row is history, not invisible")
}

// FIRST-WINS, mirroring MarkResolved's own refusal to overwrite. "Who said this, and when" must stay
// answerable, so a second attestation neither moves the timestamp nor replaces the attester or note —
// and the caller is TOLD, so it can report to the second operator that their note was not recorded
// rather than silently discarding it.
func TestPlaidOrphanedItemRepository_Integration_RecordOperatorResolution_IsFirstWins(t *testing.T) {
	repo, ctx := newOrphanRepo(t)
	asOf := time.Now().UTC()

	id := seedOrphanRow(t, ctx, repo, orphanFixture{
		plaidItemID: "item-twice-attest-" + uuid.NewString(), accessToken: "tok",
		orphanedAt: asOf, tokenExpiresAt: asOf.Add(domain.OrphanedItemTokenRetention),
	})

	require.NoError(t, repo.RecordOperatorResolution(ctx, id, "user_first", "First operator's account.", asOf))

	err := repo.RecordOperatorResolution(ctx, id, "user_second", "Second operator's account.", asOf.Add(time.Hour))
	require.ErrorIs(t, err, ErrOrphanedItemAlreadyAttested,
		"the second writer must be told, not silently ignored: their note would otherwise vanish")

	after := readAttestationState(t, ctx, repo, id)
	assert.WithinDuration(t, asOf, after.operatorResolvedAt.UTC(), time.Second)
	assert.Equal(t, "user_first", *after.operatorResolvedBy)
	assert.Equal(t, "First operator's account.", *after.operatorNote)
}

// A write against an id that is not in the table is distinguishable from an already-attested one, so
// the endpoint can answer 404 rather than a misleading conflict.
func TestPlaidOrphanedItemRepository_Integration_RecordOperatorResolution_UnknownIDIsNotFound(t *testing.T) {
	repo, ctx := newOrphanRepo(t)

	err := repo.RecordOperatorResolution(ctx, uuid.NewString(), "user_x", "note", time.Now().UTC())
	assert.ErrorIs(t, err, ErrOrphanedItemNotFound)
}

// ─── RED 5: THE DECIDED BEHAVIOUR OF THE AUTOMATED SWEEP ─────────────────────────────────────────
//
// ⚠ THE DECISION, ASSERTED RATHER THAN LEFT IMPLICIT: an operator attestation DOES NOT remove a row
// from the retry sweep's selection. GetRetryablePage's predicate is unchanged, and this test is what
// makes that a decision instead of an omission — see that function's comment for the argument. The
// one-line version: continuing to retry is the ONLY mechanism that can upgrade an unverified claim
// into a proved removal, it self-heals (a genuinely-removed Item comes back ITEM_NOT_FOUND, which
// stamps resolved_at), and stopping on a claim would let one mistaken click permanently silence the
// only automated path on an Item that may still be billing an erased user.
func TestPlaidOrphanedItemRepository_Integration_GetRetryablePage_StillReturnsOperatorAttestedRows(t *testing.T) {
	repo, ctx := newOrphanRepo(t)
	asOf := time.Now().UTC()
	resolvedAt := asOf.Add(-time.Hour)

	attested := seedOrphanRow(t, ctx, repo, orphanFixture{
		plaidItemID: "item-attested-retryable-" + uuid.NewString(), accessToken: "tok-attested",
		orphanedAt: asOf, tokenExpiresAt: asOf.Add(domain.OrphanedItemTokenRetention),
	})
	require.NoError(t, repo.RecordOperatorResolution(ctx, attested, "user_2attestingAdmin", "Handled at Plaid.", asOf))

	// The control, and it is what makes this test falsifiable rather than a tautology: a row that IS
	// proved-resolved must still be excluded. Without it, a predicate that had stopped filtering
	// anything at all would satisfy the assertion above.
	proved := seedOrphanRow(t, ctx, repo, orphanFixture{
		plaidItemID: "item-proved-" + uuid.NewString(), accessToken: "tok-proved",
		orphanedAt: asOf, tokenExpiresAt: asOf.Add(domain.OrphanedItemTokenRetention),
		resolvedAt: &resolvedAt,
	})

	rows, err := repo.GetRetryablePage(ctx, asOf, nil, 500)
	require.NoError(t, err)

	got := map[string]domain.PlaidOrphanedItem{}
	for _, r := range rows {
		got[r.ID] = r
	}
	assert.Contains(t, got, attested,
		"DECIDED: an attestation is not proof, so the sweep keeps trying — that is what can turn the claim into a proof")
	assert.NotContains(t, got, proved,
		"and resolved_at still excludes a row, or the assertion above would mean nothing")

	// The attestation must also survive the read, so the sweep's own logging and any future decision
	// over it sees the same row the admin surface does.
	require.NotNil(t, got[attested].OperatorResolvedAt)
	assert.Equal(t, "user_2attestingAdmin", got[attested].OperatorResolvedBy)
}

// The admin read carries the attestation, decrypted-and-complete, and derives the fourth state from
// it. This is where the "show which kind of resolution it was" requirement is actually observable
// end-to-end over real SQL.
func TestPlaidOrphanedItemRepository_Integration_List_ReportsTheOperatorAttestedState(t *testing.T) {
	repo, ctx := newOrphanRepo(t)
	asOf := time.Now().UTC()

	// A row whose credential is already spent, so WITHOUT the attestation it is unambiguously
	// manual_intervention_required — which the control assertion below relies on.
	id := seedOrphanRow(t, ctx, repo, orphanFixture{
		plaidItemID: "item-list-attest-" + uuid.NewString(),
		orphanedAt:  asOf.Add(-31 * 24 * time.Hour), tokenExpiresAt: asOf.Add(-24 * time.Hour),
	})

	rows, _, err := repo.List(ctx, 500, 0)
	require.NoError(t, err)
	var control *domain.PlaidOrphanedItem
	for i := range rows {
		if rows[i].ID == id {
			control = &rows[i]
		}
	}
	require.NotNil(t, control)
	require.Equal(t, domain.OrphanedItemManualInterventionRequired, control.AdminState(asOf),
		"before the attestation this row must read as work for a human, or the assertion below proves nothing")

	require.NoError(t, repo.RecordOperatorResolution(ctx, id, "user_2attestingAdmin", "Removed at Plaid on the 4th.", asOf))

	rows, _, err = repo.List(ctx, 500, 0)
	require.NoError(t, err)
	var got *domain.PlaidOrphanedItem
	for i := range rows {
		if rows[i].ID == id {
			got = &rows[i]
		}
	}
	require.NotNil(t, got, "⚠ an attested row must stay VISIBLE and auditable — never filtered out of the admin list")
	assert.Equal(t, domain.OrphanedItemOperatorAttested, got.AdminState(asOf))
	assert.Equal(t, "user_2attestingAdmin", got.OperatorResolvedBy)
	assert.Equal(t, "Removed at Plaid on the 4th.", got.OperatorResolutionNote)

	view := got.AdminView(asOf)
	assert.Equal(t, domain.OrphanedItemOperatorAttested, view.AdminState)
	assert.Equal(t, "user_2attestingAdmin", view.OperatorResolvedBy)
}

// ─── The two CHECK constraints migration 000137 adds, asserted against the database itself ───────
//
// A constraint no test ever violates is indistinguishable from an absent one — the same argument
// TestPlaidOrphanedItems_Integration_PurgedTokenIsNullIsEnforcedByTheDatabase makes for 000135's.
func TestPlaidOrphanedItems_Integration_OperatorAttestationConstraintsAreEnforcedByTheDatabase(t *testing.T) {
	repo, ctx := newOrphanRepo(t)
	asOf := time.Now().UTC()

	id := seedOrphanRow(t, ctx, repo, orphanFixture{
		plaidItemID: "item-constraint-" + uuid.NewString(),
		orphanedAt:  asOf, tokenExpiresAt: asOf.Add(domain.OrphanedItemTokenRetention),
	})

	_, err := repo.db.Exec(ctx,
		`UPDATE plaid_orphaned_items SET operator_resolved_at = $2 WHERE id = $1`, id, asOf)
	assert.ErrorContains(t, err, "plaid_orphaned_items_operator_attestation_is_attributed",
		"an attestation with no attester must be refused by the database, not merely avoided by the code")

	_, err = repo.db.Exec(ctx,
		`UPDATE plaid_orphaned_items SET operator_resolved_by = $2 WHERE id = $1`, id, "user_x")
	assert.ErrorContains(t, err, "plaid_orphaned_items_operator_attestation_is_attributed",
		"and an attester with no attestation is meaningless bookkeeping")

	_, err = repo.db.Exec(ctx,
		`UPDATE plaid_orphaned_items SET operator_resolution_note = $2 WHERE id = $1`, id, "explains nothing")
	assert.ErrorContains(t, err, "plaid_orphaned_items_operator_note_needs_an_attestation",
		"a note on an unattested row would read as the explanation of a resolution that never happened")

	// The control: the legal write the constraints exist to permit.
	_, err = repo.db.Exec(ctx, `
		UPDATE plaid_orphaned_items
		SET operator_resolved_at = $2, operator_resolved_by = $3, operator_resolution_note = $4
		WHERE id = $1`, id, asOf, "user_x", "explains everything")
	require.NoError(t, err, "the whole triple together must be accepted, or the constraints are simply wrong")
}
