package operations

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	humaerrors "github.com/maxnussbaum/BoostedBudget/internal/huma/errors"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	humamiddleware "github.com/maxnussbaum/BoostedBudget/internal/huma/middleware"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// RetentionWithheldReader is the narrow read this surface needs from the profile repository —
// Issue #1584. Deliberately two methods and no writes: the admin surface for a stuck retention
// backlog is a LOOKING surface, because what to do about the backlog (send late against a re-armed
// clock, waive the notice, or notify out of band) is an open product decision and a button here
// would take it by implication.
//
// Narrow rather than repository.ProfileRepository, matching the pattern
// RetentionCleanupCheckWorker already uses for the same two-method dependency: the wide interface
// would drag ~100 unrelated methods into every test double for this handler.
type RetentionWithheldReader interface {
	CountRetentionDeletionsWithheld(ctx context.Context) (repository.RetentionWithholdingCounts, error)
	GetRetentionDeletionsWithheldPage(ctx context.Context, after *repository.RetentionSweepCursor, limit int) ([]repository.RetentionWithheldAccount, error)
}

// WithRetentionWithheldReader sets the retention-withholding reader on the handler (builder
// pattern). Issue #1584.
func (h *AdminHandler) WithRetentionWithheldReader(r RetentionWithheldReader) *AdminHandler {
	h.retentionWithheldReader = r
	return h
}

// ListAdminRetentionWithheldInput is the query for the withheld-retention admin listing.
//
// KEYSET pagination, not offset, unlike the orphaned-Plaid-Item listing beside it — and the
// difference is deliberate rather than inconsistency. That table holds single digits of rows and is
// read as a short work list. This population is a BACKLOG: it grows with every cancelled account
// whose clock runs out while no EmailService is wired (which is every environment today), so it can
// be large, and a reader walking it must not have rows shift under them between pages. The cursor
// is the same (retention_expires_at, id) pair the repository orders by, echoed back as next_cursor.
type ListAdminRetentionWithheldInput struct {
	inputs.AdminAuthenticatedInput
	Limit int `query:"limit" default:"50" minimum:"1" maximum:"500" doc:"Number of accounts to return"`
	// CursorExpiresAt and CursorUserID must be supplied TOGETHER — a keyset position is the pair,
	// and half of one silently selects a different page. Enforced in the handler, because huma has
	// no cross-field requirement.
	CursorExpiresAt string `query:"cursor_expires_at" doc:"Keyset cursor: retention_expires_at of the last account on the previous page (RFC3339). Must be sent together with cursor_user_id."`
	CursorUserID    string `query:"cursor_user_id" format:"uuid" doc:"Keyset cursor: user_id of the last account on the previous page. Must be sent together with cursor_expires_at."`
}

// AdminRetentionWithheldAccount is one stuck account as this surface reports it.
//
// ⚠ NO EMAIL ADDRESS, ONLY WHETHER THERE IS ONE. The decision an operator makes here — re-arm,
// waive, or contact out of band — needs to know an address EXISTS; it never needs the address
// itself, and putting a decrypted one in an HTTP response would spread plaintext PII into every
// log and proxy on the path for no gain.
type AdminRetentionWithheldAccount struct {
	UserID             string `json:"user_id"`
	SubscriptionStatus string `json:"subscription_status"`
	RetentionExpiresAt string `json:"retention_expires_at"`
	// DaysWithheld is how long this account has been stuck, computed against the response's single
	// as_of instant. Server-side because it is the severity ordering an operator actually triages
	// by, and a client deriving it from its own clock would disagree with the sort order the server
	// used.
	DaysWithheld int `json:"days_withheld"`
	// Notice30dSentAt / Notice7dSentAt name WHICH promise was not kept; null means never recorded.
	// At least one is null in every row — that is what makes the account withheld.
	Notice30dSentAt *string `json:"notice_30d_sent_at"`
	Notice7dSentAt  *string `json:"notice_7d_sent_at"`
	// HasEmailOnFile is the FOURTH withholding reason and this is the only surface anywhere that
	// can report it. The retention_deletions_withheld metric provably cannot: its reason label would
	// have to come from a SQL predicate, users.email is ciphertext, and email_blind_idx (the one
	// column that could discriminate) carries a UNIQUE index capping that population at one row
	// database-wide. This listing decrypts per row instead, at exactly the point
	// canSendRetentionNotice checks it. False means no notice can be sent to this account even
	// after an EmailService is wired and its clock re-armed — it needs a different remedy entirely.
	HasEmailOnFile bool `json:"has_email_on_file"`
}

