/**
 * Minimum shape required by getMerchantDisplayName.
 * Structurally compatible with both Transaction and BankingTransaction.
 */
export interface MerchantNameFields {
  name: string
  merchant_name?: string | null
  effective_merchant_name?: string | null
}

/**
 * Returns the best display name for a transaction's merchant:
 *   effective_merchant_name (user override / normalized) → merchant_name → name
 *
 * Centralises the fallback so a precedence change only needs one edit.
 *
 * NOTE: `MerchantNameFields` is structurally compatible with any object that has
 * a `name: string` field (including InvestmentActivity). Callers that work with
 * `FeedRow` (BankingTransaction | InvestmentActivity) MUST narrow to
 * BankingTransaction before calling this function — investment rows have their
 * own display path and must not reach this helper.
 */
export function getMerchantDisplayName(tx: MerchantNameFields): string {
  return tx.effective_merchant_name || tx.merchant_name || tx.name
}
