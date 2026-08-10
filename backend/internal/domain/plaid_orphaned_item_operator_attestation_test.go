package domain

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Issue #1497 item 2 — the OPERATOR-ATTESTED resolution, and specifically the half of it that lives
// in the state derivation rather than in the schema.
//
// ⚠ WHY THIS FILE EXISTS AT ALL. Adding a second resolution concept to this table does not merely add
// a column; it CREATES an ambiguity in an existing reader. AdminState checks ResolvedAt and then token
// liveness, and an operator-attested row has resolved_at still NULL — so without the third state below
// an attested row would keep reporting manual_intervention_required, i.e. the admin surface would keep
// demanding the exact work the operator just said they had done. Every assertion here is about that.

// attested is the fixture shape this file adds to newOrphan(): a row whose credential has expired (so
// the pre-existing derivation would say manual_intervention_required) and which an operator has
// claimed to have handled in Plaid's dashboard.
func attestedOrphan() PlaidOrphanedItem {
	o := newOrphan()
	// Dead credential: without the attestation this row is unambiguously manual_intervention_required,
	// which is what makes the assertions below about the attestation and not about anything else.
	o.AccessToken = ""
	purged := orphanStateAsOf.Add(-2 * time.Hour)
	o.TokenPurgedAt = &purged

	attested := orphanStateAsOf.Add(-1 * time.Hour)
	o.OperatorResolvedAt = &attested
	o.OperatorResolvedBy = "user_2adminClerkID"
	o.OperatorResolutionNote = "Removed the Item by hand in Plaid's dashboard."
	return o
}

// RED 3 — the attestation is visible WHERE THE STATE IS COMPUTED, not only where it is rendered.
func TestPlaidOrphanedItem_AdminState_OperatorAttestationIsItsOwnState(t *testing.T) {
	o := attestedOrphan()

	// The control: strip the attestation and the same row is the state the operator was reacting to.
	control := o
	control.OperatorResolvedAt = nil
	control.OperatorResolvedBy = ""
	require.Equal(t, OrphanedItemManualInterventionRequired, control.AdminState(orphanStateAsOf),
		"without the attestation this fixture must be manual_intervention_required, or this test proves nothing")

	assert.Equal(t, OrphanedItemOperatorAttested, o.AdminState(orphanStateAsOf),
		"an operator who says they handled the Item must not keep being told to handle the Item")
}

// ⚠ AND IT IS NOT 'resolved'. The whole point of a separate column is that a claim is not a proof:
// resolved means an automated /item/remove PROVED the Item gone at Plaid, and an unverified human
// assertion must never be laundered into that. This is the `'replaced'`-treated-as-proof shape.
func TestPlaidOrphanedItem_AdminState_OperatorAttestationIsNotProvedResolution(t *testing.T) {
	o := attestedOrphan()
	assert.NotEqual(t, OrphanedItemResolved, o.AdminState(orphanStateAsOf),
		"an unverified operator claim must be distinguishable from a proved removal, forever")
	assert.Nil(t, o.ResolvedAt, "and the attestation must not have invented a proof")
}

// An attestation outranks the machine-derived nagging even when the credential is still live and the
// sweep is still working: the operator's claim is the newer information about the Item at Plaid, and
// the row must say which kind of statement it is carrying.
func TestPlaidOrphanedItem_AdminState_OperatorAttestationOutranksAutoRetrying(t *testing.T) {
	o := newOrphan()
	require.Equal(t, OrphanedItemAutoRetrying, o.AdminState(orphanStateAsOf))

	attested := orphanStateAsOf.Add(-5 * time.Minute)
	o.OperatorResolvedAt = &attested
	o.OperatorResolvedBy = "user_2adminClerkID"

	assert.Equal(t, OrphanedItemOperatorAttested, o.AdminState(orphanStateAsOf))
}

// RED 4 — COEXISTENCE, PINNED. A row both attested AND later proved-removed must resolve as PROVED.
//
// ⚠ THIS USED TO WORK BY ACCIDENT and this test is why it no longer does: it holds only because
// AdminState checks ResolvedAt before the attestation, i.e. proof beats claim. That check order was
// not written with attestation in mind, so nothing but this test stops a future reordering (say,
// "surface the human's statement first, it is more recent") from making a proved removal report as an
// unverified claim — a strictly weaker fact than the one on the row.
func TestPlaidOrphanedItem_AdminState_ProvedRemovalBeatsAnOperatorAttestation(t *testing.T) {
	o := attestedOrphan()
	resolved := orphanStateAsOf.Add(-30 * time.Minute)
	o.ResolvedAt = &resolved

	assert.Equal(t, OrphanedItemResolved, o.AdminState(orphanStateAsOf),
		"proof beats claim: a row Plaid confirmed removed must not be downgraded to a human assertion")
}

