import { describe, expect, it, mock } from 'bun:test'
import { CheckoutUnavailableError, RevenueCatCheckout } from './revenueCatCheckout'

const MOUNT_CLASS = 'fixed inset-0 z-[100] overflow-auto pointer-events-none [&>*]:pointer-events-auto'

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}

const tick = () => new Promise(resolve => setTimeout(resolve, 0))

function setup() {
  const purchaseResult = deferred<void>()
  const release = mock(async () => {})
  const refresh = mock(async () => ({ access_resolved: true, paid: null }))
  const purchase = mock((_offeringId: string, _packageId: string, _htmlTarget: HTMLElement) => purchaseResult.promise)
  // The hosted paywall API is gone; this canary proves the controller never reaches for it again.
  const hostedPaywall = mock(() => Promise.resolve())
  const close = mock(() => {})
  const load = mock(async (_userId: string, _isCurrent: () => boolean) => ({ purchase, close, hostedPaywall }))
  const begin = mock(async (_tier: string, _billingInterval: 'month' | 'year') => ({
    id: 'lease',
    offering: 'web',
    package: 'premium_annual',
  }))
  const controller = new RevenueCatCheckout({
    begin, release, refresh, load,
  })
  controller.setIdentity('user_clerk')
  return { controller, purchaseResult, release, refresh, purchase, hostedPaywall, load, close, begin }
}

