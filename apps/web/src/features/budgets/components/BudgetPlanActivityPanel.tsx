import { useState } from 'react'
import type { BudgetPlanVsActualResponse } from '@boostedbudget/api-client/models'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from '@/components/ui/dialog'
import { GoalContributionsSheet } from '../../savings/components/GoalContributionsSheet'
import { BudgetPlanFlowChart, type BudgetPlanFlowData } from './BudgetPlanFlowChart'
import { BudgetTransactionList } from './transaction-link/BudgetTransactionList'

interface Props { data: BudgetPlanVsActualResponse; onRefresh: () => void }

export default function BudgetPlanActivityPanel({ data, onRefresh }: Props) {
  const [privacyMode, setPrivacyMode] = useState(false)
  const [selected, setSelected] = useState<BudgetPlanFlowData['lines'][number] | null>(null)
  const currentSelection = data.eligibility === 'eligible' && !privacyMode && selected && data.lines.find((line) => line.id === selected.id && (line.kind !== 'savings' || !line.reasons.some((reason) => reason === 'mixed_currency' || reason === 'unknown_currency')))
  return (
    <div className="space-y-3">
      <Button variant="outline" aria-pressed={privacyMode} onClick={() => { setPrivacyMode(!privacyMode); setSelected(null) }}>{privacyMode ? 'Show activity amounts' : 'Hide activity amounts'}</Button>
      <BudgetPlanFlowChart data={data} privacyMode={privacyMode} onSelectLine={setSelected} />
      <Dialog open={!!currentSelection && currentSelection.kind !== 'savings'} onOpenChange={(open) => { if (!open) setSelected(null) }}>
        <DialogContent className="max-w-2xl max-h-[85vh] overflow-y-auto">
          <DialogHeader><DialogTitle>Allocation activity</DialogTitle><DialogDescription>Posted spending from {data.period_start} through {data.actual_through}. Totals exclude the portion allocated to savings.</DialogDescription></DialogHeader>
          {currentSelection && currentSelection.kind !== 'savings' && <BudgetTransactionList budgetId={data.budget_id} categoryId={currentSelection.budget_category_id ?? 'unbudgeted'} currencyCode={data.currency_code} snapshotVersion={data.as_of} />}
        </DialogContent>
      </Dialog>
      {currentSelection && currentSelection.kind === 'savings' && currentSelection.savings_goal_id && (
        <GoalContributionsSheet goalId={currentSelection.savings_goal_id} goalName={currentSelection.label} currencyCode={data.currency_code} contextNote="Full savings history includes all dates, proposals, carried balances, and closed periods. It does not reconcile directly to this budget period’s contribution total." open onOpenChange={(open) => { if (!open) setSelected(null) }} onContributionChanged={onRefresh} />
      )}
    </div>
  )
}
