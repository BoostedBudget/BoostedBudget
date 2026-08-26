package operations

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// withheldSpec builds the SAME route table `make generate-openapi` reads
// (operations.RegisterAllRoutesForSpec — a route wired only in cmd/server/main.go produces a
// zero-byte spec diff) and returns the registry plus the withheld listing's operation.
func withheldSpec(t *testing.T) (huma.Registry, *huma.Operation) {
	t.Helper()
	_, api := humatest.New(t, huma.DefaultConfig("BoostedBudget API", "1.0.0"))
	RegisterAllRoutesForSpec(api)
	spec := api.OpenAPI()

	item := spec.Paths["/api/v1/admin/retention/withheld"]
	require.NotNil(t, item,
		"the withheld listing is missing from RegisterAllRoutesForSpec — `make generate-openapi` reads that helper, NOT main.go, so a route wired only in main.go ships an unspecified endpoint")
	require.NotNil(t, item.Get)
	return spec.Components.Schemas, item.Get
}

// validateWithheldBody round-trips a real handler response through JSON and validates it against
// the GENERATED schema for its own body — the artefact a client's code generator consumes.
func validateWithheldBody(t *testing.T, registry huma.Registry, body any) []error {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	var decoded any
	require.NoError(t, json.Unmarshal(raw, &decoded))

	schema := registry.Map()["ListAdminRetentionWithheldOutputBody"]
	require.NotNil(t, schema, "the response body schema must be registered, or there is nothing for a client to generate from")

	res := &huma.ValidateResult{}
	huma.Validate(registry, schema, huma.NewPathBuffer([]byte(""), 0), huma.ModeReadFromServer, decoded, res)
	out := make([]error, 0, len(res.Errors))
	out = append(out, res.Errors...)
	return out
}

// TestListAdminRetentionWithheld_LastPageSatisfiesItsOwnGeneratedContract is the finding that can
// actually break a client. The handler returns no cursor on the final page of the keyset walk; if
// the generated schema marks next_cursor required and non-null, then the LAST page of every walk —
// the one page every walk necessarily ends on — is a response the published contract declares
// invalid. A strict client rejects it; a generated TypeScript client types the field non-optional
// and dereferences it.
//
// Validated against the generated schema rather than against the Go struct on purpose: the Go
// struct was never the thing in disagreement.
func TestListAdminRetentionWithheld_LastPageSatisfiesItsOwnGeneratedContract(t *testing.T) {
	registry, _ := withheldSpec(t)
	expired := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// A SHORT page — the terminating page of any keyset walk.
	shortPage := &stubRetentionWithheldReader{
		counts: repository.RetentionWithholdingCounts{WithheldNoticeMissing: 1},
		page: []repository.RetentionWithheldAccount{
			{UserID: "u1", SubscriptionStatus: "cancelled", RetentionExpiresAt: expired, HasEmailOnFile: true},
		},
	}
	out, err := (&AdminHandler{}).WithRetentionWithheldReader(shortPage).
		ListAdminRetentionWithheld(t.Context(), &ListAdminRetentionWithheldInput{Limit: 50})
	require.NoError(t, err)
	require.Nil(t, out.Body.NextCursor, "a short page is the end of the walk — the fixture is wrong if a cursor was produced")

	assert.Empty(t, validateWithheldBody(t, registry, out.Body),
		"the LAST page of every keyset walk must validate against the endpoint's own published schema — if next_cursor is required and non-null there, the contract rejects a response the server always eventually sends")

	// An EMPTY page — a walk over an empty backlog, which is the normal state.
	emptyPage := &stubRetentionWithheldReader{}
	out, err = (&AdminHandler{}).WithRetentionWithheldReader(emptyPage).
		ListAdminRetentionWithheld(t.Context(), &ListAdminRetentionWithheldInput{Limit: 50})
	require.NoError(t, err)
	assert.Empty(t, validateWithheldBody(t, registry, out.Body),
		"an empty backlog is the healthy steady state and must not be an off-contract response")

	// A FULL page still carries a cursor, so the fix must not be "drop the field".
	fullPage := &stubRetentionWithheldReader{
		page: []repository.RetentionWithheldAccount{{UserID: "u1", RetentionExpiresAt: expired}},
	}
	out, err = (&AdminHandler{}).WithRetentionWithheldReader(fullPage).
		ListAdminRetentionWithheld(t.Context(), &ListAdminRetentionWithheldInput{Limit: 1})
	require.NoError(t, err)
	require.NotNil(t, out.Body.NextCursor)
	assert.Empty(t, validateWithheldBody(t, registry, out.Body),
		"a continuable page must validate too — making next_cursor optional must not make it unpublishable")
}

