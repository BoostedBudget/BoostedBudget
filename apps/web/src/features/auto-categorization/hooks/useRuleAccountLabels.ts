/**
 * useRuleAccountLabels — Issue #1626 (Task K).
 *
 * The one account id → name index every rule-condition surface renders through. Both surfaces
 * that show a condition take their names from HERE (the rules list via
 * `AutoCategorizationPage` → `AutoCategorizationRuleCard`, and the retained-history panel
 * directly), so the live rule and the frozen `definition_snapshot` cannot disagree about what an
 * account is called — the guarantee `formatConditionValue`'s extraction exists to hold.
 *
 * ⚠ `useAllAccounts`, NOT `useActiveAccounts`. This is a display/lookup path, and that hook's
 * docblock is explicit that a lookup built from the active-only list renders a retained
 * (disconnected, replaced, expired) account as "Unknown account". A categorization rule outlives
 * the account it names — the rule row and its execution history are both retained — so narrowing
 * this to active accounts would put the neutral placeholder in front of a name we hold.
 * The account SELECTION path (the editor's dropdown) correctly stays on the active list.
 */

import { useMemo } from 'react'
import { useAllAccounts } from '@/features/accounts/hooks'
import { buildAccountLabelIndex } from '../utils/formatConditionValue'

export function useRuleAccountLabels(): Readonly<Record<string, string>> {
  const { accounts } = useAllAccounts()
  return useMemo(() => buildAccountLabelIndex(accounts ?? []), [accounts])
}
