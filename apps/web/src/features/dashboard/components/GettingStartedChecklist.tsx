import { Link } from 'react-router'
import { CheckCircle2, Circle, X, PartyPopper, ChevronRight } from 'lucide-react'
import { Button } from '@/components/ui/button'
import type { GettingStartedStep } from '../hooks/useGettingStarted'

interface GettingStartedChecklistProps {
  steps: GettingStartedStep[]
  completedCount: number
  totalSteps: number
  isCelebrating: boolean
  onDismiss: () => void
}

export function GettingStartedChecklist({
  steps,
  completedCount,
  totalSteps,
  isCelebrating,
  onDismiss,
}: GettingStartedChecklistProps) {
  const progressPercent = Math.round((completedCount / totalSteps) * 100)

  return (
    <div className="rounded-xl border border-blue-200 bg-white shadow-sm dark:border-blue-900 dark:bg-gray-800">
      <div className="flex items-start justify-between px-5 pt-4 pb-2">
        <div className="flex-1 min-w-0">
          <h2 className="text-base font-semibold text-gray-900 dark:text-white">
            Getting Started
          </h2>
          <p className="mt-0.5 text-sm text-gray-500 dark:text-gray-400">
            {completedCount} of {totalSteps} complete
          </p>
          <div
            role="progressbar"
            aria-valuenow={completedCount}
            aria-valuemax={totalSteps}
            className="mt-2 h-1.5 w-full overflow-hidden rounded-full bg-gray-100 dark:bg-gray-700"
          >
            <div
              className="h-full rounded-full bg-blue-500 transition-all duration-500"
              style={{ width: `${progressPercent}%` }}
            />
          </div>
        </div>
        <Button
          variant="outline"
          size="sm"
          aria-label="Dismiss getting started checklist"
          onClick={onDismiss}
          className="ml-3 h-7 w-7 shrink-0 rounded-full p-0 text-gray-400 hover:text-gray-600 dark:text-gray-500 dark:hover:text-gray-300"
        >
          <X className="h-3.5 w-3.5" aria-hidden="true" />
        </Button>
      </div>

      <div className="px-5 pb-4">
        {isCelebrating ? (
          <div role="status" aria-live="polite" className="flex flex-col items-center justify-center py-4 text-center">
            <PartyPopper className="mb-2 h-8 w-8 text-emerald-500" aria-hidden="true" />
            <p className="text-sm font-medium text-gray-900 dark:text-white">
              You're all set!
            </p>
            <p className="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
              Your financial workspace is ready to go.
            </p>
          </div>
        ) : (
          <ul role="list" className="mt-1 divide-y divide-gray-100 dark:divide-gray-700">
            {steps.map((step) => (
              <li
                key={step.id}
                data-testid={`step-${step.id}`}
                data-complete={String(step.complete)}
                className="flex items-center gap-3 py-2.5"
              >
                {step.complete ? (
                  <CheckCircle2
                    className="h-4.5 w-4.5 shrink-0 text-emerald-500"
                    aria-label="Completed"
                  />
                ) : (
                  <Circle
                    className="h-4.5 w-4.5 shrink-0 text-gray-300 dark:text-gray-600"
                    aria-label="Incomplete"
                  />
                )}

                <div className="flex-1 min-w-0">
                  {step.complete ? (
                    <span className="text-sm text-gray-500 line-through dark:text-gray-400">
                      {step.label}
                    </span>
                  ) : (
                    <Link
                      to={step.href}
                      className="group flex items-center justify-between gap-1 text-sm font-medium text-gray-800 hover:text-blue-600 dark:text-gray-200 dark:hover:text-blue-400"
                    >
                      <span>{step.label}</span>
                      <ChevronRight
                        className="h-3.5 w-3.5 shrink-0 text-gray-400 transition-transform group-hover:translate-x-0.5 dark:text-gray-500"
                        aria-hidden="true"
                      />
                    </Link>
                  )}
                </div>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  )
}
