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
// `crypto` is the Workers-global Web Crypto API (from @cloudflare/workers-types).
// Do NOT import from 'node:crypto' — the tsconfig types allowlist excludes @types/node.

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
    issuedAt: number
    /**
     * #1625: hash of the ENTIRE gated batch, in order. A token authorises one specific batch,
     * not one specific call — which is what the user is actually confirming ("create this budget
     * with these categories"), and what makes a multi-write batch completable at all.
     */
    batchHash: string
    /**
     * One slot per gated call, keyed `${operationId}:${argsHash}`, valued with the
     * idempotencyKey minted for it at challenge time. Each slot is consumable exactly once;
     * when the last one is consumed the token is deleted, so a batch cannot be replayed.
     *
     * A Map (not a Set) because two identical calls in one batch are legitimate, so the
     * value carries a per-slot use count.
     */
    slots: Map<string, { idempotencyKey: string; uses: number }>
  }
>()

const CONFIRMATION_TTL_MS = 5 * 60 * 1000 // 5 minutes

/**
 * Validates that a token entry exists and belongs to this user/grant within TTL.
 * Extracted to eliminate duplication between validateBatchConfirmation and consumeConfirmationSlot.
 */
function isTokenEntryValid(
  entry: ReturnType<typeof pendingConfirmations.get> | undefined,
  userId: string,
  grantId: string,
  getNow: () => number,
): boolean {
  if (!entry) return false
  if (entry.userId !== userId) return false
  if (entry.grantId !== grantId) return false
  if (getNow() - entry.issuedAt > CONFIRMATION_TTL_MS) return false
  return true
}

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

/** Stable key identifying one gated call within a batch. */
async function slotKey(operationId: string, args: Record<string, unknown>): Promise<string> {
  return `${operationId}:${await hashArgs(args)}`
}

/**
 * Hash of the WHOLE batch, order-sensitive — binds a token to one exact calls array.
 *
 * #1625: this hashed only the confirmation-GATED subset while both the challenge message and the
 * invalid-token message told the agent to replay "the IDENTICAL calls array". An ungated read
 * could therefore be swapped, added or removed and the token still validated. The promise was
 * wider than the guarantee; now the guarantee matches the promise.
 */
async function hashBatch(calls: Array<{ operationId: string; args: Record<string, unknown> }>): Promise<string> {
  const keys: string[] = []
  for (const c of calls) keys.push(await slotKey(c.operationId, c.args))
  const bytes = new TextEncoder().encode(keys.join('|'))
  const digest = await crypto.subtle.digest('SHA-256', bytes)
  return [...new Uint8Array(digest)].map((b) => b.toString(16).padStart(2, '0')).join('')
}

async function issueBatchConfirmationToken(
  userId: string,
  grantId: string,
  calls: Array<{ operationId: string; args: Record<string, unknown> }>,
  gated: Array<{ operationId: string; args: Record<string, unknown> }>,
  getNow: () => number,
): Promise<string> {
  // Opportunistic sweep: evict expired entries before inserting a new one.
  // Challenged-but-never-confirmed tokens accumulate until isolate recycle without this.
  const now = getNow()
  for (const [tok, entry] of pendingConfirmations) {
    if (now - entry.issuedAt > CONFIRMATION_TTL_MS) {
      pendingConfirmations.delete(tok)
    }
  }

  const slots = new Map<string, { idempotencyKey: string; uses: number }>()
  for (const g of gated) {
    const key = await slotKey(g.operationId, g.args)
    const existing = slots.get(key)
    if (existing) {
      existing.uses += 1
    } else {
      // idempotencyKey is minted at challenge time and forwarded on the confirmed write so the
      // backend can deduplicate network retries of the same logical MCP write.
      slots.set(key, { idempotencyKey: crypto.randomUUID(), uses: 1 })
    }
  }

  const token = crypto.randomUUID()
  pendingConfirmations.set(token, {
    userId,
    grantId,
    issuedAt: now,
    // Hash covers the whole batch; SLOTS stay gated-only. Widening slots would make an ungated
    // read consume a use and fail the second time it appeared.
    batchHash: await hashBatch(calls),
    slots,
  })
  return token
}

/**
 * The message an agent sees when a token does not validate.
 *
 * #1625: this used to omit args-mismatch entirely, so an agent whose replayed payload differed
 * by a single field had no way to work out why, and would retry forever. Naming the cause and
 * the remedy is the whole fix for that loop.
 */
const INVALID_CONFIRMATION_MESSAGE =
  'Confirmation token is invalid, expired, already used, issued for a different user/grant, ' +
  'or the calls do not match the batch it was issued for. ' +
  'Re-invoke with NO confirmationToken to get a fresh one, then replay the IDENTICAL calls array with it.'

