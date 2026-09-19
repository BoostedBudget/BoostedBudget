export interface CheckoutAuthority {
  paid?: { status: string; tier: string } | null
  access_resolved: boolean
}

export type CheckoutState = 'idle' | 'loading' | 'open' | 'pending' | 'confirmed' | 'cancelled' | 'unavailable'

interface Dependencies {
  begin: (tier: string, billingInterval: 'month' | 'year') => Promise<{ id: string; offering: string; package: string }>
  release: (id: string) => Promise<unknown>
  refresh: () => Promise<CheckoutAuthority>
  load: (userId: string, isCurrent: () => boolean) => Promise<{
    purchase: (offeringId: string, packageId: string, htmlTarget: HTMLElement) => Promise<unknown>
    close: () => void
  }>
}

/** One SDK session at a time. SDK close() stops tracking; it does not dismiss a paywall. */
export class RevenueCatCheckout {
  state: CheckoutState = 'idle'
  private userId: string | null = null
  private generation = 0
  private busy = false
  private cancelActive?: () => void
  private lease?: string
  private targets: HTMLElement[] = []
  private listeners = new Set<() => void>()

  constructor(private readonly deps: Dependencies) {}

  subscribe = (listener: () => void) => {
    this.listeners.add(listener)
    return () => { this.listeners.delete(listener) }
  }
  getSnapshot = () => this.state

  private update(state: CheckoutState) {
    this.state = state
    this.listeners.forEach(listener => listener())
  }

  private destroyTargets() {
    this.targets.forEach(target => { target.replaceChildren(); target.remove() })
    this.targets = []
  }

  setIdentity(userId: string | null) {
    if (this.userId === userId) return
    this.userId = userId
    this.generation++
    this.cancelActive?.()
    this.cancelActive = undefined
    this.busy = false
    this.lease = undefined
    this.destroyTargets()
    this.update('idle')
  }

  async retry() {
    if (!this.userId || this.busy) return
    this.busy = true
    const generation = this.generation
    try {
      await this.confirm(generation)
    } finally {
      if (generation === this.generation) this.busy = false
    }
  }

  private async confirm(generation: number) {
    this.update('loading')
    try {
      const summary = await this.deps.refresh()
      if (generation !== this.generation) return
      if (summary.paid?.status === 'active') {
        this.update('confirmed')
        if (this.lease) await this.deps.release(this.lease).catch(() => {})
        if (generation === this.generation) this.lease = undefined
      } else this.update('pending')
    } catch {
      if (generation === this.generation) this.update('pending')
    }
  }

  async start(tier: string, billingInterval: 'month' | 'year') {
    if (!this.userId || this.busy) return
    this.busy = true
    const generation = this.generation
    const userId = this.userId
    const current = () => generation === this.generation
    let sdk: Awaited<ReturnType<Dependencies['load']>> | undefined
    let sdkClosed = false
    let targets: HTMLElement[] = []
    const closeSDK = () => {
      if (sdk && !sdkClosed) { sdkClosed = true; sdk.close() }
    }
    const removeTargets = () => targets.forEach(target => { target.replaceChildren(); target.remove() })
    const cancelled = new Promise<void>(resolve => {
      this.cancelActive = () => { closeSDK(); removeTargets(); resolve() }
    })
    this.update('loading')
    try {
      // Configuration failure must not strand a checkout lease for thirty minutes.
      // Admission still precedes all offering/paywall interaction.
      sdk = await this.deps.load(userId, current)
      if (!current()) return
      const intent = await this.deps.begin(tier, billingInterval)
      if (!current()) return
      this.lease = intent.id
      targets = [document.createElement('div')]
      this.targets = targets
      const target = targets[0]!
      target.setAttribute('data-ph-mask', '')
      target.className = 'fixed inset-0 z-[100] overflow-auto pointer-events-none [&>*]:pointer-events-auto'
      document.body.append(target)
      this.update('open')
      // Removing SDK DOM need not settle the purchase. Our cancellation must.
      await Promise.race([sdk.purchase(intent.offering, intent.package, targets[0]!), cancelled])
      if (!current()) return
      await this.confirm(generation)
    } catch (error) {
      if (!current()) return
      const cancelled = typeof error === 'object' && error !== null && 'errorCode' in error && error.errorCode === 1
      if (cancelled) {
        if (this.lease) await this.deps.release(this.lease).catch(() => {})
        if (current()) { this.lease = undefined; this.update('cancelled') }
      } else this.update(this.lease ? 'pending' : 'unavailable')
    } finally {
      closeSDK()
      removeTargets()
      if (current()) {
        this.targets = []
        this.cancelActive = undefined
        this.busy = false
      }
    }
  }
}
