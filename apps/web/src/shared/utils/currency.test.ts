import { describe, expect, it } from 'bun:test'
import { createCurrencyDisplay } from './currency'

describe('createCurrencyDisplay', () => {
  it('preserves literal malformed markers without converting the amount', () => {
    const display = createCurrencyDisplay('U$D')
    expect(display.marker).toBe('U$D')
    expect(display.format(1234.5)).toBe('U$D 1,234.50')
  })

  it.each([['USD', '$', '$25.50'], ['EUR', '€', '€25.50'], ['JPY', '¥', '¥26'], ['KWD', 'KWD', 'KWD 25.500']])(
    'preserves native %s formatting', (code, marker, formatted) => {
      const display = createCurrencyDisplay(code)
      expect(display.marker).toBe(marker)
      expect(display.format(25.5)).toBe(formatted)
    },
  )

  it('preserves explicitly requested decimal precision', () => {
    expect(createCurrencyDisplay('JPY', 2).format(25.5)).toBe('¥25.50')
  })
})
