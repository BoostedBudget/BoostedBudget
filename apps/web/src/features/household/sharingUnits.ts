/**
 * The unit a household sharing control can actually address.
 *
 * Issue #1569 T10.3 item C, extracted from `ConsentPreview` in T10-FIX item 5 so the post-Plaid
 * `ShareNewAccountsDialog` cannot drift from it again. That dialog kept rendering a switch per
 * ACCOUNT while calling `setVisibility(account.user_account_id, …)`, so two maskless siblings gave
 * the user two controls over one backend value and made two writes for it — the exact defect
 * ConsentPreview had already been fixed for. One module, one grouping decision, both surfaces.
 *
 * `user_account_id` is the sharing key, not `account.id`: `hidden_account_ids` holds
 * user_account_ids, and `AcceptInvite`'s IDOR check validates them against
 * `ListActiveUserAccountFingerprints`. Two MASKLESS accounts at one institution are forced to
 * share one (migration 000100), which is why a unit can hold more than one account.
 */
import type { Account } from '@/features/accounts/types'

export interface SharingUnit {
  key: string
  /**
   * `undefined` when the accounts row has no `user_account_id`. Such an account cannot be shared
   * AT ALL — `AcceptInvite` builds the shareable set from `user_accounts`, so it can never enter
   * `household_account_shares` — and both surfaces must say so rather than label it "Shared"
   * (#1569 T10-FIX item 8).
   */
  shareId?: string
  accounts: Account[]
}

/**
 * Groups accounts into sharing units, preserving first-appearance order.
 *
 * Accounts with NO `user_account_id` are each their own unit and are never grouped with one
 * another: they share the ABSENCE of a sharing id, not a shared one, and merging them would fold
 * unrelated accounts into one card and hide all but the first.
 */
export function groupAccountsBySharingUnit(accounts: Account[]): SharingUnit[] {
  const units: SharingUnit[] = []
  const byShareId = new Map<string, SharingUnit>()

  for (const account of accounts) {
    const shareId = account.user_account_id || undefined
    if (!shareId) {
      units.push({ key: `account-${account.id}`, accounts: [account] })
      continue
    }
    const existing = byShareId.get(shareId)
    if (existing) {
      existing.accounts.push(account)
      continue
    }
    const unit: SharingUnit = { key: `share-${shareId}`, shareId, accounts: [account] }
    byShareId.set(shareId, unit)
    units.push(unit)
  }

  return units
}

/** The name the user recognises this account by, in the order the rest of the app uses. */
export function sharingUnitDisplayName(account: Account): string {
  return account.nickname || account.official_name || account.name
}