// AdminRetentionWithheldCursor is the keyset position to pass back for the next page.
type AdminRetentionWithheldCursor struct {
	ExpiresAt string `json:"cursor_expires_at"`
	UserID    string `json:"cursor_user_id"`
}

// ListAdminRetentionWithheldOutput is the response body.
//
// ⚠ WithheldTotal IS THE WHOLE-POPULATION COUNT, NOT THE PAGE'S. It comes from
// CountRetentionDeletionsWithheld over the SAME spliced predicate the listing selects on
// (repository.retentionWithheldPopulationPredicate), which is why the count is re-read here rather
// than left to the client to infer from len(accounts) — page one of a hundred would otherwise
// report a backlog of `limit`.
//
// ⚠ THE TWO NUMBERS ARE EVENTUALLY CONSISTENT, NOT ONE SNAPSHOT, and an earlier draft of this
// comment claimed the opposite. Sharing a predicate makes the two reads describe the same
// population DEFINITION; it does not make them observe the same INSTANT. They are separate
// statements on a pooled connection with no enclosing transaction, so anything that moves a row
// into or out of the population between them — a notice marker being stamped, a user requesting
// deletion (which makes deletion_requested_at non-NULL and drops the row), the retention clock of
// a pending account crossing NOW() (which adds one) — is visible to one read and not the other.
// The observable results are withheld_total = 1 beside an empty page, or an accounts array longer
// than withheld_total.
//
// That is ACCEPTED rather than closed, deliberately: this is a human triage surface polled by an
// operator, the skew is bounded by one refresh, and the alternative — a REPEATABLE READ
// transaction or a single statement returning both — would put a snapshot-holding transaction on
// an admin read of an unbounded backlog to buy consistency nobody acts on. Read as_of, and read
// both numbers as of that instant ± the window between the two queries. Pinned by
// TestListAdminRetentionWithheld_CountAndPageAreEventuallyConsistent, which interleaves a
// population change between the two reads and asserts the handler reports both faithfully instead
// of reconciling them into a number it did not measure.
type ListAdminRetentionWithheldOutput struct {
	Body struct {
		Accounts []AdminRetentionWithheldAccount `json:"accounts"`
		// WithheldTotal is every account the notice gate is holding indefinitely, across all pages.
		WithheldTotal int `json:"withheld_total"`
		// NoticePending is the HEALTHY population, reported beside the alarming one on purpose: a
		// withheld_total of 40 means something very different when notice_pending is 0 than when it
		// is 4000. These two are disjoint — a pending account's clock is still RUNNING, so the
		// notice sweep can still reach it, and it is not listed in accounts.
		NoticePending int `json:"notice_pending"`
		// NextCursor is ABSENT on the last page — omitempty, not a null, and the difference is the
		// contract rather than a style choice. huma renders a *struct field as a bare `$ref`, which
		// carries no `"null"` in its type union the way a *string field's schema does; marking it
		// required therefore published a schema that REJECTS the terminating page of every keyset
		// walk, and generated a TypeScript model typing the field non-optional for a client to
		// dereference. omitempty drops it from `required`, so absent is the schema-valid encoding of
		// "the walk is over". Pinned by
		// TestListAdminRetentionWithheld_LastPageSatisfiesItsOwnGeneratedContract (validates a real
		// last-page body against the GENERATED schema) and
		// TestListAdminRetentionWithheld_LastPageOmitsTheCursorFromTheWire.
		//
		// Present does not promise the next page is non-empty — it promises only that this page was
		// full, which is all a keyset walk can honestly say without a second query.
		NextCursor *AdminRetentionWithheldCursor `json:"next_cursor,omitempty"`
		Limit      int                           `json:"limit"`
		AsOf       string                        `json:"as_of"`
	}
}

