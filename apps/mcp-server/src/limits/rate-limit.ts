/**
 * In-process rate/concurrency limiter for the MCP Worker (Issue #1087 GAP-4).
 *
 * Per-user, per-Durable-Object-isolate limits:
 *   - maxPerMinute: max requests per user per 60-second sliding window
 *   - maxConcurrent: max inflight requests per user at any one time
 *
 * These limits are enforced inside the host-proxy before the manifest gate.
 * Because Durable Objects are session-scoped, these are per-session limits —
 * a user with multiple concurrent sessions gets separate limits per session.
 * For cross-session rate limiting, Cloudflare's native RateLimit binding should
 * be added in a follow-up (requires Workers Paid plan).
 */

export type RateLimitErrorCode = 'rate_limit_exceeded' | 'concurrency_exceeded'

export class RateLimitError extends Error {
  constructor(
    public readonly code: RateLimitErrorCode,
    message: string,
  ) {
    super(message)
    this.name = 'RateLimitError'
  }
}

interface WindowState {
  count: number
  windowStart: number
}

export interface RateLimiterConfig {
  maxPerMinute: number
  maxConcurrent: number
}

export interface RateLimiter {
  /** Check if the user is within the per-minute rate limit. Throws RateLimitError if exceeded. */
  check(userId: string): void
  /** Acquire a concurrency slot. Returns a release() function. Throws RateLimitError if at limit. */
  acquire(userId: string): () => void
}

const WINDOW_MS = 60_000

/**
 * Create a rate/concurrency limiter.
 * @param config - maxPerMinute and maxConcurrent limits
 * @param getNow - optional time provider for testing (defaults to Date.now)
 */
export function createRateLimiter(
  config: RateLimiterConfig,
  getNow: () => number = Date.now,
): RateLimiter {
  const { maxPerMinute, maxConcurrent } = config
  const windows = new Map<string, WindowState>()
  const inflight = new Map<string, number>()

  return {
    check(userId: string): void {
      const now = getNow()
      let state = windows.get(userId)

      // Reset window if expired
      if (!state || now - state.windowStart >= WINDOW_MS) {
        state = { count: 0, windowStart: now }
        windows.set(userId, state)
      }

      if (state.count >= maxPerMinute) {
        throw new RateLimitError(
          'rate_limit_exceeded',
          `Rate limit exceeded: max ${maxPerMinute} requests per minute`,
        )
      }

      state.count++
    },

    acquire(userId: string): () => void {
      const current = inflight.get(userId) ?? 0

      if (current >= maxConcurrent) {
        throw new RateLimitError(
          'concurrency_exceeded',
          `Concurrency limit exceeded: max ${maxConcurrent} concurrent requests`,
        )
      }

      inflight.set(userId, current + 1)

      // Return a release function
      return () => {
        const after = (inflight.get(userId) ?? 1) - 1
        if (after <= 0) {
          inflight.delete(userId)
        } else {
          inflight.set(userId, after)
        }
      }
    },
  }
}
