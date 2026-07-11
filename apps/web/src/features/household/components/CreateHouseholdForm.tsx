/**
 * CreateHouseholdForm Component
 *
 * Issue #1370: Household feature — create-household flow (Task 4a)
 *
 * Shown when an eligible owner (Premium+ / household-tier subscriber) does
 * not yet belong to a household. Calls the generated `useCreateHousehold`
 * mutation directly (per the Task 4 brief) rather than through a wrapping
 * feature hook — unlike useHouseholdInvites/useHouseholdMembers, there is no
 * natural "list" query to co-locate a hook around for a one-shot create
 * action.
 *
 * Loading state is tracked via local `isSubmitting` state (set around the
 * `mutateAsync` call) rather than the mutation's own `isPending`, matching
 * the convention already used in InvitePanel.tsx.
 */

import { useState, type FormEvent } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import {
  useCreateHousehold,
  getGetMyHouseholdQueryKey,
  getGetHouseholdEntitlementQueryKey,
} from '@boostedbudget/api-client/endpoints/household/household'
import { Button } from '@/components/ui/button'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { getErrorDetail } from '../hooks/errorStatus'

export interface CreateHouseholdFormProps {
  className?: string
}

export function CreateHouseholdForm({ className }: CreateHouseholdFormProps = {}) {
  const queryClient = useQueryClient()
  const createHouseholdMutation = useCreateHousehold()

  const [name, setName] = useState('')
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [validationError, setValidationError] = useState<string | null>(null)
  const [submitError, setSubmitError] = useState<string | null>(null)

  const handleSubmit = async (event: FormEvent) => {
    event.preventDefault()
    if (isSubmitting) return

    const trimmed = name.trim()
    if (!trimmed) {
      setValidationError('Household name is required.')
      return
    }

    setValidationError(null)
    setSubmitError(null)
    setIsSubmitting(true)
    try {
      await createHouseholdMutation.mutateAsync({ data: { name: trimmed } })
      // Creating a household changes both the household record itself and
      // the caller's entitlement summary (isOwner/seatsUsed/seatsTotal come
      // from the separate entitlement query) — both must be invalidated or
      // useHousehold() can keep reporting stale ownership/seat data.
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: getGetMyHouseholdQueryKey() }),
        queryClient.invalidateQueries({ queryKey: getGetHouseholdEntitlementQueryKey() }),
      ])
      setName('')
    } catch (err) {
      setSubmitError(getErrorDetail(err) ?? 'Could not create household. Please try again.')
    } finally {
      setIsSubmitting(false)
    }
  }

  return (
    <Card className={className}>
      <CardHeader>
        <CardTitle>Create a household</CardTitle>
      </CardHeader>
      <CardContent>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-2">
            <label htmlFor="household-name" className="text-sm font-medium">
              Household name
            </label>
            <input
              id="household-name"
              type="text"
              value={name}
              onChange={(e) => {
                setName(e.target.value)
                if (validationError) setValidationError(null)
              }}
              placeholder="The Smiths"
              maxLength={255}
              disabled={isSubmitting}
              className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-sm transition-colors file:border-0 file:bg-transparent file:text-sm file:font-medium placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50"
            />
            <p className="text-xs text-muted-foreground">
              Invite family members to share budgets and split subscription costs.
            </p>
          </div>

          {validationError && (
            <p role="alert" className="text-sm text-destructive">
              {validationError}
            </p>
          )}
          {submitError && (
            <p role="alert" className="text-sm text-destructive">
              {submitError}
            </p>
          )}

          <Button type="submit" disabled={isSubmitting}>
            {isSubmitting ? 'Creating…' : 'Create household'}
          </Button>
        </form>
      </CardContent>
    </Card>
  )
}
