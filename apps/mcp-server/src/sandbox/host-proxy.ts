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
import { createRateLimiter, RateLimitError } from '../limits/rate-limit'
import type { RateLimiter } from '../limits/rate-limit'
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
  /** User ID for rate/concurrency limiting AND confirmation token binding (clerkUserId) */
  userId?: string
  /** Grant ID for confirmation token binding — prevents cross-grant replay after re-auth */
  grantId?: string
  /** Rate limiter instance — shared across tool invocations in the same DO session */
  rateLimiter?: RateLimiter
  /**
   * Injectable clock for confirmation token TTL — returns current epoch ms.
   * Defaults to Date.now(). Override in tests to control time without real sleep.
   */
  getNow?: () => number
}

// Default limiter: 60 req/min, 5 concurrent per DO session (per-user, per-session)
const DEFAULT_LIMITER = createRateLimiter({ maxPerMinute: 60, maxConcurrent: 5 })

// One-time confirmation tokens, one-use, 5-minute TTL.
// Bound to: userId + grantId + operationId + argsHash.
// The userId+grantId binding prevents cross-user and cross-grant replay within a shared isolate.
// The argsHash binding prevents a token for one write from authorizing a different write.
// idempotencyKey is a stable UUID generated at challenge time and forwarded on the confirmed write
// so the backend can deduplicate retries of the same logical operation.
const pendingConfirmations = new Map<
  string,
  {
    userId: string
    grantId: string
    operationId: string
    argsHash: string
    issuedAt: number
    idempotencyKey: string
  }
>()

const CONFIRMATION_TTL_MS = 5 * 60 * 1000 // 5 minutes

/**
 * Recursively stable-stringify for use in crypto hash — handles nested objects
 * deterministically so nested field ordering cannot produce collisions.
 */
function stableStringify(v: unknown): string {
  if (v === null || typeof v !== 'object') return JSON.stringify(v)
  if (Array.isArray(v)) return '[' + v.map(stableStringify).join(',') + ']'
  const keys = Object.keys(v as object).sort()
  return '{' + keys.map((k) => JSON.stringify(k) + ':' + stableStringify((v as Record<string, unknown>)[k])).join(',') + '}'
}

/** SHA-256 hash of args — collision-resistant, includes all nested fields */
async function hashArgs(args: Record<string, unknown>): Promise<string> {
  const bytes = new TextEncoder().encode(stableStringify(args))
  const digest = await crypto.subtle.digest('SHA-256', bytes)
  return [...new Uint8Array(digest)].map((b) => b.toString(16).padStart(2, '0')).join('')
}

async function issueConfirmationToken(
  userId: string,
  grantId: string,
  operationId: string,
  args: Record<string, unknown>,
  getNow: () => number,
): Promise<string> {
  const token = crypto.randomUUID()
  // idempotencyKey is generated once at challenge time and forwarded on the confirmed write
  // so the backend can deduplicate network retries of the same logical MCP write.
  const idempotencyKey = crypto.randomUUID()
  pendingConfirmations.set(token, {
    userId,
    grantId,
    operationId,
    argsHash: await hashArgs(args),
    issuedAt: getNow(),
    idempotencyKey,
  })
  return token
}

/**
 * Returns the idempotency key that was minted at challenge time, or null if invalid.
 *
 * The token is burned (deleted from the Map) BEFORE any async work so that two
 * concurrent confirmed calls with the same token cannot both read the same entry
 * and both succeed. Burning an invalid concurrent attempt is preferable to
 * authorising two writes.
 */
