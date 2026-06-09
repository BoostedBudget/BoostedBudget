// Issue #1267 Gap W5: /boosts/:id — rule detail + recent execution history (last 5)
import { useParams, Link } from 'react-router-dom'
import { ArrowLeft } from 'lucide-react'
import { useGetAutomationRuleByID } from '@boostedbudget/api-client/endpoints/automation-rules/automation-rules'
import { RULE_TYPE_LABELS } from '@/features/rules/constants'
import { RuleExecutionHistory } from '@/features/rules/components/RuleExecutionHistory'
import type { RuleType } from '@/features/rules/types'

export function RuleDetailPage() {
  const { id } = useParams<{ id: string }>()

  const { data: rule, isLoading } = useGetAutomationRuleByID(id ?? '')

  if (isLoading) {
    return (
      <div className="animate-pulse space-y-4 p-6">
        <div className="h-8 w-48 rounded bg-muted" />
        <div className="h-4 w-full rounded bg-muted" />
        <div className="h-4 w-3/4 rounded bg-muted" />
      </div>
    )
  }

  if (!rule) {
    return (
      <div className="p-6 text-center text-muted-foreground">
        <p className="text-base font-medium">Rule not found</p>
        <p className="mt-1 text-sm">We couldn't find a rule with this ID.</p>
        <Link
          to="/boosts?tab=rules"
          className="mt-4 inline-flex items-center gap-1.5 text-sm text-primary hover:underline"
        >
          <ArrowLeft className="h-4 w-4" />
          Back to Rules
        </Link>
      </div>
    )
  }

  const typeLabel = RULE_TYPE_LABELS[rule.rule_type as RuleType] ?? rule.rule_type

  return (
    <div className="flex flex-col gap-6 p-6">
      <div className="flex items-center gap-3">
        <Link
          to="/boosts?tab=rules"
          className="inline-flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground"
        >
          <ArrowLeft className="h-4 w-4" />
          Back to Rules
        </Link>
      </div>

      <div>
        <h1 className="text-2xl font-semibold text-foreground">{rule.name}</h1>
        <p className="mt-1 text-sm text-muted-foreground">{typeLabel}</p>
        {rule.description && (
          <p className="mt-2 text-sm text-muted-foreground">{rule.description}</p>
        )}
      </div>

      <section>
        <h2 className="mb-3 text-lg font-medium text-foreground">Recent Executions</h2>
        <RuleExecutionHistory ruleId={id ?? ''} limit={5} />
      </section>
    </div>
  )
}
