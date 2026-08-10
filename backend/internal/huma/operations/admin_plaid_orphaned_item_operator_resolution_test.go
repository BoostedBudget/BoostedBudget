package operations

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// Issue #1497 item 2 — the operator-attestation WRITE endpoint.
//
// ⚠ WHAT THESE TESTS ARE ACTUALLY GUARDING. The endpoint's whole job is to record an unverified human
// claim WITHOUT letting it masquerade as a proved removal, so the assertions are about (a) the
// attester being taken from the authenticated admin identity rather than from the request body, (b)
// both no-op paths being REPORTED to the operator instead of answered with a cheerful 200 that
// discards their note, and (c) the handler calling the attestation write and nothing else — never
// MarkResolved, whose stub in this package panics for exactly that reason.

func attestInput(id, note string) *RecordPlaidOrphanedItemOperatorResolutionInput {
	in := &RecordPlaidOrphanedItemOperatorResolutionInput{ID: id}
	in.ClerkUserID = "user_2attestingAdmin"
	in.Body.Note = note
	return in
}

// The happy path: the claim is recorded, attributed to the AUTHENTICATED admin, and the response says
// what was written.
func TestRecordPlaidOrphanedItemOperatorResolution_RecordsAnAttributedClaim(t *testing.T) {
	repo := &stubOrphanRepo{}
	h := &AdminHandler{plaidOrphanedItemRepo: repo}

	out, err := h.RecordPlaidOrphanedItemOperatorResolution(context.Background(),
		attestInput("11111111-1111-1111-1111-111111111111", "Removed the Item by hand in Plaid's dashboard."))
	require.NoError(t, err)

	require.Equal(t, 1, repo.attestCalls, "the attestation must actually be written")
	assert.Equal(t, "11111111-1111-1111-1111-111111111111", repo.attestID)
	assert.Equal(t, "Removed the Item by hand in Plaid's dashboard.", repo.attestNote)

	// ⚠ THE ATTESTER COMES FROM THE JWT, NOT FROM THE BODY. A client-supplied attester would let one
	// operator record a claim in another's name, which destroys the only mitigation an unverified
	// attestation has: that somebody real is answerable for it.
	assert.Equal(t, "user_2attestingAdmin", repo.attestOperator)

	assert.Equal(t, "user_2attestingAdmin", out.Body.OperatorResolvedBy)
	assert.NotEmpty(t, out.Body.OperatorResolvedAt)
	assert.WithinDuration(t, time.Now().UTC(), repo.attestAsOf, time.Minute)
}

// ⚠ THE SIBLING THAT MAKES THE "does not resolve" ASSERTIONS MEAN SOMETHING. The handler must not go
// anywhere near the proved-removal path: stubOrphanRepo.MarkResolved panics, so a handler that reached
// for it would blow up here rather than quietly promoting a claim to a proof.
func TestRecordPlaidOrphanedItemOperatorResolution_NeverTouchesTheProvedResolutionPath(t *testing.T) {
	repo := &stubOrphanRepo{}
	h := &AdminHandler{plaidOrphanedItemRepo: repo}

	assert.NotPanics(t, func() {
		_, err := h.RecordPlaidOrphanedItemOperatorResolution(context.Background(),
			attestInput("11111111-1111-1111-1111-111111111111", "handled"))
		require.NoError(t, err)
	}, "the attestation endpoint must not call MarkResolved, PurgeExpiredTokens or RecordRetryFailure")
	assert.Equal(t, 1, repo.attestCalls)
}

// An already-attested record answers 409 rather than 200, because the operator's note was NOT recorded
// and telling them otherwise is how somebody comes to believe a record exists that does not. Y1 fixed
// this exact silence on the dead-letter surface; it is not being reintroduced here.
func TestRecordPlaidOrphanedItemOperatorResolution_AlreadyAttestedIsAConflictNotASilentSuccess(t *testing.T) {
	repo := &stubOrphanRepo{attestErr: repository.ErrOrphanedItemAlreadyAttested}
	h := &AdminHandler{plaidOrphanedItemRepo: repo}

	_, err := h.RecordPlaidOrphanedItemOperatorResolution(context.Background(),
		attestInput("11111111-1111-1111-1111-111111111111", "I did this too"))

	require.Error(t, err)
	assertHTTPStatus(t, err, 409) // an existing attestation is first-wins and the second writer must be told
	assert.Contains(t, err.Error(), "attestation")
}

