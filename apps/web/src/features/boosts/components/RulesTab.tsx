// Issue #1267: Boosts page — thin wrapper rendering RulesPage as a tab
// Suppresses the RulesPage internal <h1> (which says "Automation Rules") to avoid
// a duplicate heading when the parent BoostsPage already says "Boosts".
// R8-1c: Passes showTypeSegment + ruleTypeFilter so the tab defaults to
// transfer-generating rule types (transfer_suggestion, savings_goal) while
// still letting the user browse "All rules" without orphaning other types.
import { RulesPage } from '@/features/rules/components/RulesPage'
import { TRANSFER_GENERATING_RULE_TYPES } from '../constants'

export function RulesTab() {
  return (
    <div className="[&_h1]:hidden">
      <RulesPage
        showTypeSegment
        ruleTypeFilter={TRANSFER_GENERATING_RULE_TYPES}
      />
    </div>
  )
}
