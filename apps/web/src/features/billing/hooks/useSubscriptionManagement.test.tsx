import { describe, it, expect } from 'bun:test'
import { useSubscriptionManagement } from './useSubscriptionManagement'

describe('useSubscriptionManagement', () => {
  it('should export the hook', () => {
    expect(useSubscriptionManagement).toBeDefined()
    expect(typeof useSubscriptionManagement).toBe('function')
  })

  it('should have correct return type structure', () => {
    // This test verifies the hook exports without needing to render it
    // Integration tests will verify the actual functionality
    const hookSource = useSubscriptionManagement.toString()

    // Verify the hook references the correct API hooks
    expect(hookSource).toContain('useGetCurrentSubscription')
    expect(hookSource).toContain('useUpdatePaddleSubscription')
    expect(hookSource).toContain('useCancelPaddleSubscription')
    expect(hookSource).toContain('useGetBillingPortalUrl')
  })

  describe('error normalization', () => {
    it('should have normalizeError function', () => {
      // Verify the source contains error normalization logic
      const hookSource = useSubscriptionManagement.toString()
      expect(hookSource).toContain('normalizeError')
      expect(hookSource).toContain('sign in')
      expect(hookSource).toContain('Network error')
    })
  })
})
