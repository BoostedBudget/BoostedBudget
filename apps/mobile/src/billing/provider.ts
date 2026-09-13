export type BillingPlatform = 'ios' | 'android' | 'web'
export type BillingProvider = 'revenuecat' | 'paddle'

export function asBillingPlatform(platform: string): BillingPlatform | null {
  return platform === 'ios' || platform === 'android' || platform === 'web' ? platform : null
}

export function selectBillingProvider(platform: BillingPlatform): BillingProvider {
  if (platform === 'ios' || platform === 'android') return 'revenuecat'
  return 'paddle'
}
