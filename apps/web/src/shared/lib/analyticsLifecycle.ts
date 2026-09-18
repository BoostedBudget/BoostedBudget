import type { PostHogConfig, Properties } from 'posthog-js'
import { ANALYTICS_EVENTS } from '../analytics'

export type AnalyticsLifecycleState = 'pending' | 'granted' | 'denied'

export interface AnalyticsPurposes {
  analytics: boolean
  identifiedDiagnostics: boolean
  interactionCapture: boolean
}

export const NO_PURPOSES: AnalyticsPurposes = {
  analytics: false,
  identifiedDiagnostics: false,
  interactionCapture: false,
}

export interface AnalyticsAuthority {
  scope: string
  purposes: AnalyticsPurposes
  distinctId?: string
  personProperties?: Properties
}

export interface AnalyticsLifecycleSnapshot {
  generation: number
  state: AnalyticsLifecycleState
  scope: string | null
  withdrawn: boolean
}

export interface AnalyticsClient {
  capture(event: string, properties?: Properties): unknown
  clearLegacyConsent(): void
  clearPersistence(): void
  clearQueuedEvents?(): void
  identify(distinctId: string, properties?: Properties): void
  init(key: string, config: Partial<PostHogConfig>): unknown
  ready?(): Promise<void>
  reset(resetDeviceId: boolean): void
  setCaptureHeatmaps(enabled: boolean): void
  setPersistenceEnabled(enabled: boolean): void
  setPersonProperties(properties: Properties): void
  startSessionRecording(): void
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
  apply(authority: AnalyticsAuthority): Promise<void>
  capture(event: string, properties?: Properties): void
  clearWithdrawal(scope: string): void
  deny(): void
  getSnapshot(): AnalyticsLifecycleSnapshot
  getState(): AnalyticsLifecycleState
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
  // `authorityGeneration` moves only on scope-level mutations (setScope, withdraw,
  // clearWithdrawal); it is the snapshot's public generation, so purpose-only
  // reconciles cannot re-fire pageviews. `reconcileGeneration` moves on every
  // apply() and feeds only the post-await bail.
  let authorityGeneration = 0
  let reconcileGeneration = 0
  const withdrawnScopes = new Set<string>()
  let applied: AnalyticsPurposes = { ...NO_PURPOSES }
  let suspended = false
  const listeners = new Set<(snapshot: AnalyticsLifecycleSnapshot) => void>()

  function isWithdrawn(scope: string | null): boolean {
    return scope !== null && withdrawnScopes.has(scope)
  }

  function getSnapshot(): AnalyticsLifecycleSnapshot {
    return {
      generation: authorityGeneration,
      state,
      scope: currentScope,
      withdrawn: isWithdrawn(currentScope),
    }
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
    client.setCaptureHeatmaps(false)
    applied = { ...NO_PURPOSES }
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

  function reconcile(purposes: AnalyticsPurposes, personProperties?: Properties): void {
    if (purposes.identifiedDiagnostics !== applied.identifiedDiagnostics) {
      if (purposes.identifiedDiagnostics) {
        const name = personProperties?.name
        if (typeof name === 'string' && name.length > 0) client.setPersonProperties({ name })
      } else {
        // Raw SDK capture: the typed product-event facade would drop `$unset`.
        client.capture(ANALYTICS_EVENTS.POSTHOG_SET, { $unset: ['name'] })
      }
    }
    if (purposes.interactionCapture !== applied.interactionCapture) {
      if (purposes.interactionCapture) client.startSessionRecording()
      else client.stopSessionRecording()
    }
    applied = { ...purposes }
  }

  async function apply(authority: AnalyticsAuthority): Promise<void> {
    const { scope, purposes, distinctId, personProperties } = authority
    const authorityAtStart = authorityGeneration
    reconcileGeneration += 1
    const reconcileAtStart = reconcileGeneration

    if (scope !== currentScope) return
    if (withdrawnScopes.has(scope)) return
    if (!purposes.analytics) {
      deny()
      return
    }

    if (state !== 'granted') {
      suspended = false
      if (client.ready) {
        try {
          await client.ready()
        } catch {
          deny()
          return
        }
      }
      // The SDK is loaded lazily, so authority can change while the import is in
      // flight. A withdrawal, a scope change, a newer apply or a suspension must
      // win over this stale continuation.
      if (
        authorityAtStart !== authorityGeneration ||
        reconcileAtStart !== reconcileGeneration ||
        scope !== currentScope ||
        withdrawnScopes.has(scope) ||
        suspended
      ) return
      if (isDoNotTrackEnabled() || !init()) {
        deny()
        return
      }
      client.clearLegacyConsent()
      client.setPersistenceEnabled(true)
      state = 'granted'
      captureGate.setGranted(true)
      client.setCaptureHeatmaps(purposes.analytics)
      if (distinctId) client.identify(distinctId, personProperties)
      reconcile(purposes, personProperties)
      notifyState()
      return
    }

    reconcile(purposes, personProperties)
  }

  return {
    apply,
    capture(event, properties) {
      if (state === 'granted' && captureGate.isGranted()) client.capture(event, properties)
    },
    clearWithdrawal(scope) {
      if (!withdrawnScopes.has(scope)) return
      withdrawnScopes.delete(scope)
      if (scope !== currentScope) return
      authorityGeneration += 1
      notifyState()
    },
    deny,
    getSnapshot,
    getState: () => state,
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
      authorityGeneration = 0
      reconcileGeneration = 0
      withdrawnScopes.clear()
      applied = { ...NO_PURPOSES }
      suspended = false
      listeners.clear()
    },
    setPersonProperties(properties) {
      if (state === 'granted' && captureGate.isGranted()) client.setPersonProperties(properties)
    },
    setPending() {
      if (isWithdrawn(currentScope)) return
      suspended = true
      captureGate.setGranted(false)
      suspendClient()
      setState('pending')
    },
    setScope(scope) {
      if (scope === currentScope) return
      if (currentScope !== null) deny()
      currentScope = scope
      authorityGeneration += 1
      state = 'pending'
      notifyState()
    },
    subscribe(listener) {
      listeners.add(listener)
      return () => listeners.delete(listener)
    },
    withdraw(scope) {
      if (scope !== currentScope) return
      withdrawnScopes.add(scope)
      authorityGeneration += 1
      const previousState = state
      deny()
      if (state === previousState) notifyState()
    },
  }
}
