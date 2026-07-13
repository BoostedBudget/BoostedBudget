/**
 * HouseholdCard Component
 *
 * Issue #1370: Household feature — household info card (Task 4a)
 *
 * The top-level card shown for an existing household: name, seat usage, and
 * the current user's own role. Owners get an inline rename affordance (edit
 * icon toggles the name into an editable input, saved via the generated
 * `useUpdateHouseholdName` mutation called directly — same rationale as
 * CreateHouseholdForm, no wrapping feature hook needed for a one-shot
 * rename). Non-owners see the name as plain read-only text with NO edit
 * affordance rendered at all (not merely disabled).
 *
 * Reads `useHousehold()` directly (household, isOwner, seatsUsed,
 * seatsTotal) rather than accepting props — consistent with how
 * InvitePanel.tsx composes its own feature hook, and keeps this component
 * self-contained for the composing HouseholdTab (Task 4b): render
 * `<HouseholdCard />` with no wiring required.
 */

import { useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { Pencil, Check, X } from 'lucide-react'
import {
  useUpdateHouseholdName,
  getGetMyHouseholdQueryKey,
} from '@boostedbudget/api-client/endpoints/household/household'
import { Card, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics'
import { RoleBadge } from './RoleBadge'
import { SeatUsageBadge } from './SeatUsageBadge'
import { useHousehold } from '../hooks/useHousehold'
import { getErrorDetail } from '../hooks/errorStatus'

export interface HouseholdCardProps {
  className?: string
}

export function HouseholdCard({ className }: HouseholdCardProps = {}) {
  const queryClient = useQueryClient()
  const { household, isOwner, seatsUsed, seatsTotal } = useHousehold()
  const updateHouseholdNameMutation = useUpdateHouseholdName()
  const trackEvent = useTrackEvent()

  const [isEditing, setIsEditing] = useState(false)
  const [draftName, setDraftName] = useState('')
  const [isSaving, setIsSaving] = useState(false)
  const [validationError, setValidationError] = useState<string | null>(null)
  const [submitError, setSubmitError] = useState<string | null>(null)

  if (!household) return null

  const startEditing = () => {
    setDraftName(household.name)
    setValidationError(null)
    setSubmitError(null)
    setIsEditing(true)
  }

  const cancelEditing = () => {
    setIsEditing(false)
    setValidationError(null)
    setSubmitError(null)
  }

  const handleSave = async () => {
    if (isSaving) return

    const trimmed = draftName.trim()
    if (!trimmed) {
      setValidationError('Household name is required.')
      return
    }

    setValidationError(null)
    setSubmitError(null)
    setIsSaving(true)
    try {
      await updateHouseholdNameMutation.mutateAsync({ data: { name: trimmed } })
      await queryClient.invalidateQueries({ queryKey: getGetMyHouseholdQueryKey() })
      // Fire only once the rename has actually persisted, never on failure.
      // No household name in the payload — see events.ts's PII note.
      trackEvent(ANALYTICS_EVENTS.HOUSEHOLD_RENAMED)
      setIsEditing(false)
    } catch (err) {
      setSubmitError(getErrorDetail(err) ?? 'Could not rename household. Please try again.')
    } finally {
      setIsSaving(false)
    }
  }

  return (
    <Card className={className}>
      <CardHeader className="flex flex-row items-start justify-between gap-4">
        {isEditing ? (
          <div className="flex-1 space-y-2">
            <label htmlFor="household-name-edit" className="sr-only">
              Household name
            </label>
            <div className="flex items-center gap-2">
              <input
                id="household-name-edit"
                type="text"
                value={draftName}
                onChange={(e) => {
                  setDraftName(e.target.value)
                  if (validationError) setValidationError(null)
                }}
                maxLength={255}
                disabled={isSaving}
                autoFocus
                className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-sm transition-colors placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50"
              />
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                aria-label="Save household name"
                disabled={isSaving}
                onClick={handleSave}
              >
                <Check className="size-4" />
              </Button>
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                aria-label="Cancel"
                disabled={isSaving}
                onClick={cancelEditing}
              >
                <X className="size-4" />
              </Button>
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
          </div>
        ) : (
          <div className="flex flex-1 items-center gap-2">
            <CardTitle data-ph-mask>{household.name}</CardTitle>
            {isOwner && (
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                aria-label="Rename household"
                onClick={startEditing}
              >
                <Pencil className="size-4" />
              </Button>
            )}
          </div>
        )}

        <div className="flex shrink-0 items-center gap-2">
          <RoleBadge role={isOwner ? 'owner' : 'member'} />
          <SeatUsageBadge used={seatsUsed} total={seatsTotal} />
        </div>
      </CardHeader>
    </Card>
  )
}
