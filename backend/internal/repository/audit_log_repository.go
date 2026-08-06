package repository

import (
	"context"
	"fmt"
)

// AuditLogRepository writes GDPR audit-trail entries to audit_logs. It is the REAL implementation
// of services.AuditLogger, which it satisfies structurally, and it replaces cmd/server/main.go's
// mockAuditLogger — the only production implementation there has ever been, which wrote an
// slog line and persisted NOTHING. Every `auditLogger.Log(...)` in the GDPR deletion path therefore
// produced no durable record at all, while audit_logs existed, was queried elsewhere, and even had
// erasure-redaction logic written against it. Issue #1506.
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
//   - user_id DOES have one. ExecuteDeletion writes its `user.deleted` entry AFTER
//     DeleteUserCascade has committed — the subject's users row no longer exists. Putting the
//     subject in user_id would make that INSERT a foreign-key violation EVERY time, in production,
//     for the single most important entry in the trail. Pinned by
//     TestAuditLogRepository_Integration_RowOutlivesAndIsWritableAfterTheSubjectsHardDeletion.
//
// THE INTERFACE CARRIES NO ACTOR, and user_id is therefore written as SQL NULL rather than filled
// in with the subject. Log(ctx, action, resourceType, resourceID) has no actor parameter, and none
// of its four production call sites has one to give: RequestDeletion and CancelDeletion receive
// only the subject's id, and ExecuteDeletion and RegisterUser run under a background worker or a
// sign-up flow with no separate acting principal. NULL is the honest reading and matches the
// column's own documented meaning ("NULL if system action or user deleted") and
// RecordDeletionResidue's precedent. Writing the subject into user_id would manufacture the claim
// "this person performed this action", which for a retention-triggered deletion — nobody asked; the
// clock expired — is simply false. A false record is worse than an absent one. Extending the
// interface with an explicit actor is tracked separately (see the ticket referenced in
// services.AuditLogger's doc comment); today no caller could supply a non-nil value for it.
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
}

type auditLogRepository struct {
	db DB
}

// NewAuditLogRepository creates the production AuditLogRepository.
func NewAuditLogRepository(db DB) AuditLogRepository {
	return &auditLogRepository{db: db}
}

func (r *auditLogRepository) Log(ctx context.Context, action, resourceType, resourceID string) error {
	// Refuse rather than write a row that looks like an audit entry and names nothing. An entry
	// with an empty action, or one that identifies no subject, is worse than no entry: it still
	// occupies the trail an operator queries, while answering none of the questions the trail
	// exists to answer. Same reasoning as RecordDeletionResidue's empty-effect refusal.
	if action == "" || resourceType == "" || resourceID == "" {
		return fmt.Errorf("audit log: action, resource type and resource id are all required (got %q/%q/%q)",
			action, resourceType, resourceID)
	}
	// user_id is left NULL deliberately — see the type doc. It also means this INSERT performs no
	// FK check on users, so it cannot request FOR KEY SHARE on a users row another transaction
	// holds FOR UPDATE; that is the cross-user deadlock class (SQLSTATE 40P01) called out in
	// DeleteUserCascade's redaction comment.
	_, err := r.db.Exec(ctx,
		`INSERT INTO audit_logs (user_id, action, resource_type, resource_id) VALUES (NULL, $1, $2, $3)`,
		action, resourceType, resourceID)
	if err != nil {
		return fmt.Errorf("audit log %q on %s %s: %w", action, resourceType, resourceID, err)
	}
	return nil
}
