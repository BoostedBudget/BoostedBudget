import { afterEach, beforeEach, describe, expect, it, mock } from 'bun:test'
import { mockBeginRevenueCatCheckout } from '@/test/mocks/subscription-api'
import { CheckoutUnavailableError, RevenueCatCheckout } from './revenueCatCheckout'
import { revenueCatCheckout } from './revenueCatRuntime'

// Derived from the production class so the test cannot drift from the adapter's real signature.
type Dependencies = ConstructorParameters<typeof RevenueCatCheckout>[0]
type SdkSession = Awaited<ReturnType<Dependencies['load']>>

interface FakeOffering {
  availablePackages: { identifier: string }[]
}

const OFFERING_ID = 'web'
const PACKAGE_ID = 'premium_monthly'
const AVAILABLE: Record<string, FakeOffering> = {
  [OFFERING_ID]: { availablePackages: [{ identifier: PACKAGE_ID }] },
}

// Bun does not hoist mock.module(), and the adapter imports the SDK dynamically inside `load`,
// so registering the mock here lands before the first `load` call resolves the specifier.
const getOfferings = mock(async (): Promise<{ all: Record<string, FakeOffering> }> => ({ all: AVAILABLE }))
const sdkPurchase = mock(async (_options: unknown): Promise<unknown> => ({}))
const close = mock(() => {})
const configure = mock(() => ({ getOfferings, purchase: sdkPurchase, close }))

mock.module('@revenuecat/purchases-js', () => ({ Purchases: { configure } }))

// The adapter keeps its dependencies private. Reach the exact object the production controller
// drives rather than re-declaring the adapter's behaviour inside the test.
const { deps } = revenueCatCheckout as unknown as { deps: Dependencies }

function mountTarget() {
  const target = document.createElement('div')
  document.body.append(target)
  return target
}

async function loadSession(): Promise<SdkSession> {
  return deps.load('user_clerk', () => true)
}

describe('RevenueCat adapter checkout-unavailable contract', () => {
  let originalEnv = window.ENV

  beforeEach(() => {
    originalEnv = window.ENV
    window.ENV = { ...window.ENV, REVENUECAT_PUBLIC_API_KEY: 'rc_test_public_key' }
    configure.mockClear()
    getOfferings.mockClear()
    sdkPurchase.mockClear()
    close.mockClear()
    mockBeginRevenueCatCheckout.mockClear()
    getOfferings.mockImplementation(async () => ({ all: AVAILABLE }))
  })

  afterEach(() => {
    window.ENV = originalEnv
  })

  it('rejects an unknown package identifier as checkout-unavailable', async () => {
    const sdk = await loadSession()
    await expect(sdk.purchase(OFFERING_ID, 'premium_lifetime', mountTarget()))
      .rejects.toThrow(CheckoutUnavailableError)
  })

  it('rejects an unknown offering id as checkout-unavailable', async () => {
    getOfferings.mockImplementation(async () => ({ all: {} }))
    const sdk = await loadSession()
    await expect(sdk.purchase('unknown_offering', PACKAGE_ID, mountTarget()))
      .rejects.toThrow(CheckoutUnavailableError)
  })

  it('rejects a detached mount target as checkout-unavailable', async () => {
    const sdk = await loadSession()
    await expect(sdk.purchase(OFFERING_ID, PACKAGE_ID, document.createElement('div')))
      .rejects.toThrow(CheckoutUnavailableError)
  })

  it('classifies a failed offering fetch as checkout-unavailable before any purchase', async () => {
    getOfferings.mockImplementation(async () => { throw new Error('offline') })
    const sdk = await loadSession()
    await expect(sdk.purchase(OFFERING_ID, PACKAGE_ID, mountTarget()))
      .rejects.toThrow(CheckoutUnavailableError)
    expect(sdkPurchase).not.toHaveBeenCalled()
  })

  it('leaves an ambiguous SDK purchase rejection unclassified', async () => {
    sdkPurchase.mockImplementation(async () => { throw new Error('payment ambiguous') })
    const sdk = await loadSession()
    const rejection = await sdk.purchase(OFFERING_ID, PACKAGE_ID, mountTarget()).catch((error: unknown) => error)
    expect(rejection).toBeInstanceOf(Error)
    expect(rejection).not.toBeInstanceOf(CheckoutUnavailableError)
  })

  it('rejects an unknown tier without calling the checkout API or configuring the SDK', () => {
    expect(() => deps.begin('enterprise', 'month')).toThrow(CheckoutUnavailableError)
    expect(mockBeginRevenueCatCheckout).not.toHaveBeenCalled()
    expect(configure).not.toHaveBeenCalled()
  })
})
