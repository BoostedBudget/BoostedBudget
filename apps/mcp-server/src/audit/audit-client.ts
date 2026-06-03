/**
 * MCP audit client — posts read-class and protocol events to the Go backend.
 *
 * Write-class events are audited by the Go backend itself synchronously
 * during the write request (same request lifecycle). This client handles
 * the async, best-effort path for read-class and protocol events from the Worker.
 *
 * Note: fire-and-forget for read events (failures must never break MCP operations).
 * Write events: the Worker calls this AFTER a successful write; if the audit call
 * fails, the write still committed but the read-side log entry is missing.
 * For true fail-closed write audit, implement within the Go write handler itself.
 */

import { MCP_WORKER_USER_AGENT } from '../client/user-agent'

/** Minimal env shape required by the audit client. */
interface AuditClientEnv {
  BOOSTEDBUDGET_API_URL: string
  BOOSTEDBUDGET_ORIGIN_SECRET: string
  MCP_WORKER_SECRET: string
}

export interface AuditEntry {
  actorUserId: string
  grantId: string
  scopes?: string[]
  operationClass: 'read' | 'write'
  operationId: string
  confirmationPresented?: boolean
  confirmationConfirmed?: boolean
  result: 'success' | 'denied' | 'error' | 'confirmation_required'
  idempotencyKey?: string
  revocationState?: string
  requestId?: string
}

export interface AuditClient {
  record(entry: AuditEntry): Promise<void>
}

/**
 * Create an audit client that posts to POST /api/v1/mcp/audit.
 * The endpoint requires two headers:
 *   - X-Origin-Secret: proves the request is from a Cloudflare-proxied origin (layer 1)
 *   - X-MCP-Worker-Secret: proves the request is from THIS Worker specifically (layer 2)
 * Returns a no-op client when env is not configured.
 */
export function createAuditClient(env: AuditClientEnv): AuditClient {
  if (!env.BOOSTEDBUDGET_API_URL || !env.BOOSTEDBUDGET_ORIGIN_SECRET || !env.MCP_WORKER_SECRET) {
    return { record: () => Promise.resolve() }
  }

  return {
    async record(entry: AuditEntry): Promise<void> {
      const url = `${env.BOOSTEDBUDGET_API_URL}/api/v1/mcp/audit`
      await fetch(url, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          // Empty UA trips the backend WAF "no User-Agent" managed-challenge rule.
          'User-Agent': MCP_WORKER_USER_AGENT,
          'X-Origin-Secret': env.BOOSTEDBUDGET_ORIGIN_SECRET,
          'X-MCP-Worker-Secret': env.MCP_WORKER_SECRET,
        },
        body: JSON.stringify({
          actor_user_id: entry.actorUserId,
          grant_id: entry.grantId,
          scopes: entry.scopes ?? [],
          operation_class: entry.operationClass,
          operation_id: entry.operationId,
          confirmation_presented: entry.confirmationPresented ?? false,
          confirmation_confirmed: entry.confirmationConfirmed ?? false,
          result: entry.result,
          idempotency_key: entry.idempotencyKey,
          revocation_state: entry.revocationState,
          request_id: entry.requestId,
        }),
      }).catch(() => {
        // Audit failures must never break MCP operations — fire-and-forget
      })
    },
  }
}