// ListAdminRetentionWithheld enumerates the accounts the Issue #1569 T1 pre-deletion-notice gate is
// holding back from hard deletion — Issue #1584.
//
// GET /api/v1/admin/retention/withheld
//
// THE STATE IT MAKES ADDRESSABLE. T1 stopped the retention sweep hard-deleting an INACTIVE OR
// CANCELLED account that was never sent the 30-day and 7-day notices
// apps/web/src/legal/PrivacyPolicy.tsx promises. Both statuses, not just cancelled: the population
// is expiredRetentionClockPredicate's `subscription_status IN ('inactive', 'cancelled')`, spliced
// into repository.retentionWithheldPopulationPredicate, which the count and this listing share.
// Both notice queries bound their population by retention_expires_at > NOW(), so once such an
// account's clock runs out with a marker still NULL it cannot be deleted by the RETENTION SWEEP,
// or noticed by the current notice queries.
//
// ⚠ THAT IS NOT "CAN NEVER BE DELETED", and the stronger phrasing this comment used to carry was
// false in the one direction that matters most. User-requested GDPR erasure is a SEPARATE
// authorisation: repository.userRequestedDeletionClaimPredicate requires
// `deletion_requested_at IS NOT NULL`, while this population requires it to be NULL, so a withheld
// account that asks to be erased leaves this listing and is deleted by the scheduled-deletion path
// exactly as any other user is — deletion_service.go's ExecuteDeletion distinguishes the two kinds
// explicitly. Proved by
// TestRetentionWithheldEnumeration_Integration_UserRequestedDeletionStillDeletesAWithheldAccount.
// Telling an administrator no deletion path exists would invite them to refuse a request they are
// legally obliged to honour.
//
// The retention_deletions_withheld gauge has COUNTED this population since T1 (which is why
// #1584's "no existing query can SELECT these users at all" is only half true); nothing enumerated
// it, so there was no way to act on any individual account.
//
// ⚠ THIS ENDPOINT DRAINS NOTHING, AND ITS BEING READ-ONLY IS THE DESIGN, not an unfinished edge.
// #1584 lists three mutually exclusive remedies — send late against a re-armed retention_expires_at,
// record a waiver and permit deletion, or notify out of band — and choosing between them is a
// product decision parked until after go-live with beta. A "resolve" button here would take that
// decision by implication and, worse, would be the first path in this codebase that permits a hard
// delete without the promised notice. When the decision is taken, the remedy belongs in its own
// endpoint with its own audit row, exactly as the three purges are shaped.
func (h *AdminHandler) ListAdminRetentionWithheld(ctx context.Context, input *ListAdminRetentionWithheldInput) (*ListAdminRetentionWithheldOutput, error) {
	if h.retentionWithheldReader == nil {
		return nil, huma.Error503ServiceUnavailable("retention withholding reader not available")
	}

	// A keyset position is the PAIR. Half of one is not a smaller request, it is a different and
	// silently wrong page, so it is refused rather than partly honoured.
	if (input.CursorExpiresAt == "") != (input.CursorUserID == "") {
		return nil, huma.Error422UnprocessableEntity(
			"cursor_expires_at and cursor_user_id are one keyset position and must be supplied together — sending only one would select a different page than the walk intended")
	}

	var cursor *repository.RetentionSweepCursor
	if input.CursorExpiresAt != "" {
		expiresAt, err := time.Parse(time.RFC3339, input.CursorExpiresAt)
		if err != nil {
			return nil, huma.Error422UnprocessableEntity(
				"cursor_expires_at must be an RFC3339 timestamp, as returned in next_cursor by the previous page")
		}
		cursor = &repository.RetentionSweepCursor{ExpiresAt: expiresAt, UserID: input.CursorUserID}
	}

	counts, err := h.retentionWithheldReader.CountRetentionDeletionsWithheld(ctx)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, fmt.Errorf("failed to count accounts withheld from retention deletion: %w", err))
	}

	accounts, err := h.retentionWithheldReader.GetRetentionDeletionsWithheldPage(ctx, cursor, input.Limit)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, fmt.Errorf("failed to list accounts withheld from retention deletion: %w", err))
	}

	// ONE instant for the whole response, for the reason #1466 threaded asOf through every liveness
	// derivation in this codebase: days_withheld is a comparison against a clock, and reading
	// time.Now() per row would let two rows in one response disagree about where the boundary is.
	// Echoed back as as_of so a reader can tell which instant the numbers were computed for.
	asOf := time.Now().UTC()

	out := &ListAdminRetentionWithheldOutput{}
	out.Body.Accounts = make([]AdminRetentionWithheldAccount, 0, len(accounts))
	for _, acct := range accounts {
		out.Body.Accounts = append(out.Body.Accounts, AdminRetentionWithheldAccount{
			UserID:             acct.UserID,
			SubscriptionStatus: acct.SubscriptionStatus,
			RetentionExpiresAt: acct.RetentionExpiresAt.UTC().Format(time.RFC3339),
			DaysWithheld:       int(asOf.Sub(acct.RetentionExpiresAt).Hours() / 24),
			Notice30dSentAt:    formatOptionalRFC3339(acct.Notice30dSentAt),
			Notice7dSentAt:     formatOptionalRFC3339(acct.Notice7dSentAt),
			HasEmailOnFile:     acct.HasEmailOnFile,
		})
	}
	out.Body.WithheldTotal = counts.WithheldNoticeMissing
	out.Body.NoticePending = counts.NoticePending
	out.Body.Limit = input.Limit
	out.Body.AsOf = asOf.Format(time.RFC3339)

	// A full page means there MAY be more; a short page means the walk is exhausted. Deliberately
	// not "there IS more", which would need a second query to be true — see the field's own doc.
	if len(accounts) == input.Limit && len(accounts) > 0 {
		last := accounts[len(accounts)-1]
		out.Body.NextCursor = &AdminRetentionWithheldCursor{
			ExpiresAt: last.RetentionExpiresAt.UTC().Format(time.RFC3339),
			UserID:    last.UserID,
		}
	}
	return out, nil
}

