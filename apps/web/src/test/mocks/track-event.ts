import { mock } from 'bun:test'

export const mockTrackEvent = mock(() => {})

mock.module('@/shared/hooks/useTrackEvent', () => ({
  useTrackEvent: () => mockTrackEvent,
}))
