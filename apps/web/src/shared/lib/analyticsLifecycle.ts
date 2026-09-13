import type { PostHogConfig, Properties } from 'posthog-js'

export type AnalyticsLifecycleState = 'pending' | 'granted' | 'denied'

export interface AnalyticsClient {
  capture(event: string, properties?: Properties): unknown
  identify(distinctId: string, properties?: Properties): void
  init(key: string, config: Partial<PostHogConfig>): unknown
  opt_in_capturing(options: { captureEventName: false }): void
  opt_out_capturing(): void
  reset(resetDeviceId: boolean): void
  setPersonProperties(properties: Properties): void
  stopSessionRecording(): void
}

interface LifecycleDependencies {
  client: AnalyticsClient
  config: () => Partial<PostHogConfig>
  getHost: () => string
  getKey: () => string | undefined
  isDoNotTrackEnabled: () => boolean
}

export interface ProductAnalyticsLifecycle {
  capture(event: string, properties?: Properties): void
  deny(): void
  getState(): AnalyticsLifecycleState
  grant(distinctId?: string, personProperties?: Properties): void
  init(): boolean
  resetForTests(): void
  setPersonProperties(properties: Properties): void
  setPending(): void
}

export function createProductAnalyticsLifecycle({
  client,
  config,
  getHost,
  getKey,
  isDoNotTrackEnabled,
}: LifecycleDependencies): ProductAnalyticsLifecycle {
  let state: AnalyticsLifecycleState = 'pending'
  let initialized = false

  function init(): boolean {
    if (initialized) return true
    const key = getKey()
    if (!key || isDoNotTrackEnabled()) return false
    client.init(key, {
      api_host: getHost(),
      ui_host: 'https://us.posthog.com',
      ...config(),
    })
    initialized = true
    return true
  }

  return {
    capture(event, properties) {
      if (state === 'granted') client.capture(event, properties)
    },
    deny() {
      state = 'denied'
      if (!initialized) return
      client.stopSessionRecording()
      client.reset(true)
      client.opt_out_capturing()
    },
    getState: () => state,
    grant(distinctId, personProperties) {
      if (isDoNotTrackEnabled() || !init()) {
        state = 'denied'
        return
      }
      client.opt_in_capturing({ captureEventName: false })
      state = 'granted'
      if (distinctId) client.identify(distinctId, personProperties)
    },
    init,
    resetForTests() {
      state = 'pending'
      initialized = false
    },
    setPersonProperties(properties) {
      if (state === 'granted') client.setPersonProperties(properties)
    },
    setPending() {
      state = 'pending'
    },
  }
}
