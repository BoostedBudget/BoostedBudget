import { beforeEach, describe, expect, mock, test } from 'bun:test'
import { PostHog, type PostHogConfig } from 'posthog-js'
import {
  createAnalyticsCaptureGate,
  createProductAnalyticsLifecycle,
  type AnalyticsClient,
} from './analyticsLifecycle'
import {
  clearPostHogQueuedEvents,
  clearPostHogSessionPersistence,
  guardPostHogRetryQueue,
  posthogConfig,
  setPostHogPersistenceEnabled,
  type PostHogWithQueues,
} from './posthog'

function storageKeysFor(name: string): string[] {
  return Array.from({ length: sessionStorage.length }, (_, index) => sessionStorage.key(index))
    .filter((key): key is string => key?.includes(name) === true)
}

function persistedDeviceId(name: string): string {
  const value = sessionStorage.getItem(`ph_${name}`)
  if (!value) throw new Error(`missing PostHog persistence for ${name}`)
  return (JSON.parse(value) as { $device_id: string }).$device_id
}

function realLifecycle(name: string) {
  const sdk = new PostHog()
  const capturedEvents: string[] = []
  const sdkCapture = sdk.capture.bind(sdk)
  sdk.capture = ((...args: Parameters<typeof sdk.capture>) => {
    capturedEvents.push(args[0])
    return sdkCapture(...args)
  }) as typeof sdk.capture
  const sdkWithQueues = sdk as unknown as PostHogWithQueues
  const gate = createAnalyticsCaptureGate()
  const client: AnalyticsClient = {
    capture: (event, properties) => sdk.capture(event, properties),
    clearLegacyConsent: () => sdk.clear_opt_in_out_capturing(),
    clearPersistence: () => clearPostHogSessionPersistence(name),
    clearQueuedEvents: () => clearPostHogQueuedEvents(sdkWithQueues),
    identify: (id, properties) => sdk.identify(id, properties),
    init: (key, config) => {
      clearPostHogSessionPersistence(name)
      const result = sdk.init(key, {
        ...config,
        api_host: 'https://posthog.invalid',
        api_transport: 'fetch',
        persistence_name: name,
      } as Partial<PostHogConfig>)
      guardPostHogRetryQueue(sdkWithQueues, gate.getGeneration, gate.isGranted)
      return result
    },
    reset: resetDeviceId => sdk.reset(resetDeviceId),
    setPersistenceEnabled: enabled => setPostHogPersistenceEnabled(sdk, enabled),
    setPersonProperties: properties => sdk.setPersonProperties(properties),
    stopSessionRecording: () => sdk.stopSessionRecording(),
  }
  const lifecycle = createProductAnalyticsLifecycle({
    captureGate: gate,
    client,
    config: () => posthogConfig(gate.isGranted),
    getHost: () => 'https://posthog.invalid',
    getKey: () => 'phc_test',
    isDoNotTrackEnabled: () => false,
  })
  return { lifecycle, sdk, capturedEvents }
}

describe('PostHog real SDK tab persistence', () => {
  beforeEach(() => {
    sessionStorage.clear()
    localStorage.clear()
    Object.defineProperty(navigator, 'userAgent', {
      configurable: true,
      value: 'Mozilla/5.0 AppleWebKit/537.36 Chrome/140.0 Safari/537.36',
    })
    globalThis.fetch = mock(() => Promise.resolve(new Response('{}', { status: 200 }))) as unknown as typeof fetch
  })

  test('grant to pending and grant to deny both remove all SDK session persistence', () => {
    const { lifecycle } = realLifecycle('pending-tab')
    lifecycle.setScope('user-a')
    lifecycle.grantFromAuthority('user-a', 'user-a')
    expect(storageKeysFor('pending-tab').length).toBeGreaterThan(0)

    lifecycle.setPending()
    expect(storageKeysFor('pending-tab')).toEqual([])

    lifecycle.grantFromAuthority('user-a', 'user-a')
    expect(storageKeysFor('pending-tab').length).toBeGreaterThan(0)
    lifecycle.deny()
    expect(storageKeysFor('pending-tab')).toEqual([])
  })

  test('one tab withdrawal clears only that SDK session and leaves another tab capturing', () => {
    const productionName = 'shared-production-name'
    const { lifecycle: tabA } = realLifecycle(productionName)
    tabA.setScope('user-a')
    tabA.grantFromAuthority('user-a', 'user-a')
    const tabADeviceId = persistedDeviceId(productionName)
    const clonedTabStorage = Array.from(
      { length: sessionStorage.length },
      (_, index) => [sessionStorage.key(index), sessionStorage.getItem(sessionStorage.key(index) ?? '')] as const,
    )

    // A duplicated browser tab may begin with a copy of the opener's sessionStorage.
    // Initializing only after B's grant must discard that cloned SDK identity.
    const { lifecycle: tabB } = realLifecycle(productionName)
    tabB.setScope('user-b')
    tabB.grantFromAuthority('user-b', 'user-b')
    expect(persistedDeviceId(productionName)).not.toBe(tabADeviceId)

    const tabBStorage = Array.from(
      { length: sessionStorage.length },
      (_, index) => [sessionStorage.key(index), sessionStorage.getItem(sessionStorage.key(index) ?? '')] as const,
    )
    sessionStorage.clear()
    for (const [key, value] of clonedTabStorage) if (key && value) sessionStorage.setItem(key, value)

    tabA.withdraw('user-a')

    expect(storageKeysFor(productionName)).toEqual([])
    sessionStorage.clear()
    for (const [key, value] of tabBStorage) if (key && value) sessionStorage.setItem(key, value)
    expect(storageKeysFor(productionName).length).toBeGreaterThan(0)
    expect(tabB.getState()).toBe('granted')
  })

  for (const authority of ['anonymous cookie', 'signed-in server'] as const) {
    test(`migrates a legacy native denial after ${authority} grant`, () => {
      const legacyConsentKey = '__ph_opt_in_out_phc_test'
      localStorage.setItem(legacyConsentKey, '0')
      const { lifecycle, sdk, capturedEvents } = realLifecycle(`legacy-${authority}`)
      const scope = authority === 'anonymous cookie' ? 'anonymous' : 'user-a'
      lifecycle.setScope(scope)

      lifecycle.capture('before_authority')
      expect(storageKeysFor(`legacy-${authority}`)).toEqual([])
      expect(localStorage.getItem(legacyConsentKey)).toBe('0')

      if (authority === 'anonymous cookie') {
        lifecycle.grantFromAuthority(scope)
      } else {
        lifecycle.grantFromAuthority(scope, 'user-a')
      }

      expect(lifecycle.getState()).toBe('granted')
      expect(sdk.is_capturing()).toBe(true)
      expect(capturedEvents).not.toContain('$opt_in')
      const eventsBeforeCapture = capturedEvents.length
      lifecycle.capture('after_authority')
      expect(capturedEvents).toHaveLength(eventsBeforeCapture + 1)
      expect(capturedEvents.at(-1)).toBe('after_authority')
      expect(capturedEvents).not.toContain('$opt_in')
      expect(localStorage.getItem(legacyConsentKey)).toBeNull()
      expect(storageKeysFor(`legacy-${authority}`).length).toBeGreaterThan(0)
    })
  }
})
