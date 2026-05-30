/**
 * Host-proxy — TRUSTED execution boundary between MCP agent and Go backend.
 * Enforces: manifest gate (unknown/excluded operations), write confirmation tokens,
 * credential injection (Authorization + X-Origin-Secret), error redaction.
 * Agent never sees user credentials; injected here on trusted side.
 */

import { getCapability } from '../manifest/capability-manifest'
import { redactError } from '../errors/redact'
import { ApiError } from '../client/fetch-mutator'
import type { FetchMutator, Credentials } from '../client/fetch-mutator'
import crypto from 'node:crypto'

export type HostProxyErrorCode =
  | 'denied'
  | 'excluded'
  | 'confirmation_required'
  | 'invalid_confirmation'
  | 'backend_error'

/** Error thrown by the host-proxy — always safe to return to the agent */
export class HostProxyError extends Error {
  constructor(
    public readonly code: HostProxyErrorCode,
    message: string,
    /** For confirmation_required: one-time confirmation token the agent must send back */
    public readonly confirmationToken?: string,
  ) {
    super(message)
    this.name = 'HostProxyError'
  }
}

export interface InvokeOptions {
  /** Confirmation token for write operations */
  confirmationToken?: string
  /** Optional idempotency key */
  idempotencyKey?: string
}

export interface HostProxyConfig {
  mutator: FetchMutator
  getCredentials?: () => Promise<Credentials>
}

// One-time confirmation tokens, one-use, bound to operationId + args hash, 5-minute TTL.
// Binding to a hash of the args prevents confirming one write from authorizing a different one.
const pendingConfirmations = new Map<
  string,
  { operationId: string; argsHash: string; issuedAt: number }
>()

const CONFIRMATION_TTL_MS = 5 * 60 * 1000 // 5 minutes

/** Simple stable hash of an args object — deterministic JSON stringify + djb2 */
function hashArgs(args: Record<string, unknown>): string {
  const json = JSON.stringify(args, Object.keys(args).sort())
  let h = 5381
  for (let i = 0; i < json.length; i++) {
    h = (((h << 5) + h) ^ json.charCodeAt(i)) >>> 0
  }
  return h.toString(16)
}

function issueConfirmationToken(operationId: string, args: Record<string, unknown>): string {
  const token = crypto.randomUUID()
  pendingConfirmations.set(token, { operationId, argsHash: hashArgs(args), issuedAt: Date.now() })
  return token
}

function consumeConfirmationToken(
  token: string,
  operationId: string,
  args: Record<string, unknown>,
): boolean {
  const entry = pendingConfirmations.get(token)
  if (!entry) return false
  if (entry.operationId !== operationId) return false
  if (entry.argsHash !== hashArgs(args)) return false // args must match exactly
  if (Date.now() - entry.issuedAt > CONFIRMATION_TTL_MS) {
    pendingConfirmations.delete(token)
    return false
  }
  // One-time use: delete immediately on consumption
  pendingConfirmations.delete(token)
  return true
}

export function createHostProxy(config: HostProxyConfig) {
  const { mutator, getCredentials } = config

  return {
    async invoke(operationId: string, args: Record<string, unknown>, opts: InvokeOptions = {}): Promise<unknown> {
      // 1. Manifest gate
      const entry = getCapability(operationId)

      if (!entry) {
        throw new HostProxyError('denied', `Unknown operation: ${operationId}`)
      }

      if (entry.excluded) {
        throw new HostProxyError('excluded', `Operation '${operationId}' is not available via MCP`)
      }

      // 2. Write gate: confirmation token required
      if (entry.confirmationRequired) {
        if (!opts.confirmationToken) {
          const token = issueConfirmationToken(operationId, args)
          throw new HostProxyError(
            'confirmation_required',
            `Write operation '${operationId}' requires confirmation. Re-invoke with the provided confirmationToken.`,
            token,
          )
        }

        const valid = consumeConfirmationToken(opts.confirmationToken, operationId, args)
        if (!valid) {
          throw new HostProxyError(
            'invalid_confirmation',
            'Confirmation token is invalid, expired, or already used.',
          )
        }
      }

      try {
        const credentials = getCredentials ? await getCredentials() : undefined
        const requestConfig = args as Parameters<FetchMutator>[0]
        const extraHeaders: Record<string, string> | undefined = credentials
          ? {
              Authorization: credentials.authorization,
              'X-Origin-Secret': credentials.originSecret,
              ...(opts.idempotencyKey && { 'Idempotency-Key': opts.idempotencyKey }),
            }
          : undefined

        return await mutator(
          requestConfig,
          extraHeaders
            ? { headers: extraHeaders }
            : opts.idempotencyKey
              ? { idempotencyKey: opts.idempotencyKey }
              : undefined,
        )
      } catch (err) {
        const message =
          err instanceof ApiError
            ? redactError({ message: String(err.message), status: err.status }).message
            : redactError(err).message
        throw new HostProxyError('backend_error', message)
      }
    },
  }
}
