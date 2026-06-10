// Issue #1267 Gap W5: /boosts/:id — rule detail + recent execution history (last 5)
import { useParams, Link } from 'react-router-dom'
import { ArrowLeft, AlertCircle } from 'lucide-react'
import { useGetBoostByID } from '@boostedbudget/api-client/endpoints/boosts/boosts'
import { EmptyState } from '@/components/ui/empty-state'
import { BOOST_TYPE_LABELS } from '../boostConstants'
import { BoostExecutionHistory } from './BoostExecutionHistory'
import type { BoostType } from '../types'

export function BoostDetailPage() {
  const { id } = useParams<{ id: string }>()

  const { data: rule, isLoading } = useGetBoostByID(id ?? '')

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
      <div className="flex flex-col items-center justify-center p-6">
        <EmptyState
          icon={<AlertCircle />}
          title="Boost not found"
          description="We couldn't find a boost with this ID."
          action={
            <Link
              to="/boosts?tab=boosts"
              className="inline-flex items-center gap-1.5 text-sm text-primary hover:underline"
            >
              <ArrowLeft className="h-4 w-4" />
              Back to Boosts
            </Link>
          }
        />
      </div>
    )
  }

  const typeLabel = BOOST_TYPE_LABELS[rule.boost_type as BoostType] ?? rule.boost_type

  return (
    <div className="flex flex-col gap-6 p-6">
      <div className="flex items-center gap-3">
        <Link
          to="/boosts?tab=boosts"
          className="inline-flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground"
        >
          <ArrowLeft className="h-4 w-4" />
          Back to Boosts
        </Link>
      </div>

      <div>
        <h1 className="text-2xl font-semibold text-foreground"><span data-ph-mask>{rule.name}</span></h1>
        <p className="mt-1 text-sm text-muted-foreground">{typeLabel}</p>
        {rule.description && (
          <p className="mt-2 text-sm text-muted-foreground"><span data-ph-mask>{rule.description}</span></p>
        )}
      </div>

      <section>
        <BoostExecutionHistory boostId={id ?? ''} limit={5} />
      </section>
    </div>
  )
}
