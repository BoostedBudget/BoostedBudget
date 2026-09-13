import type { PostHogConfig, Properties } from 'posthog-js'

export type AnalyticsLifecycleState = 'pending' | 'granted' | 'denied'

export interface AnalyticsLifecycleSnapshot {
  generation: number
  state: AnalyticsLifecycleState
  scope: string | null
}

export interface AnalyticsClient {
  capture(event: string, properties?: Properties): unknown
  clearLegacyConsent(): void
  clearPersistence(): void
  clearQueuedEvents?(): void
  identify(distinctId: string, properties?: Properties): void
  init(key: string, config: Partial<PostHogConfig>): unknown
  reset(resetDeviceId: boolean): void
  setPersistenceEnabled(enabled: boolean): void
  setPersonProperties(properties: Properties): void
  stopSessionRecording(): void
}

export interface AnalyticsCaptureGate {
  getGeneration(): number
  isGranted(): boolean
  setGranted(granted: boolean): void
}

export function createAnalyticsCaptureGate(): AnalyticsCaptureGate {
  let granted = false
  let generation = 0
  return {
    getGeneration: () => generation,
    isGranted: () => granted,
    setGranted(next) {
      if (granted && !next) generation += 1
      granted = next
    },
  }
}

interface LifecycleDependencies {
  captureGate: AnalyticsCaptureGate
  client: AnalyticsClient
  config: () => Partial<PostHogConfig>
  getHost: () => string
  getKey: () => string | undefined
  isDoNotTrackEnabled: () => boolean
}

export interface ProductAnalyticsLifecycle {
  capture(event: string, properties?: Properties): void
  deny(): void
  getSnapshot(): AnalyticsLifecycleSnapshot
  getState(): AnalyticsLifecycleState
  grant(distinctId?: string, personProperties?: Properties): void
  grantExplicitly(scope: string, distinctId?: string, personProperties?: Properties): void
  grantFromAuthority(scope: string, distinctId?: string, personProperties?: Properties): void
  init(): boolean
  resetForTests(preserveInitialization?: boolean): void
  setPersonProperties(properties: Properties): void
  setPending(): void
  setScope(scope: string): void
  subscribe(listener: (snapshot: AnalyticsLifecycleSnapshot) => void): () => void
  withdraw(scope: string): void
}

export function createProductAnalyticsLifecycle({
  captureGate,
  client,
  config,
  getHost,
  getKey,
  isDoNotTrackEnabled,
}: LifecycleDependencies): ProductAnalyticsLifecycle {
  let state: AnalyticsLifecycleState = 'pending'
  let initialized = false
  let currentScope: string | null = null
  let scopeGeneration = 0
  let withdrawnScope: string | null = null
  const listeners = new Set<(snapshot: AnalyticsLifecycleSnapshot) => void>()

  function getSnapshot(): AnalyticsLifecycleSnapshot {
    return { generation: scopeGeneration, state, scope: currentScope }
  }

  function notifyState(): void {
    for (const listener of listeners) listener(getSnapshot())
  }

  function setState(nextState: AnalyticsLifecycleState): void {
    if (state === nextState) return
    state = nextState
    notifyState()
  }

  function init(): boolean {
    if (initialized) return true
    if (!captureGate.isGranted()) return false
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

  function suspendClient(): void {
    if (initialized) client.setPersistenceEnabled(false)
    client.clearPersistence()
    if (!initialized) return
    client.clearQueuedEvents?.()
    client.stopSessionRecording()
  }

  function deny(): void {
    captureGate.setGranted(false)
    setState('denied')
    suspendClient()
    if (!initialized) return
    client.reset(true)
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
    captureGate.setGranted(true)
    if (isDoNotTrackEnabled() || !init()) {
      deny()
      return
    }
    client.clearLegacyConsent()
    client.setPersistenceEnabled(true)
    state = 'granted'
    if (distinctId) client.identify(distinctId, personProperties)
    // Subscribers may capture the first pageview, so identity must be established first.
    notifyState()
  }

  return {
    capture(event, properties) {
      if (state === 'granted' && captureGate.isGranted()) client.capture(event, properties)
    },
    deny,
    getSnapshot,
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
    resetForTests(preserveInitialization = false) {
      const keepInitialization = preserveInitialization && initialized
      captureGate.setGranted(false)
      if (initialized) {
        suspendClient()
        client.clearLegacyConsent()
        client.reset(true)
      }
      state = 'pending'
      initialized = keepInitialization
      currentScope = null
      scopeGeneration = 0
      withdrawnScope = null
      listeners.clear()
    },
    setPersonProperties(properties) {
      if (state === 'granted' && captureGate.isGranted()) client.setPersonProperties(properties)
    },
    setPending() {
      if (withdrawnScope === currentScope) return
      captureGate.setGranted(false)
      suspendClient()
      setState('pending')
    },
    setScope(scope) {
      if (scope === currentScope) return
      if (currentScope !== null) deny()
      currentScope = scope
      scopeGeneration += 1
      withdrawnScope = null
      state = 'pending'
      notifyState()
    },
    subscribe(listener) {
      listeners.add(listener)
      return () => listeners.delete(listener)
    },
    withdraw(scope) {
      if (scope !== currentScope) return
      withdrawnScope = scope
      scopeGeneration += 1
      deny()
    },
  }
}
