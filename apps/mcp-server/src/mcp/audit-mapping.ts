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
    // `!== 'read'` rather than `=== 'write'`. NOT a pre-existing bug: before #1569 Task 16
    // operationClass was typed 'read' | 'write' and execute.ts derived it from
    // `confirmationRequired ? 'write' : 'read'`, so 'destructive' was unreachable and
    // `=== 'write'` was correct. Task 16 made execute.ts record capability.class verbatim, which
    // is what would have BROKEN it — a confirmed destructive call would have been recorded as
    // unconfirmed, the exact opposite of what the tier exists to show. Pinned by
    // agent-audit.test.ts's destructive/success case and its confirmation_required control.
    confirmationConfirmed: event.operationClass !== 'read' && event.result === 'success',
  }
}