// formatOptionalRFC3339 renders a nullable timestamp for the JSON body, preserving nil as null.
// A notice marker's absence is the entire signal on this surface, so it must serialise as null and
// never as a zero time — "0001-01-01T00:00:00Z" reads to a client as a date, not as "never".
func formatOptionalRFC3339(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}

// RegisterAdminRetentionWithheldOperations registers the withheld-retention admin listing.
//
// Called from RegisterAdminRoutes, and named *Operations rather than *Routes for the reason
// RegisterAdminPlaidOrphanedItemOperations gives: cmd/server's route-registration drift test
// matches `Register…Routes(`, and a second `…Routes` name registered from inside RegisterAdminRoutes
// would look to it like a group main.go had failed to call.
func RegisterAdminRetentionWithheldOperations(api huma.API, handler *AdminHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "listAdminRetentionWithheld",
		Method:      http.MethodGet,
		Path:        "/api/v1/admin/retention/withheld",
		Summary:     "List accounts withheld from retention deletion (admin)",
		Description: "Returns inactive or cancelled accounts whose data-retention window has expired " +
			"but which the pre-deletion-notice gate is refusing to hard-delete, because at least one of " +
			"the 30-day and 7-day notices promised by the privacy policy was never recorded. What is " +
			"blocked is the retention sweep, and only it: the notice queries select only users whose " +
			"retention clock has not yet run out, so no existing path can send the missing notice and " +
			"the retention sweep will hold these accounts indefinitely. A user-requested GDPR erasure is " +
			"a separate authorisation and still deletes them normally; requesting deletion also removes " +
			"the account from this listing. This is a read-only view — it sends nothing, extends no " +
			"retention clock and waives no notice; choosing between those remedies is an open decision " +
			"and will land as its own endpoint. has_email_on_file is reported per account because it is " +
			"the one withholding reason the retention_deletions_withheld metric cannot separate. No " +
			"email address is returned. withheld_total and the returned page come from two separate " +
			"queries and are eventually consistent: a notice marker, a deletion request or a retention " +
			"clock crossing the current instant between them can make the total disagree with the page " +
			"by a row. Keyset-paginated: pass next_cursor's two fields back as cursor_expires_at and " +
			"cursor_user_id; next_cursor is absent on the final page. Requires admin role.",
		Tags:     []string{"Admin"},
		Security: []map[string][]string{{"clerk": {}}},
		Middlewares: huma.Middlewares{
			humamiddleware.NewAdminMiddleware(handler.config, handler.adminService),
		},
	}, handler.ListAdminRetentionWithheld)
}
