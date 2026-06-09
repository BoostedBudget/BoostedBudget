// Issue #1267: Boosts page — thin wrapper rendering RulesPage as a tab
// Suppresses the RulesPage internal <h1> (which says "Automation Rules") to avoid
// a duplicate heading when the parent BoostsPage already says "Boosts".
import { RulesPage } from '@/features/rules/components/RulesPage'

export function RulesTab() {
  return (
    <div className="[&_h1]:hidden">
      <RulesPage />
    </div>
  )
}
