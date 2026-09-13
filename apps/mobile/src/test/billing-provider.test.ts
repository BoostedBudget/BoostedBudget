import { selectBillingProvider } from '../billing/provider'

describe('billing provider contract', () => {
  it.each([
    ['ios', 'revenuecat'],
    ['android', 'revenuecat'],
    ['web', 'paddle'],
  ] as const)('selects the expected billing provider on %s', (platform, expected) => {
    expect(selectBillingProvider(platform)).toBe(expected)
  })

  it('fails closed for unsupported platforms', () => {
    expect(() => selectBillingProvider('windows')).toThrow('Unsupported billing platform: windows')
  })
})