/**
 * Validates that `token` authorises exactly this batch. Does NOT consume the slots — each call
 * consumes its own slot inside invoke(), so a batch that fails mid-flight cannot silently
 * re-authorise itself.
 *
 * Returns true on success. The token is left in place; it is deleted when its last slot is used.
 */
async function validateBatchConfirmation(
  token: string,
  userId: string,
  grantId: string,
  calls: Array<{ operationId: string; args: Record<string, unknown> }>,
  getNow: () => number,
): Promise<boolean> {
  const entry = pendingConfirmations.get(token)
  if (!isTokenEntryValid(entry, userId, grantId, getNow)) return false
  return entry!.batchHash === (await hashBatch(calls))
}

/**
 * Consumes one slot for a single confirmed call. Returns its idempotency key, or null.
 *
 * Burn-before-validate is retained from the original single-token design: the slot's use count
 * is decremented SYNCHRONOUSLY, before any await, so two concurrent confirmed calls cannot both
 * read the same slot and both succeed. Unlike the original, a miss does NOT destroy the other
 * slots — doing so would recreate the very loop #1625 fixes.
 */
async function consumeConfirmationSlot(
  token: string,
  userId: string,
  grantId: string,
  operationId: string,
  args: Record<string, unknown>,
  getNow: () => number,
): Promise<{ idempotencyKey: string } | null> {
  const entry = pendingConfirmations.get(token)
  if (!isTokenEntryValid(entry, userId, grantId, getNow)) return null

  const key = await slotKey(operationId, args)
  const slot = entry!.slots.get(key)
  if (!slot || slot.uses <= 0) return null

  slot.uses -= 1
  if (slot.uses === 0) entry!.slots.delete(key)
  if (entry!.slots.size === 0) pendingConfirmations.delete(token)

  return { idempotencyKey: slot.idempotencyKey }
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
    /**
     * #1625: batch confirmation gate. Called by writeExecute BEFORE any call runs.
     *
     * With no token it mints ONE token covering every gated call and reports how many there are;
     * the caller must execute nothing and hand the token back. With a token it validates that the
     * token was issued for exactly this batch. Slots are consumed later, per call, inside invoke().
     *
     * Returns `needsConfirmation: false` when the batch contains no gated call at all.
     */
    async confirmBatch(
      calls: Array<{ operationId: string; args: Record<string, unknown> }>,
      token?: string,
    ): Promise<{ needsConfirmation: boolean; token?: string; count: number }> {
      // `calls` is the WHOLE batch. Which of them are gated is decided HERE, from the capability
      // manifest, rather than by the caller — one place makes that judgement, so the set that is
      // hashed and the set that gets slots cannot drift apart the way they did in #1625.
      const gated = calls.filter((c) => {
        const cap = getCapability(c.operationId)
        return Boolean(cap && !cap.excluded && cap.confirmationRequired)
      })
      if (gated.length === 0) return { needsConfirmation: false, count: 0 }
      if (!token) {
        const issued = await issueBatchConfirmationToken(userId, grantId, calls, gated, getNow)
        return { needsConfirmation: true, token: issued, count: gated.length }
      }
      const ok = await validateBatchConfirmation(token, userId, grantId, calls, getNow)
      if (!ok) {
        throw new HostProxyError('invalid_confirmation', INVALID_CONFIRMATION_MESSAGE)
      }
      return { needsConfirmation: false, count: gated.length }
    },

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
          // A single-call challenge is just the batch-of-one case, so it goes through the same
          // path — that keeps one code path for both and stops the two drifting apart.
          // Batch-of-one: the full batch and the gated set are the same single call, so the two
          // hash spaces coincide and a self-issued token stays redeemable through confirmBatch
          // for that exact one-call array (and, correctly, not as part of a larger one).
          const token = await issueBatchConfirmationToken(
            userId, grantId, [{ operationId, args }], [{ operationId, args }], getNow,
          )
          throw new HostProxyError(
            'confirmation_required',
            `Write operation '${operationId}' requires confirmation. ` +
              'Re-invoke with the IDENTICAL calls array plus the provided confirmationToken.',
            token,
          )
        }
        const consumed = await consumeConfirmationSlot(
          opts.confirmationToken,
          userId,
          grantId,
          operationId,
          args,
          getNow,
        )
        if (!consumed) {
          throw new HostProxyError('invalid_confirmation', INVALID_CONFIRMATION_MESSAGE)
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
        const requestConfig = args as unknown as Parameters<FetchMutator>[0]
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
            ? redactError({ message: String(err.message), status: err.status, body: err.body }).message
            : redactError(err).message
        throw new HostProxyError('backend_error', message)
      } finally {
        releaseSlot?.()
      }
    },
  }
}
