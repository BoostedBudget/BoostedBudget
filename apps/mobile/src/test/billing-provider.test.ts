import { asBillingPlatform, selectBillingProvider } from '../billing/provider'

describe('billing provider contract', () => {
  it.each([
    ['ios', 'revenuecat'],
    ['android', 'revenuecat'],
    ['web', 'paddle'],
  ] as const)('selects the expected billing provider on %s', (platform, expected) => {
    expect(selectBillingProvider(platform)).toBe(expected)
  })

  it('narrows supported platforms and fails closed without throwing', () => {
    expect(asBillingPlatform('ios')).toBe('ios')
    expect(asBillingPlatform('windows')).toBeNull()
  })
})
