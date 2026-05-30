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

async function issueConfirmationToken(operationId: string, args: Record<string, unknown>): Promise<string> {
  const token = crypto.randomUUID()
  pendingConfirmations.set(token, { operationId, argsHash: await hashArgs(args), issuedAt: Date.now() })
  return token
}

async function consumeConfirmationToken(
  token: string,
  operationId: string,
  args: Record<string, unknown>,
): Promise<boolean> {
  const entry = pendingConfirmations.get(token)
  if (!entry) return false
  if (entry.operationId !== operationId) return false
  if (entry.argsHash !== await hashArgs(args)) return false // args must match exactly
  if (Date.now() - entry.issuedAt > CONFIRMATION_TTL_MS) {
    pendingConfirmations.delete(token)
    return false
  }
  pendingConfirmations.delete(token)
  return true
}

export function createHostProxy(config: HostProxyConfig) {
  const { mutator, getCredentials } = config

  return {
    async invoke(operationId: string, args: Record<string, unknown>, opts: InvokeOptions = {}): Promise<unknown> {
      const entry = getCapability(operationId)
      if (!entry) {
        throw new HostProxyError('denied', `Unknown operation: ${operationId}`)
      }
      if (entry.excluded) {
        throw new HostProxyError('excluded', `Operation '${operationId}' is not available via MCP`)
      }

      if (entry.confirmationRequired) {
        if (!opts.confirmationToken) {
          const token = await issueConfirmationToken(operationId, args)
          throw new HostProxyError(
            'confirmation_required',
            `Write operation '${operationId}' requires confirmation. Re-invoke with the provided confirmationToken.`,
            token,
          )
        }
        const valid = await consumeConfirmationToken(opts.confirmationToken, operationId, args)
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
        const headers: Record<string, string> = credentials
          ? {
              Authorization: credentials.authorization,
              'X-Origin-Secret': credentials.originSecret,
              ...(opts.idempotencyKey && { 'Idempotency-Key': opts.idempotencyKey }),
            }
          : opts.idempotencyKey
            ? { 'Idempotency-Key': opts.idempotencyKey }
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
      }
    },
  }
}
