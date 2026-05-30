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
    confirmationConfirmed: event.operationClass === 'write' && event.result === 'success',
  }
}