async function consumeConfirmationToken(
  token: string,
  userId: string,
  grantId: string,
  operationId: string,
  args: Record<string, unknown>,
  getNow: () => number,
): Promise<{ idempotencyKey: string } | null> {
  // Capture and immediately burn — one-time use enforced before any async work.
  const entry = pendingConfirmations.get(token)
  if (!entry) return null
  pendingConfirmations.delete(token) // burn first; sync, before any await

  // Binding checks: reject cross-user, cross-grant, wrong-op, expired, and args-mismatch.
  if (entry.userId !== userId) return null
  if (entry.grantId !== grantId) return null
  if (entry.operationId !== operationId) return null
  if (getNow() - entry.issuedAt > CONFIRMATION_TTL_MS) return null
  if (entry.argsHash !== await hashArgs(args)) return null // TOCTOU: args must match exactly
  return { idempotencyKey: entry.idempotencyKey }
}

export function createHostProxy(config: HostProxyConfig) {
  const {
    mutator,
    getCredentials,
    userId = 'anonymous',
    grantId = 'anonymous',
    rateLimiter = DEFAULT_LIMITER,
    getNow = Date.now,
  } = config

  return {
    async invoke(operationId: string, args: Record<string, unknown>, opts: InvokeOptions = {}): Promise<unknown> {
      // Rate limit check (before manifest gate — saves DB lookups on abusive callers)
      try {
        rateLimiter.check(userId)
      } catch (err) {
        if (err instanceof RateLimitError) {
          throw new HostProxyError('denied', err.message)
        }
        throw err
      }

      const entry = getCapability(operationId)
      if (!entry) {
        throw new HostProxyError('denied', `Unknown operation: ${operationId}`)
      }
      if (entry.excluded) {
        throw new HostProxyError('excluded', `Operation '${operationId}' is not available via MCP`)
      }

      let confirmedIdempotencyKey: string | undefined
      if (entry.confirmationRequired) {
        if (!opts.confirmationToken) {
          // Token is bound to this userId+grantId — prevents cross-user and cross-grant replay.
          // The idempotency key is minted here and forwarded on the confirmed call so the backend
          // can deduplicate retries.
          const token = await issueConfirmationToken(userId, grantId, operationId, args, getNow)
          throw new HostProxyError(
            'confirmation_required',
            `Write operation '${operationId}' requires confirmation. Re-invoke with the provided confirmationToken.`,
            token,
          )
        }
        const consumed = await consumeConfirmationToken(opts.confirmationToken, userId, grantId, operationId, args, getNow)
        if (!consumed) {
          throw new HostProxyError(
            'invalid_confirmation',
            'Confirmation token is invalid, expired, already used, or issued for a different user/grant.',
          )
        }
        confirmedIdempotencyKey = consumed.idempotencyKey
      }

      // Acquire concurrency slot
      let releaseSlot: (() => void) | undefined
      try {
        releaseSlot = rateLimiter.acquire(userId)
      } catch (err) {
        if (err instanceof RateLimitError) {
          throw new HostProxyError('denied', err.message)
        }
        throw err
      }

      try {
        const credentials = getCredentials ? await getCredentials() : undefined
        const requestConfig = args as Parameters<FetchMutator>[0]
        // Use idempotency key from confirmed write (stable, minted at challenge time) if available,
        // otherwise fall back to caller-provided key.
        const effectiveIdempotencyKey = confirmedIdempotencyKey ?? opts.idempotencyKey
        const headers: Record<string, string> = credentials
          ? {
              Authorization: credentials.authorization,
              'X-Origin-Secret': credentials.originSecret,
              ...(effectiveIdempotencyKey && { 'Idempotency-Key': effectiveIdempotencyKey }),
            }
          : effectiveIdempotencyKey
            ? { 'Idempotency-Key': effectiveIdempotencyKey }
            : {}

        return await mutator(
          requestConfig,
          Object.keys(headers).length > 0 ? { headers } : undefined,
        )
      } catch (err) {
        const message =
          err instanceof ApiError
            ? redactError({ message: String(err.message), status: err.status }).message
            : redactError(err).message
        throw new HostProxyError('backend_error', message)
      } finally {
        releaseSlot?.()
      }
    },
  }
}
