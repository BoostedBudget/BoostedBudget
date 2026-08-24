/**
 * Map execute audit events to AuditEntry records.
 * Extracted to a separate module so it can be unit-tested independently.
 */

import type { ExecuteAuditEvent } from './tools/execute'
import type { AuditEntry } from '../audit/audit-client'

/**
 * Maps an ExecuteAuditEvent to an AuditEntry for recording.
 */
export function toAuditEntry(
  event: ExecuteAuditEvent,
  actor: { clerkUserId: string; grantId: string },
): AuditEntry {
  return {
    actorUserId: actor.clerkUserId,
    grantId: actor.grantId,
    ...event,
    confirmationPresented: event.result === 'confirmation_required',
    // `!== 'read'` rather than `=== 'write'`: since #1569 Task 16 a confirmation-gated call can
    // be class 'destructive', and `=== 'write'` would have silently recorded every confirmed
    // destructive call as unconfirmed.
    confirmationConfirmed: event.operationClass !== 'read' && event.result === 'success',
  }
}
