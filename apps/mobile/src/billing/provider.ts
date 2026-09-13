export type BillingPlatform = 'ios' | 'android' | 'web'
export type BillingProvider = 'revenuecat' | 'paddle'

export function selectBillingProvider(platform: string): BillingProvider {
  if (platform === 'ios' || platform === 'android') return 'revenuecat'
  if (platform === 'web') return 'paddle'
  throw new Error(`Unsupported billing platform: ${platform}`)
}
