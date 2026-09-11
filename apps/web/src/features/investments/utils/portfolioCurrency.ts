/**
 * Currency rendering for the investment surfaces (#1631 CX1).
 *
 * Every investment figure used to go through a dollar-DEFAULTING formatter, so a GBP portfolio
 * rendered "$52,340.50" and a mixed-currency portfolio rendered "$52,340.50" as well -- the UI
 * asserting a currency the backend had explicitly REFUSED to assert. The backend serves
 * `currency_code: null` alongside `is_mixed_currency: true` for exactly that reason: there is no
 * FX rate source in this system, so a multi-currency total is a currency-blind sum and not a
 * quantity of any currency at all.
 *
 * Three states, and they are NOT two:
 *
 *   named    - a currency_code is stated. Render in it.
 *   mixed    - is_mixed_currency is true. Render the number with NO symbol, and show the badge.
 *   unknown  - no code and not mixed. Render as dollars, exactly as before.
 *
 * The third is the state of every row written before migration 000197, and treating it as mixed
 * would strip the symbol from the whole existing user base on deploy day with no change to their
 * data. Unknown is not a conflict -- the same rule the backend applies throughout this task.
 *
 * ⚠ #1631 I3 -- there is also a FOURTH situation, and it is not "unknown": a `currency_code` that
 * fails the three-letter test is a provider value we could not parse, not a value that was never
 * set. Falling back to dollars there would re-commit this file's original defect in miniature, so
 * it gets the mixed case's treatment (unlabelled), not the unknown case's (dollars) -- even though
 * it is neither named nor mixed. Do not fold it into `unknown`; do not fold it into `mixed` in the
 * `is_mixed_currency` sense either, since `isCurrencyUnnameable` stays about the backend's own
 * refusal and a bad string is a client-side parse failure, not that.
 */

/** The two currency fields every investment aggregate carries. Both optional: an older cached
 *  response, or a mid-rolling-deploy client, can be missing either. */
export interface PortfolioCurrencyFields {
  currency_code?: string | null
  is_mixed_currency?: boolean
}

/** True when the backend has refused to name a currency because the total mixes several. */
export function isCurrencyUnnameable(fields: PortfolioCurrencyFields | undefined): boolean {
  return fields?.is_mixed_currency === true
}

/**
 * Formats an investment amount in the currency the API named, or unlabelled when it refused to
 * name one. Two decimal places throughout, matching formatCurrencyFull.
 */
export function formatPortfolioAmount(
  value: number,
  fields: PortfolioCurrencyFields | undefined,
  /** Decimal places. Defaults to 2, matching formatCurrencyFull. Pass 0 at the call sites that
   *  previously used formatCurrency, so this change moves the SYMBOL and nothing else. */
  fractionDigits: number = 2,
): string {
  if (isCurrencyUnnameable(fields)) {
    // No symbol. A currency-blind sum labelled with any currency is the confidently-wrong number
    // this function exists to prevent -- and the mixed flag wins over a stated code, because a
    // code arriving alongside the flag is a contradiction, not permission to label.
    return new Intl.NumberFormat('en-US', {
      minimumFractionDigits: fractionDigits,
      maximumFractionDigits: fractionDigits,
    }).format(value)
  }

  const code = fields?.currency_code

  // `undefined`/absent is a DIFFERENT situation from a malformed code: it means a legacy row
  // written before migration 000197, and rendering it as dollars is the documented, deliberate
  // choice for the existing user base. Do not touch this path.
  if (code == null) {
    return new Intl.NumberFormat('en-US', {
      style: 'currency',
      currency: 'USD',
      minimumFractionDigits: fractionDigits,
      maximumFractionDigits: fractionDigits,
    }).format(value)
  }

  // A code that fails the three-letter test is a FOURTH situation: a provider value we could not
  // parse, not a value that was never set. Falling back to USD would re-commit the original defect
  // in miniature -- labelling a figure with a currency nobody established. Intl.NumberFormat raises
  // a RangeError on anything that is not three letters, so this still has to avoid throwing; it
  // does that the same way the mixed-currency case does, by rendering the number unlabelled rather
  // than by guessing a currency.
  if (!/^[A-Za-z]{3}$/.test(code)) {
    return new Intl.NumberFormat('en-US', {
      minimumFractionDigits: fractionDigits,
      maximumFractionDigits: fractionDigits,
    }).format(value)
  }

  return new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: code.toUpperCase(),
    minimumFractionDigits: fractionDigits,
    maximumFractionDigits: fractionDigits,
  }).format(value)
}