// TestListAdminRetentionWithheld_LastPageOmitsTheCursorFromTheWire pins the serialisation itself,
// not just schema conformance. Keeping this assertion is the point: a client distinguishes "walk
// finished" from "walk continues" by this key alone, so it must be verifiably absent — never a
// zero-valued cursor object, which would restart the walk at an arbitrary position forever.
func TestListAdminRetentionWithheld_LastPageOmitsTheCursorFromTheWire(t *testing.T) {
	expired := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	shortPage := &stubRetentionWithheldReader{
		page: []repository.RetentionWithheldAccount{{UserID: "u1", RetentionExpiresAt: expired}},
	}
	out, err := (&AdminHandler{}).WithRetentionWithheldReader(shortPage).
		ListAdminRetentionWithheld(t.Context(), &ListAdminRetentionWithheldInput{Limit: 50})
	require.NoError(t, err)

	raw, err := json.Marshal(out.Body)
	require.NoError(t, err)
	var wire map[string]any
	require.NoError(t, json.Unmarshal(raw, &wire))

	_, present := wire["next_cursor"]
	assert.False(t, present,
		"the terminating page must not carry next_cursor on the wire at all — an emitted null contradicts the $ref schema, and an emitted zero cursor would make the walk non-terminating")

	fullPage := &stubRetentionWithheldReader{
		page: []repository.RetentionWithheldAccount{{UserID: "u1", RetentionExpiresAt: expired}},
	}
	out, err = (&AdminHandler{}).WithRetentionWithheldReader(fullPage).
		ListAdminRetentionWithheld(t.Context(), &ListAdminRetentionWithheldInput{Limit: 1})
	require.NoError(t, err)
	raw, err = json.Marshal(out.Body)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &wire))
	cursor, present := wire["next_cursor"]
	require.True(t, present, "a full page must still publish a cursor, or the walk cannot be continued")
	assert.NotNil(t, cursor)
}

// TestListAdminRetentionWithheld_DescriptionNamesTheWholePopulation. The published description is
// the only thing an operator reads before acting, and the population this endpoint selects is
// `subscription_status IN ('inactive', 'cancelled')` — expiredRetentionClockPredicate, spliced into
// retentionWithheldPopulationPredicate, which BOTH the count and the page share. A description
// naming only cancelled accounts invites an operator to misclassify an inactive one and reach for
// the wrong remedy.
func TestListAdminRetentionWithheld_DescriptionNamesTheWholePopulation(t *testing.T) {
	_, op := withheldSpec(t)
	desc := strings.ToLower(op.Description)

	assert.Contains(t, desc, "inactive",
		"the query selects inactive accounts as well as cancelled ones (expiredRetentionClockPredicate); a description that names only one half of the population is a misclassification waiting to happen")
	assert.Contains(t, desc, "cancelled")
}

// TestListAdminRetentionWithheld_DescriptionScopesTheNonDeletabilityClaim. "Terminal" and "can
// never be deleted" are globally FALSE, and falsely reassuring in the one direction that matters:
// user-requested GDPR deletion is a SEPARATE authorisation
// (userRequestedDeletionClaimPredicate requires deletion_requested_at IS NOT NULL, while this
// population requires it to be NULL) and it still deletes these accounts. Only the RETENTION SWEEP
// is blocked. Proved end-to-end by
// TestRetentionWithheldEnumeration_Integration_UserRequestedDeletionStillDeletesAWithheldAccount.
func TestListAdminRetentionWithheld_DescriptionScopesTheNonDeletabilityClaim(t *testing.T) {
	_, op := withheldSpec(t)
	desc := strings.ToLower(op.Description)

	assert.NotContains(t, desc, "terminal state",
		"an unqualified 'terminal state' reads as 'no deletion path exists', which would convince an operator that a GDPR erasure request cannot be honoured for these users — it can, and it must be")
	assert.NotContains(t, desc, "never be deleted")
	assert.Contains(t, desc, "retention sweep",
		"the refusal must be attributed to the retention sweep specifically, because that is the only path it blocks")
	assert.Contains(t, desc, "gdpr",
		"the description must say outright that a user-requested erasure still deletes these accounts — the reassurance an operator most needs and the claim the original wording denied")
}

