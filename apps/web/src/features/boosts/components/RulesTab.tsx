// Issue #1267: Boosts page — thin wrapper rendering BoostsManagePage as a tab
// Suppresses the BoostsManagePage internal <h1> to avoid
// a duplicate heading when the parent BoostsPage already says "Boosts".
// R8-1c: Passes showTypeSegment + ruleTypeFilter so the tab defaults to
// transfer-generating boost types (transfer_suggestion, savings_goal) while
// still letting the user browse "All boosts" without orphaning other types.
import { BoostsManagePage } from './BoostsManagePage'
import { TRANSFER_GENERATING_RULE_TYPES } from '../constants'

export function RulesTab() {
  return (
    <div className="[&_h1]:hidden">
      <BoostsManagePage
        showTypeSegment
        ruleTypeFilter={TRANSFER_GENERATING_RULE_TYPES}
      />
    </div>
  )
}
