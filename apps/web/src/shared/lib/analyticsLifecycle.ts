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
  grantExplicitly(scope: string, distinctId?: string, personProperties?: Properties): void
  grantFromAuthority(scope: string, distinctId?: string, personProperties?: Properties): void
  init(): boolean
  resetForTests(): void
  setPersonProperties(properties: Properties): void
  setPending(): void
  setScope(scope: string): void
  subscribe(listener: (state: AnalyticsLifecycleState) => void): () => void
  withdraw(scope: string): void
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
  let currentScope: string | null = null
  let withdrawnScope: string | null = null
  const listeners = new Set<(state: AnalyticsLifecycleState) => void>()

  function notifyState(): void {
    for (const listener of listeners) listener(state)
  }

  function setState(nextState: AnalyticsLifecycleState): void {
    if (state === nextState) return
    state = nextState
    notifyState()
  }

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

  function deny(): void {
    setState('denied')
    if (!initialized) return
    client.stopSessionRecording()
    client.reset(true)
    client.opt_out_capturing()
  }

  function grantForScope(
    scope: string,
    isExplicitGrant: boolean,
    distinctId?: string,
    personProperties?: Properties,
  ): void {
    if (scope !== currentScope) return
    if (!isExplicitGrant && withdrawnScope === scope) return
    if (isExplicitGrant) withdrawnScope = null
    if (state === 'granted') return
    if (isDoNotTrackEnabled() || !init()) {
      setState('denied')
      return
    }
    client.opt_in_capturing({ captureEventName: false })
    state = 'granted'
    if (distinctId) client.identify(distinctId, personProperties)
    notifyState()
  }

  return {
    capture(event, properties) {
      if (state === 'granted') client.capture(event, properties)
    },
    deny,
    getState: () => state,
    grant(distinctId, personProperties) {
      if (currentScope === null) currentScope = 'legacy'
      grantForScope(currentScope, true, distinctId, personProperties)
    },
    grantExplicitly(scope, distinctId, personProperties) {
      grantForScope(scope, true, distinctId, personProperties)
    },
    grantFromAuthority(scope, distinctId, personProperties) {
      grantForScope(scope, false, distinctId, personProperties)
    },
    init,
    resetForTests() {
      state = 'pending'
      initialized = false
      currentScope = null
      withdrawnScope = null
      listeners.clear()
    },
    setPersonProperties(properties) {
      if (state === 'granted') client.setPersonProperties(properties)
    },
    setPending() {
      if (withdrawnScope === currentScope) return
      setState('pending')
    },
    setScope(scope) {
      if (scope === currentScope) return
      if (currentScope !== null) deny()
      currentScope = scope
      withdrawnScope = null
      setState('pending')
    },
    subscribe(listener) {
      listeners.add(listener)
      return () => listeners.delete(listener)
    },
    withdraw(scope) {
      if (scope !== currentScope) return
      withdrawnScope = scope
      deny()
    },
  }
}