describe('RevenueCat checkout authority and identity', () => {
  it('does not acquire a lease if the SDK cannot be configured', async () => {
    const s = setup()
    s.load.mockRejectedValue(new Error('Public key missing'))
    await s.controller.start('premium', 'month')
    expect(s.begin).not.toHaveBeenCalled()
    expect(s.controller.state).toBe('unavailable')
  })

  it('purchases the selected package into exactly one mount target', async () => {
    const s = setup()
    const mountedBefore = document.querySelectorAll('[data-ph-mask]').length
    const result = s.controller.start('premium', 'year')
    await tick()

    const mounted = document.querySelectorAll<HTMLElement>('[data-ph-mask]')
    expect(mounted.length - mountedBefore).toBe(1)
    const target = mounted[mounted.length - 1]!
    expect(target.className).toBe(MOUNT_CLASS)
    expect(s.begin).toHaveBeenCalledWith('premium', 'year')
    expect(s.purchase).toHaveBeenCalledTimes(1)
    expect(s.purchase.mock.calls[0]).toEqual(['web', 'premium_annual', target])
    expect(s.hostedPaywall).not.toHaveBeenCalled()

    s.purchaseResult.resolve()
    await result
    expect(target.isConnected).toBe(false)
  })

  it('does not confirm SDK success without backend paid access', async () => {
    const s = setup()
    const result = s.controller.start('premium', 'month')
    s.purchaseResult.resolve()
    await result
    expect(s.controller.state).toBe('pending')
    expect(s.release).not.toHaveBeenCalled()
  })

  it('confirms backend paid access and releases admission', async () => {
    const s = setup()
    s.refresh.mockResolvedValue({ access_resolved: true, paid: { status: 'active', tier: 'premium' } } as never)
    const result = s.controller.start('premium', 'month')
    s.purchaseResult.resolve()
    await result
    expect(s.controller.state).toBe('confirmed')
    expect(s.release).toHaveBeenCalledWith('lease')
    expect(s.load).toHaveBeenCalledWith('user_clerk', expect.any(Function))
  })

  it('allows verified own paid confirmation despite unresolved household access', async () => {
    const s = setup()
    s.refresh.mockResolvedValue({ access_resolved: false, paid: { status: 'active', tier: 'premium' } } as never)
    await s.controller.retry()
    expect(s.controller.state).toBe('confirmed')
  })

  it('releases explicit cancellation but retains ambiguous errors', async () => {
    for (const [error, expected] of [[{ errorCode: 1 }, 'cancelled'], [new Error('network'), 'pending']] as const) {
      const s = setup()
      const result = s.controller.start('premium', 'month')
      s.purchaseResult.reject(error)
      await result
      expect(s.controller.state).toBe(expected)
      expect(s.release.mock.calls.length).toBe(expected === 'cancelled' ? 1 : 0)
    }
  })

  it('releases the lease and reports unavailable when the SDK cannot resolve the package', async () => {
    const s = setup()
    s.purchaseResult.reject(new CheckoutUnavailableError('Checkout is unavailable'))
    await s.controller.start('premium', 'month')
    expect(s.controller.state).toBe('unavailable')
    expect(s.release).toHaveBeenCalledWith('lease')
  })

  it('cancels a removed purchase surface without waiting for its SDK promise and protects the next session', async () => {
    const s = setup()
    const result = s.controller.start('premium', 'month')
    await tick()
    const target = s.purchase.mock.calls[0]![2]
    expect(target.isConnected).toBe(true)
    s.controller.setIdentity('user_other')
    expect(target.isConnected).toBe(false)
    const nextPurchase = deferred<void>()
    s.purchase.mockImplementation(() => nextPurchase.promise)
    const next = s.controller.start('premium', 'month')
    await tick()
    expect(s.load).toHaveBeenCalledTimes(2)
    expect(s.close).toHaveBeenCalledTimes(1)
    await result // The removed surface's unresolved SDK promise must not strand this call.
    const nextTarget = s.purchase.mock.calls[1]![2]
    s.purchaseResult.resolve() // A late callback from the old SDK cannot close the next purchase.
    await tick()
    expect(nextTarget.isConnected).toBe(true)
    expect(s.controller.state).toBe('open')
    expect(s.refresh).not.toHaveBeenCalled()
    nextPurchase.resolve()
    await next
  })

  it('serializes confirmation and keeps the SDK session until verification finishes', async () => {
    const s = setup()
    const verification = deferred<{ access_resolved: boolean; paid: null }>()
    s.refresh.mockImplementation(() => verification.promise)
    const result = s.controller.start('premium', 'month')
    s.purchaseResult.resolve()
    await tick()
    const concurrent = s.controller.start('premium', 'month')
    await tick()
    const configurations = s.load.mock.calls.length
    verification.resolve({ access_resolved: false, paid: null })
    await Promise.all([result, concurrent])
    expect(configurations).toBe(1)
  })

  it('fences a deferred SDK load without letting its cleanup touch the next account', async () => {
    const s = setup()
    const staleClose = mock(() => {})
    const loading = deferred<Awaited<ReturnType<typeof s.load>>>()
    s.load.mockImplementationOnce(() => loading.promise)
    const old = s.controller.start('premium', 'month')
    s.controller.setIdentity('user_other')
    expect(s.load.mock.calls[0]![1]()).toBe(false)
    const next = s.controller.start('premium', 'month')
    await tick()
    loading.resolve({ purchase: s.purchase, close: staleClose, hostedPaywall: s.hostedPaywall })
    await old
    expect(staleClose).toHaveBeenCalledTimes(1)
    expect(s.close).not.toHaveBeenCalled()
    expect(s.purchase.mock.calls[0]![2].isConnected).toBe(true)
    await s.controller.start('premium', 'month')
    expect(s.load).toHaveBeenCalledTimes(2)
    s.purchaseResult.resolve()
    await next
  })

  it('fences deferred confirmation and preserves the next account checkout lock', async () => {
    const s = setup()
    const verification = deferred<{access_resolved: boolean; paid: null}>()
    s.refresh.mockImplementationOnce(() => verification.promise)
    const old = s.controller.retry()
    s.controller.setIdentity('user_other')
    const next = s.controller.start('premium', 'month')
    await tick()
    verification.resolve({access_resolved: true, paid: null})
    await old
    expect(s.controller.state).toBe('open')
    await s.controller.start('premium', 'month')
    expect(s.load).toHaveBeenCalledTimes(1)
    expect(s.release).not.toHaveBeenCalled()
    s.purchaseResult.resolve()
    await next
  })
})