// A merely-attested row MUST REMAIN AUDITABLE — the view carries who said it, when, and what they
// said, so the claim can be checked later against Plaid rather than being an anonymous green tick.
func TestPlaidOrphanedItemAdminView_CarriesTheAttestationForAudit(t *testing.T) {
	o := attestedOrphan()
	view := o.AdminView(orphanStateAsOf)

	require.NotNil(t, view.OperatorResolvedAt)
	assert.Equal(t, *o.OperatorResolvedAt, *view.OperatorResolvedAt)
	assert.Equal(t, "user_2adminClerkID", view.OperatorResolvedBy,
		"an attestation nobody is named for is not an attestation")
	assert.Equal(t, "Removed the Item by hand in Plaid's dashboard.", view.OperatorResolutionNote)
	assert.Equal(t, OrphanedItemOperatorAttested, view.AdminState)

	encoded, err := json.Marshal(view)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"admin_state":"operator_attested"`)
	assert.Contains(t, string(encoded), `"operator_resolved_by":"user_2adminClerkID"`)
}

// The attestation must not have touched the credential fields the compliance argument reads. This is
// the domain-level half of RED 1; the SQL-level half is in the repository integration suite, where the
// actual UPDATE can be observed not to clear the ciphertext.
func TestPlaidOrphanedItemAdminView_AttestationDoesNotClaimTheCredentialIsGone(t *testing.T) {
	o := newOrphan() // live, retained credential
	attested := orphanStateAsOf.Add(-time.Minute)
	o.OperatorResolvedAt = &attested
	o.OperatorResolvedBy = "user_2adminClerkID"

	view := o.AdminView(orphanStateAsOf)
	assert.True(t, view.TokenRetained,
		"an operator's claim about Plaid says nothing about what this database still holds")
	assert.Nil(t, view.TokenPurgedAt)
	assert.Equal(t, OrphanedItemOperatorAttested, view.AdminState)
}

// ⚠ THE NEW STATE MUST REACH THE AGGREGATE READERS. SummarisePlaidOrphanedItems' switch is exhaustive
// by hand, not by the compiler, so a new state silently falls into no bucket at all — /admin/health
// and the Prometheus gauges would then report an attested row as nothing whatsoever, and the three
// counts would stop summing to the population.
func TestSummarisePlaidOrphanedItems_CountsOperatorAttestedSeparately(t *testing.T) {
	items := []PlaidOrphanedItem{attestedOrphan(), attestedOrphan(), newOrphan()}

	m := SummarisePlaidOrphanedItems(items, orphanStateAsOf)

	assert.Equal(t, int64(2), m.OperatorAttested)
	assert.Equal(t, int64(1), m.AutoRetrying)
	assert.Equal(t, int64(0), m.ManualInterventionRequired,
		"an attested row must not also be counted as work a human still has to do")
	assert.Equal(t, int64(0), m.Resolved,
		"and it must never be counted as a proved removal")
	assert.Equal(t, int64(len(items)),
		m.AutoRetrying+m.ManualInterventionRequired+m.Resolved+m.OperatorAttested,
		"every row must land in exactly one bucket — a state nobody counted is a state nobody sees")
}

// An attested row is still UNRESOLVED, so its attempt history still drives the re-attempt gauge. That
// is deliberate and is the same decision as GetRetryablePage's (see its comment): the sweep keeps
// trying, so the count keeps moving, and a row still failing 20-odd times after somebody attested it
// is precisely the case an operator should be told about — either the claim was wrong or the Item
// really is still there.
func TestSummarisePlaidOrphanedItems_AttestedRowsStillDriveTheReattemptGauge(t *testing.T) {
	o := attestedOrphan()
	o.RetryAttemptCount = 27

	m := SummarisePlaidOrphanedItems([]PlaidOrphanedItem{o}, orphanStateAsOf)

	assert.Equal(t, int64(27), m.MaxUnresolvedRetryAttemptCount,
		"an unverified claim must not silence the gauge that would expose the claim as wrong")
}
