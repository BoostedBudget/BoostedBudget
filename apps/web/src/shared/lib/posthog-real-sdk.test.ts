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
  const sdkWithQueues = sdk as unknown as PostHogWithQueues
  const gate = createAnalyticsCaptureGate()
  const client: AnalyticsClient = {
    capture: (event, properties) => sdk.capture(event, properties),
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
  return createProductAnalyticsLifecycle({
    captureGate: gate,
    client,
    config: () => posthogConfig(gate.isGranted),
    getHost: () => 'https://posthog.invalid',
    getKey: () => 'phc_test',
    isDoNotTrackEnabled: () => false,
  })
}

describe('PostHog real SDK tab persistence', () => {
  beforeEach(() => {
    sessionStorage.clear()
    globalThis.fetch = mock(() => Promise.resolve(new Response('{}', { status: 200 }))) as unknown as typeof fetch
  })

  test('grant to pending and grant to deny both remove all SDK session persistence', () => {
    const lifecycle = realLifecycle('pending-tab')
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
    const tabA = realLifecycle(productionName)
    tabA.setScope('user-a')
    tabA.grantFromAuthority('user-a', 'user-a')
    const tabADeviceId = persistedDeviceId(productionName)
    const clonedTabStorage = Array.from(
      { length: sessionStorage.length },
      (_, index) => [sessionStorage.key(index), sessionStorage.getItem(sessionStorage.key(index) ?? '')] as const,
    )

    // A duplicated browser tab may begin with a copy of the opener's sessionStorage.
    // Initializing only after B's grant must discard that cloned SDK identity.
    const tabB = realLifecycle(productionName)
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
})
