import { describe, expect, it, mock } from 'bun:test'
import { RevenueCatCheckout } from './revenueCatCheckout'

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}

function setup() {
  const purchase = deferred<void>()
  const release = mock(async () => {})
  const refresh = mock(async () => ({ access_resolved: true, paid: null }))
  const present = mock((_offering: string, _target: HTMLElement, _purchaseTarget: HTMLElement) => purchase.promise)
  const close = mock(() => {})
  const load = mock(async (_userId: string, _isCurrent: () => boolean) => ({ present, close }))
  const begin = mock(async () => ({ id: 'lease', offering: 'web' }))
  const controller = new RevenueCatCheckout({
    begin, release, refresh, load,
  })
  controller.setIdentity('user_clerk')
  return { controller, purchase, release, refresh, present, load, close, begin }
}

describe('RevenueCat checkout authority and identity', () => {
  it('does not acquire a lease if the SDK cannot be configured', async () => {
    const s = setup()
    s.load.mockRejectedValue(new Error('Public key missing'))
    await s.controller.start()
    expect(s.begin).not.toHaveBeenCalled()
    expect(s.controller.state).toBe('unavailable')
  })
  it('does not confirm SDK success without backend paid access', async () => {
    const s = setup()
    const result = s.controller.start()
    s.purchase.resolve()
    await result
    expect(s.controller.state).toBe('pending')
    expect(s.release).not.toHaveBeenCalled()
  })

  it('confirms backend paid access and releases admission', async () => {
    const s = setup()
    s.refresh.mockResolvedValue({ access_resolved: true, paid: { status: 'active', tier: 'premium' } } as never)
    const result = s.controller.start()
    s.purchase.resolve()
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
      const result = s.controller.start()
      s.purchase.reject(error)
      await result
      expect(s.controller.state).toBe(expected)
      expect(s.release.mock.calls.length).toBe(expected === 'cancelled' ? 1 : 0)
    }
  })

  it('cancels a removed paywall without waiting for its SDK promise and protects the next session', async () => {
    const s = setup()
    const result = s.controller.start()
    await new Promise(resolve => setTimeout(resolve, 0))
    const targets = s.present.mock.calls[0]!
    expect(targets[1].isConnected).toBe(true)
    s.controller.setIdentity('user_other')
    expect(targets[1].isConnected).toBe(false)
    expect(targets[2].isConnected).toBe(false)
    const nextPurchase = deferred<void>()
    s.present.mockImplementation(() => nextPurchase.promise)
    const next = s.controller.start()
    await new Promise(resolve => setTimeout(resolve, 0))
    expect(s.load).toHaveBeenCalledTimes(2)
    expect(s.close).toHaveBeenCalledTimes(1)
    await result // The removed paywall's unresolved SDK promise must not strand this call.
    const nextTargets = s.present.mock.calls[1]!
    s.purchase.resolve() // A late callback from the old SDK cannot close the next paywall.
    await new Promise(resolve => setTimeout(resolve, 0))
    expect(nextTargets[1].isConnected).toBe(true)
    expect(s.controller.state).toBe('open')
    expect(s.refresh).not.toHaveBeenCalled()
    nextPurchase.resolve()
    await next
  })

  it('serializes confirmation and keeps the SDK session until verification finishes', async () => {
    const s = setup()
    const verification = deferred<{ access_resolved: boolean; paid: null }>()
    s.refresh.mockImplementation(() => verification.promise)
    const result = s.controller.start()
    s.purchase.resolve()
    await new Promise(resolve => setTimeout(resolve, 0))
    const concurrent = s.controller.start()
    await new Promise(resolve => setTimeout(resolve, 0))
    const configurations = s.load.mock.calls.length
    verification.resolve({ access_resolved: false, paid: null })
    await Promise.all([result, concurrent])
    expect(configurations).toBe(1)
  })

  it('fences a deferred SDK load without letting its cleanup touch the next account', async () => {
    const s = setup()
    const staleClose = mock(() => {})
    const loading = deferred<{ present: typeof s.present; close: typeof staleClose }>()
    s.load.mockImplementationOnce(() => loading.promise)
    const old = s.controller.start()
    s.controller.setIdentity('user_other')
    expect(s.load.mock.calls[0]![1]()).toBe(false)
    const next = s.controller.start()
    await new Promise(resolve => setTimeout(resolve, 0))
    loading.resolve({present: s.present, close: staleClose})
    await old
    expect(staleClose).toHaveBeenCalledTimes(1)
    expect(s.close).not.toHaveBeenCalled()
    expect(s.present.mock.calls[0]![1].isConnected).toBe(true)
    await s.controller.start()
    expect(s.load).toHaveBeenCalledTimes(2)
    s.purchase.resolve()
    await next
  })

  it('fences deferred confirmation and preserves the next account checkout lock', async () => {
    const s = setup()
    const verification = deferred<{access_resolved: boolean; paid: null}>()
    s.refresh.mockImplementationOnce(() => verification.promise)
    const old = s.controller.retry()
    s.controller.setIdentity('user_other')
    const next = s.controller.start()
    await new Promise(resolve => setTimeout(resolve, 0))
    verification.resolve({access_resolved: true, paid: null})
    await old
    expect(s.controller.state).toBe('open')
    await s.controller.start()
    expect(s.load).toHaveBeenCalledTimes(1)
    expect(s.release).not.toHaveBeenCalled()
    s.purchase.resolve()
    await next
  })
})
