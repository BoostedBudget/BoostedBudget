// Display only: malformed provider/persisted codes keep their literal marker.
// This fallback does not validate, convert, or relabel money as another currency.
export function createCurrencyDisplay(currencyCode = 'USD', fractionDigits?: number) {
  try {
    const formatter = new Intl.NumberFormat('en-US', {
      style: 'currency',
      currency: currencyCode,
      ...(fractionDigits === undefined ? {} : { minimumFractionDigits: fractionDigits, maximumFractionDigits: fractionDigits }),
    })
    return {
      marker: formatter.formatToParts(0).find((part) => part.type === 'currency')?.value ?? currencyCode,
      format: (amount: number) => formatter.format(amount),
    }
  } catch {
    const formatter = new Intl.NumberFormat('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
    return { marker: currencyCode, format: (amount: number) => `${currencyCode} ${formatter.format(amount)}` }
  }
}