// interleavingReader models the window between the handler's two repository calls: the count sees
// the population before a change lands, the page sees it after. This is not a contrived state —
// stamping a notice marker, a user requesting deletion, or a pending account's clock crossing NOW()
// each move a row across the predicate boundary, and the two reads are separate statements with no
// enclosing transaction.
type interleavingReader struct {
	counts repository.RetentionWithholdingCounts
	page   []repository.RetentionWithheldAccount
}

func (r *interleavingReader) CountRetentionDeletionsWithheld(context.Context) (repository.RetentionWithholdingCounts, error) {
	return r.counts, nil
}

func (r *interleavingReader) GetRetentionDeletionsWithheldPage(context.Context, *repository.RetentionSweepCursor, int) ([]repository.RetentionWithheldAccount, error) {
	// The population change lands HERE, between the two reads.
	return r.page, nil
}

// TestListAdminRetentionWithheld_CountAndPageAreEventuallyConsistent pins the invariant the code
// actually enforces, against the stronger one an earlier comment claimed. Sharing
// retentionWithheldPopulationPredicate makes the two reads agree about the DEFINITION of the
// population; it does not make them observe the same INSTANT, and the handler does not wrap them in
// a transaction. So a skew is reachable, and this test asserts the handler reports BOTH numbers
// faithfully rather than reconciling them into one it did not measure.
//
// This is a real behavioural axis, not a restatement of the comment: a handler "fixed" by clamping
// withheld_total to len(accounts), or by suppressing the total when the page is empty, would report
// zero backlog while a backlog exists — precisely the unknown-is-not-zero failure this surface was
// built to avoid — and would redden here.
func TestListAdminRetentionWithheld_CountAndPageAreEventuallyConsistent(t *testing.T) {
	// Skew A: the row left the population after being counted (its notice marker was stamped, or
	// its owner requested deletion). Total 1, page empty.
	drained := &interleavingReader{counts: repository.RetentionWithholdingCounts{WithheldNoticeMissing: 1}}
	out, err := (&AdminHandler{}).WithRetentionWithheldReader(drained).
		ListAdminRetentionWithheld(t.Context(), &ListAdminRetentionWithheldInput{Limit: 50})
	require.NoError(t, err, "a skew between the two reads is an ordinary race, not a server error — failing the request would make the surface unusable under load")
	assert.Equal(t, 1, out.Body.WithheldTotal,
		"the total must be reported as measured; clamping it to the page length would report an empty backlog whenever a row drains mid-request")
	assert.Empty(t, out.Body.Accounts)

	// Skew B: the row entered the population after the count (a pending clock crossed NOW()).
	// Page longer than total — the reverse, and equally reachable.
	arrived := &interleavingReader{
		counts: repository.RetentionWithholdingCounts{WithheldNoticeMissing: 0},
		page: []repository.RetentionWithheldAccount{
			{UserID: "u1", RetentionExpiresAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		},
	}
	out, err = (&AdminHandler{}).WithRetentionWithheldReader(arrived).
		ListAdminRetentionWithheld(t.Context(), &ListAdminRetentionWithheldInput{Limit: 50})
	require.NoError(t, err)
	assert.Equal(t, 0, out.Body.WithheldTotal)
	require.Len(t, out.Body.Accounts, 1,
		"a row that entered the population after the count must still be listed — dropping it to satisfy the total would hide a genuinely stuck account")
	assert.NotEmpty(t, out.Body.AsOf,
		"as_of is what makes the skew interpretable: it is the instant the two reads bracket")
}