func TestRecordPlaidOrphanedItemOperatorResolution_UnknownRecordIs404(t *testing.T) {
	repo := &stubOrphanRepo{attestErr: repository.ErrOrphanedItemNotFound}
	h := &AdminHandler{plaidOrphanedItemRepo: repo}

	_, err := h.RecordPlaidOrphanedItemOperatorResolution(context.Background(),
		attestInput("11111111-1111-1111-1111-111111111111", "handled"))

	require.Error(t, err)
	assertHTTPStatus(t, err, 404)
}

// A whitespace-only note is refused. `minLength` on the schema counts characters, so "   " satisfies
// it — and an unexplained claim is nearly useless to whoever has to check it against Plaid later,
// which is the only reason an unverified attestation is acceptable at all.
func TestRecordPlaidOrphanedItemOperatorResolution_RefusesABlankNote(t *testing.T) {
	repo := &stubOrphanRepo{}
	h := &AdminHandler{plaidOrphanedItemRepo: repo}

	_, err := h.RecordPlaidOrphanedItemOperatorResolution(context.Background(),
		attestInput("11111111-1111-1111-1111-111111111111", "   \t \n "))

	require.Error(t, err)
	assertHTTPStatus(t, err, 422)
	assert.Equal(t, 0, repo.attestCalls, "and nothing must be written")
}

// The note is stored TRIMMED, so a note that is only leading whitespace plus one word does not read as
// an empty cell on the admin surface.
func TestRecordPlaidOrphanedItemOperatorResolution_TrimsTheNote(t *testing.T) {
	repo := &stubOrphanRepo{}
	h := &AdminHandler{plaidOrphanedItemRepo: repo}

	_, err := h.RecordPlaidOrphanedItemOperatorResolution(context.Background(),
		attestInput("11111111-1111-1111-1111-111111111111", "  Removed at Plaid.\n"))
	require.NoError(t, err)
	assert.Equal(t, "Removed at Plaid.", repo.attestNote)
}

// An unwired repository is 503, matching the listing's own behaviour rather than panicking.
func TestRecordPlaidOrphanedItemOperatorResolution_UnwiredRepoIs503(t *testing.T) {
	h := &AdminHandler{}
	_, err := h.RecordPlaidOrphanedItemOperatorResolution(context.Background(),
		attestInput("11111111-1111-1111-1111-111111111111", "handled"))
	require.Error(t, err)
	assertHTTPStatus(t, err, 503)
}

// An attestation with no authenticated Clerk identity is refused before it reaches the database. The
// resolver normally guarantees ClerkUserID is present; this is the belt-and-braces assertion that an
// anonymous attestation cannot be written even if that ever stops being true, because the CHECK
// constraint in migration 000137 would otherwise surface as an opaque 500.
func TestRecordPlaidOrphanedItemOperatorResolution_RefusesAnAnonymousAttestation(t *testing.T) {
	repo := &stubOrphanRepo{}
	h := &AdminHandler{plaidOrphanedItemRepo: repo}

	in := &RecordPlaidOrphanedItemOperatorResolutionInput{ID: "11111111-1111-1111-1111-111111111111"}
	in.AdminAuthenticatedInput = inputs.AdminAuthenticatedInput{} // no ClerkUserID
	in.Body.Note = "handled"

	_, err := h.RecordPlaidOrphanedItemOperatorResolution(context.Background(), in)
	require.Error(t, err)
	assert.Equal(t, 0, repo.attestCalls)
	assert.True(t, strings.Contains(err.Error(), "operator") || strings.Contains(err.Error(), "identity"),
		"the message must say what is missing, got %q", err.Error())
}
