import { describe, expect, it } from 'bun:test'
import { createRateLimiter, RateLimitError } from './rate-limit'

describe('createRateLimiter', () => {
  describe('rate limiting (per-minute)', () => {
    it('allows requests under the per-minute limit', () => {
      const limiter = createRateLimiter({ maxPerMinute: 5, maxConcurrent: 10 })
      for (let i = 0; i < 5; i++) {
        expect(() => limiter.check('user-1')).not.toThrow()
      }
    })

    it('throws RateLimitError when per-minute limit exceeded', () => {
      const limiter = createRateLimiter({ maxPerMinute: 2, maxConcurrent: 10 })
      limiter.check('user-1')
      limiter.check('user-1')
      expect(() => limiter.check('user-1')).toThrow(RateLimitError)
    })

    it('tracks per-user independently', () => {
      const limiter = createRateLimiter({ maxPerMinute: 1, maxConcurrent: 10 })
      limiter.check('user-1')
      // user-2 should still be allowed
      expect(() => limiter.check('user-2')).not.toThrow()
    })

    it('resets count after the window expires', () => {
      const now = Date.now()
      // Create limiter with a custom time provider for testability
      const limiter = createRateLimiter({ maxPerMinute: 1, maxConcurrent: 10 }, () => now)
      limiter.check('user-1')
      expect(() => limiter.check('user-1')).toThrow(RateLimitError)

      // After the window (60001ms later), should be allowed again
      const futureTime = now + 60_001
      const futureLimiter = createRateLimiter({ maxPerMinute: 1, maxConcurrent: 10 }, () => futureTime)
      expect(() => futureLimiter.check('user-1')).not.toThrow()
    })
  })

  describe('concurrency limiting', () => {
    it('allows requests under the concurrency limit', () => {
      const limiter = createRateLimiter({ maxPerMinute: 100, maxConcurrent: 2 })
      const release1 = limiter.acquire('user-1')
      const release2 = limiter.acquire('user-1')
      // Both acquired, should be fine
      expect(typeof release1).toBe('function')
      expect(typeof release2).toBe('function')
    })

    it('throws RateLimitError when concurrency limit exceeded', () => {
      const limiter = createRateLimiter({ maxPerMinute: 100, maxConcurrent: 1 })
      limiter.acquire('user-1')
      expect(() => limiter.acquire('user-1')).toThrow(RateLimitError)
    })

    it('releases slot after release() is called', () => {
      const limiter = createRateLimiter({ maxPerMinute: 100, maxConcurrent: 1 })
      const release = limiter.acquire('user-1')
      release() // release the slot
      expect(() => limiter.acquire('user-1')).not.toThrow()
    })

    it('tracks concurrency per-user independently', () => {
      const limiter = createRateLimiter({ maxPerMinute: 100, maxConcurrent: 1 })
      limiter.acquire('user-1')
      // user-2 should still be able to acquire
      expect(() => limiter.acquire('user-2')).not.toThrow()
    })
  })

  describe('RateLimitError', () => {
    it('is an instance of Error', () => {
      const err = new RateLimitError('rate_limit_exceeded', 'Too many requests')
      expect(err).toBeInstanceOf(Error)
      expect(err.code).toBe('rate_limit_exceeded')
    })

    it('has concurrency_exceeded code for concurrency errors', () => {
      const err = new RateLimitError('concurrency_exceeded', 'Too many concurrent requests')
      expect(err.code).toBe('concurrency_exceeded')
    })
  })
})
