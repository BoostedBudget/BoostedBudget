package repository

import (
	"context"
	"errors"
	"fmt"
	"net/netip"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// AuditLogRepository writes GDPR audit-trail entries to audit_logs. It is the REAL implementation
// of services.AuditLogger, which it satisfies structurally, and it replaces cmd/server/main.go's
// mockAuditLogger — the only production implementation there has ever been, which wrote an
// slog line and persisted NOTHING. Every `auditLogger.Log(...)` in the GDPR deletion path therefore
// produced no durable record at all, while audit_logs existed, was queried elsewhere, and even had
// erasure-redaction logic written against it. Issue #1505.
//
// WHICH COLUMN GETS WHAT, and why it is not a matter of taste:
//
//	audit_logs.user_id     -> the ACTING party    (nullable UUID, FK to users ON DELETE SET NULL)
//	audit_logs.resource_id -> the SUBJECT/TARGET  (bare UUID, NO foreign key at all)
//
// This is the table's own documented convention — migration 000003 ("User who performed the
// action" / "ID of the resource affected"), admin_user_repository.go's writers ("audit_logs.user_id
// -> the ACTING ADMIN; resource_id -> the TARGET"), DeleteUserCascade's redaction, and
// RecordDeletionResidue all agree. An earlier ticket on this branch proposed a redaction keyed to
// the wrong one; getting it backwards here would be worse than that, because:
//
//   - resource_id has NO FK, so a row keyed to an erased subject is still writable and still there.
//   - user_id DOES have one. The `user.deleted` entry is written AFTER the subject's users row has
//     been deleted — since Issue #1507 that is DeleteUserCascade's own final statement, inside the
//     same transaction as the `DELETE FROM users` it records; before #1507 it was ExecuteDeletion's
//     separate post-commit statement. Either way the users row is already gone by then, so putting
//     the subject in user_id would make that INSERT a foreign-key violation EVERY time, in
//     production, for the single most important entry in the trail. Pinned by
//     TestAuditLogRepository_Integration_RowOutlivesAndIsWritableAfterTheSubjectsHardDeletion and,
//     at the in-transaction write site, by
//     TestDeleteUserCascade_Integration_AuditEntrySurvivesItsSubjectsHardDeletion.
//
// THE INTERFACE CARRIES NO ACTOR, and user_id is therefore written as SQL NULL rather than filled
// in with the subject. Log(ctx, action, resourceType, resourceID) has no actor parameter, and none
// of its production call sites has one to give: RequestDeletion and CancelDeletion receive only the
// subject's id, RegisterUser runs under a sign-up flow with no separate acting principal, and
// DeleteUserCascade's `user.deleted` entry (via LogInTx, since Issue #1507) is written by a
// background worker acting on nobody's behalf — a retention sweep nobody asked for. NULL is the
// honest reading and matches the column's own documented meaning ("NULL if system action or user
// deleted") and
// RecordDeletionResidue's precedent.
//
// Writing the subject into user_id would manufacture the claim
// "this person performed this action", which for a retention-triggered deletion — nobody asked; the
// clock expired — is simply false. A false record is worse than an absent one. Extending the
// interface with an explicit actor is tracked separately (see the ticket referenced in
// services.AuditLogger's doc comment); today no caller could supply a non-nil value for it.
//
// ⚠⚠ THE NULL ACTOR IS NOW A LOCKING PRECONDITION, NOT ONLY AN HONESTY ONE — ISSUE #1506 MUST READ
// THIS BEFORE ADDING AN ACTOR. Since Issue #1507, LogInTx runs this INSERT INSIDE
// DeleteUserCascade's transaction, which holds `FOR UPDATE` on the subject's users row. With
// user_id NULL, MATCH SIMPLE satisfies fk_audit_logs_user_id trivially: the constraint trigger
// short-circuits on the NULL key before it reads or locks anything in users (migration 000003
// declares no MATCH FULL, so MATCH SIMPLE is the default), so the INSERT requests NO `FOR KEY
// SHARE` on any users row and cannot participate in a lock cycle. The
// moment a non-NULL actor is written here, that FK check DOES fire, and an INSERT naming user A
// while a cascade of user B holds A's row `FOR UPDATE` (and vice versa) is the cross-user deadlock
// class — SQLSTATE 40P01 — this codebase has already reproduced against a live database (see
// StampMissingPlaidCleanupMarkers's ROUND 23 correction in profile_repository.go, and
// DeleteUserCascade's redaction comment). #1506 must therefore either keep the actor NULL on the
// in-transaction path specifically, or establish a total order over users-row locks first. See
// LogInTx's own warning, which is the one a future implementer trips over at the call site.
//
// NOTHING IS WRITTEN TO `changes`. DeleteUserCascade redacts with
// `UPDATE audit_logs SET changes = NULL WHERE resource_id = $1 OR user_id = $1`, so any payload
// here would be nulled out for exactly the users whose deletions this trail is meant to evidence.
// The load-bearing facts live in `action` and `resource_id`, which redaction does not touch — the
// same reasoning, and the same choice, as RecordDeletionResidue.
type AuditLogRepository interface {
	// Log durably records one audit entry. It returns an error when the entry was NOT recorded;
	// it never reports a refused write as a success. Whether a caller treats that error as fatal
	// is the caller's decision — see services.AuditLogger.
	Log(ctx context.Context, action, resourceType, resourceID string) error

	// LogInTx records the same entry inside a caller-supplied transaction, so the entry commits or
	// rolls back WITH the state change it describes. Issue #1507. Mirrors
	// MCPAuditRepository.RecordInTx, which is the same pattern for the same reason.
	//
	// ⚠⚠ THIS IS SAFE ONLY WHILE THE ACTOR (audit_logs.user_id) IS WRITTEN AS NULL. READ THIS
	// BEFORE IMPLEMENTING ISSUE #1506.
	//
	// Its original production caller is DeleteUserCascade, which holds `FOR UPDATE` on the
	// subject's users row for the whole transaction. Issue #1569, Task 6 added two more callers,
	// AccountRepository.PurgeAccount and LinkedItemRepository.PurgeLinkedItem — neither takes a
	// lock on any users row (their FOR UPDATE is on accounts/linked_items respectively), so the
	// safety argument below is unaffected; it is restated per-caller in each of their own doc
	// comments rather than re-derived here. Because user_id is NULL, fk_audit_logs_user_id is
	// satisfied under MATCH SIMPLE and its constraint trigger short-circuits before it reads or
	// locks anything in users — this INSERT therefore requests NO `FOR KEY SHARE` on any users row,
	// and no lock cycle involving users is even expressible. That, and nothing else, is why this
	// write can live inside any of these transactions.
	//
	// #1506 proposes giving AuditLogger an explicit actor. The instant a non-NULL user_id reaches
	// THIS method, the FK check fires and takes `FOR KEY SHARE` on that actor's users row — while a
	// concurrent cascade may hold it `FOR UPDATE` and be about to want the row this one holds. That
	// is the cross-user 40P01 deadlock the deferral of #1507 was originally argued from, and it was
	// reproduced against a live database, not theorised. It is why #1507 shipped BEFORE #1506, and
	// it is a sequencing constraint on #1506, not on this method.
	//
	// If you are adding an actor: either keep the in-transaction path's actor NULL, or establish a
	// total order over users-row locks across every writer first. Do not simply widen the
	// signature. The lock-order invariant this must not perturb is stated at DeleteUserCascade's
	// household lock and pinned by
	// TestDeleteUserCascade_Integration_LockOrderMatchesAcceptInvite_NoDeadlockWithConcurrentAcceptInvite.
	LogInTx(ctx context.Context, tx pgx.Tx, action, resourceType, resourceID string) error
}

// AuditLogMetadata is the request metadata an audit row may record alongside the load-bearing
// action/resource facts. Zero values mean "not recorded": an invalid netip.Addr and uuid.Nil are
// bound as SQL NULL, and a nil Changes slice is bound as SQL NULL. This is deliberate — an absent
// value must stay absent rather than become a zero IP or an all-zero UUID that reads as data.
type AuditLogMetadata struct {
	IP        netip.Addr
	UserAgent string
	RequestID uuid.UUID
	Changes   []byte
}

// AuditLogMetadataRepository is the AuditLogRepository surface PLUS LogInTxWithMetadata, the
// in-transaction write that also records request metadata (ip_address, user_agent, request_id,
// changes — the four columns migration 000003 already declares). #1683 Task 4 adds it; Task 5's
// privacy write path is its first consumer.
//
// It is a SEPARATE interface rather than a widening of AuditLogRepository on purpose: the several
// test doubles across the repo (deletion_service_test.go's MockAuditLogRepository and the
// repository integration-test spies) implement AuditLogRepository directly, and adding a method
// there would break every one of them. Consumers that need the metadata write take this type.
type AuditLogMetadataRepository interface {
	AuditLogRepository
	LogInTxWithMetadata(ctx context.Context, tx pgx.Tx, action, resourceType, resourceID string, meta AuditLogMetadata) error
}

type auditLogRepository struct {
	db DB
}

// NewAuditLogRepository creates the production AuditLogRepository.
func NewAuditLogRepository(db DB) AuditLogRepository {
	return &auditLogRepository{db: db}
}

// NewAuditLogMetadataRepository creates the production AuditLogMetadataRepository — the same
// concrete type, exposed with the metadata-carrying method. Existing callers keep using
// NewAuditLogRepository; a consumer that needs LogInTxWithMetadata (Task 5) uses this one.
func NewAuditLogMetadataRepository(db DB) AuditLogMetadataRepository {
	return &auditLogRepository{db: db}
}

func (r *auditLogRepository) Log(ctx context.Context, action, resourceType, resourceID string) error {
	return writeAuditLogEntry(ctx, r.db, action, resourceType, resourceID)
}

// LogInTx implements AuditLogRepository.LogInTx — see that method's doc comment, and in particular
// its #1506 warning, before changing anything here. The receiver's own db handle is deliberately
// unused: the whole point is that the write lands in the CALLER's transaction.
func (r *auditLogRepository) LogInTx(ctx context.Context, tx pgx.Tx, action, resourceType, resourceID string) error {
	return writeAuditLogEntry(ctx, tx, action, resourceType, resourceID)
}

// LogInTxWithMetadata implements AuditLogMetadataRepository. It is LogInTx plus the four request-
// metadata columns migration 000003 already declares. The actor (user_id) is STILL written as the
// literal SQL NULL — see LogInTx's #1506 warning: a non-NULL actor would take FOR KEY SHARE on a
// users row inside the caller's transaction and reintroduce the cross-user 40P01 deadlock class.
func (r *auditLogRepository) LogInTxWithMetadata(ctx context.Context, tx pgx.Tx, action, resourceType, resourceID string, meta AuditLogMetadata) error {
	return writeAuditLogEntryWithMetadata(ctx, tx, action, resourceType, resourceID, meta)
}

// writeAuditLogEntry is the single statement behind both Log and LogInTx, so the two cannot drift
// apart on validation, column choice, or error wording. Same shape as recordMCPAuditEntry.
func writeAuditLogEntry(ctx context.Context, db DB, action, resourceType, resourceID string) error {
	// Refuse rather than write a row that looks like an audit entry and names nothing. An entry
	// with an empty action, or one that identifies no subject, is worse than no entry: it still
	// occupies the trail an operator queries, while answering none of the questions the trail
	// exists to answer. Same reasoning as RecordDeletionResidue's empty-effect refusal.
	//
	// This refusal is BEFORE the statement, so on the LogInTx path it fails the caller's
	// transaction without poisoning it — the caller gets an error on a still-usable tx rather than
	// a `current transaction is aborted` on every subsequent statement.
	if err := validateAuditEntry(action, resourceType, resourceID); err != nil {
		return err
	}
	// user_id is left NULL deliberately — see the type doc. It also means this INSERT performs no
	// FK check on users, so it cannot request FOR KEY SHARE on a users row another transaction
	// holds FOR UPDATE; that is the cross-user deadlock class (SQLSTATE 40P01) called out in
	// DeleteUserCascade's redaction comment. ⚠ On the LogInTx path that is not a nicety but the
	// precondition the whole in-transaction write rests on — see LogInTx's #1506 warning.
	_, err := db.Exec(ctx,
		`INSERT INTO audit_logs (user_id, action, resource_type, resource_id) VALUES (NULL, $1, $2, $3)`,
		action, resourceType, resourceID)
	if err != nil {
		return fmt.Errorf("audit log %q on %s %s: %w", action, resourceType, resourceID, err)
	}
	return nil
}

// validateAuditEntry is the refusal shared by both write helpers so they cannot drift apart. It is
// deliberately BEFORE the statement, so on an in-transaction path it fails the caller's
// transaction without poisoning it — the caller gets an error on a still-usable tx rather than a
// `current transaction is aborted` on every subsequent statement.
func validateAuditEntry(action, resourceType, resourceID string) error {
	if action == "" || resourceType == "" || resourceID == "" {
		return fmt.Errorf("audit log: action, resource type and resource id are all required (got %q/%q/%q)",
			action, resourceType, resourceID)
	}
	return nil
}

// writeAuditLogEntryWithMetadata is the metadata-carrying sibling of writeAuditLogEntry. It writes
// the same load-bearing facts plus the four request-metadata columns, and keeps user_id as the
// literal SQL NULL for the 40P01 reason restated at LogInTxWithMetadata. Invalid IP / uuid.Nil /
// nil Changes are bound as SQL NULL, never as zero values.
func writeAuditLogEntryWithMetadata(ctx context.Context, db DB, action, resourceType, resourceID string, meta AuditLogMetadata) error {
	if err := validateAuditEntry(action, resourceType, resourceID); err != nil {
		return err
	}
	var ip any
	if meta.IP.IsValid() {
		ip = meta.IP.Unmap()
	}
	var requestID any
	if meta.RequestID != uuid.Nil {
		requestID = meta.RequestID
	}
	_, err := db.Exec(ctx,
		`INSERT INTO audit_logs (user_id, action, resource_type, resource_id, ip_address, user_agent, request_id, changes) VALUES (NULL, $1, $2, $3, $4, $5, $6, $7)`,
		action, resourceType, resourceID, ip, meta.UserAgent, requestID, meta.Changes)
	if err != nil {
		return fmt.Errorf("audit log %q on %s %s: %w", action, resourceType, resourceID, err)
	}
	return nil
}

// ErrAuditLogRepositoryNotWired is returned by an explicit-destruction path whose audit sink is
// missing, INSTEAD of destroying the row un-audited. Issue #1569 round 17 task 11.
//
// The ADR's control 2 for explicit destruction of financial history is "an audit row written in
// the SAME transaction". Every repository method that implements it takes the AuditLogRepository
// as a parameter and skips the write when it is nil — deliberately, so that the many tests which
// exercise the destruction itself do not each need an audit fixture. In production that parameter
// comes from a field set by a SetAuditLogRepository call in cmd/server/main.go. Dropping that one
// line therefore used to turn control 2 OFF silently: the purge still succeeded, the trail was
// simply never written, and no test anywhere noticed.
//
// The four callers of those methods (accountService.PurgeAccount, accountService.PurgeInstitution,
// savingsGoalService.PurgeGoal and operations.ContributionHandler.DeleteContribution) now refuse
// to proceed with a nil sink and return this error. It is a misconfiguration, not a user error:
// it maps to a 500, and it can only be reached by a build whose wiring is broken — which
// TestExplicitDestructionAuditSinkIsWiredInMain also fails on in CI.
var ErrAuditLogRepositoryNotWired = errors.New(
	"audit log repository not wired: refusing to destroy financial history without an audit trail")
