import { useFeatureFlagEnabled } from 'posthog-js/react'
import { isPostHogConfigured } from '../lib/posthog'

export function useLoginEnabled(): { enabled: boolean; loading: boolean } {
  const flagValue = useFeatureFlagEnabled('login_enabled')

  // Local dev: no PostHog key → immediately enabled, no loading state
  if (!isPostHogConfigured()) return { enabled: true, loading: false }

  // PostHog configured but flags not yet evaluated
  if (flagValue === undefined) return { enabled: false, loading: true }

  return { enabled: flagValue, loading: false }
}
