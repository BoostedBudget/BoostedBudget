import { useState } from 'react'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { Card } from '@/components/ui/card'
import type { CategorizationRule } from '../types'
import { ConfirmationDialog } from '@/components/ui/confirmation-dialog'
import { formatPFCEnum } from '@/shared/categories/formatPfc'
import { conditionLabel } from '../utils/conditionLabel'
import { formatConditionValue } from '../utils/formatConditionValue'
import { quarantineSentence } from '../utils/quarantineSentence'

export interface AutoCategorizationRuleCardProps {
  rule: CategorizationRule
  onToggle: (enabled: boolean) => Promise<void>
  onEdit: () => void
  onDelete: () => Promise<void>
}

export function AutoCategorizationRuleCard({ rule, onToggle, onEdit, onDelete }: AutoCategorizationRuleCardProps) {
  const [toggling, setToggling] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false)

  const handleToggle = async () => {
    setToggling(true)
    try {
      await onToggle(!rule.enabled)
    } finally {
      setToggling(false)
    }
  }

  const handleDeleteConfirm = async () => {
    setDeleting(true)
    try {
      await onDelete()
      setShowDeleteConfirm(false)
    } finally {
      setDeleting(false)
    }
  }

  return (
    <Card className={cn('mb-4 transition-shadow hover:shadow-md', !rule.enabled && 'opacity-70 bg-muted')}>
      <div className="mb-4">
        <div className="flex items-center justify-between gap-2 mb-1">
          <h3 className="m-0 text-lg font-semibold text-foreground">
            <span data-ph-mask>{rule.name}</span>
          </h3>
          <Badge
            variant={
              !rule.enabled
                ? 'secondary'
                : rule.quarantined
                  ? 'destructive'
                  : rule.quarantine_reason
                    ? 'warning'
                    : 'success'
            }
            className="uppercase"
          >
            {/* #1625 Task 10: RuleMatches (backend/internal/rules/engine.go) checks
                !rule.Enabled BEFORE quarantine, so a disabled rule never runs regardless of
                quarantine_reason -- "Disabled" must win over "Not running"/"Needs attention",
                not the reverse. Previously quarantine_reason was checked first, so a disabled
                malformed rule read "Needs attention" and implied it was still executing: two
                false statements about the same rule at once. */}
            {!rule.enabled
              ? 'Disabled'
              : rule.quarantined
                ? 'Not running'
                : rule.quarantine_reason
                  ? 'Needs attention'
                  : 'Active'}
          </Badge>
        </div>
        {rule.quarantine_reason && (
          // #1625: a rule that cannot be evaluated used to just stop matching, with the reason
          // visible only in a metric the user cannot see. The reason is a fixed backend
          // vocabulary and never user content, so it stays OUTSIDE data-ph-mask -- masking it
          // would hide the diagnostic without protecting anything.
          //
          // ⚠ THREE DIFFERENT STATES, and every conflation of them has shipped at least once.
          // The run status comes from `enabled` and `quarantined` ONLY -- see quarantineSentence
          // above for why reading it off the reason is false whenever the RULE_QUARANTINE_ENFORCE
          // kill switch is off. The reason is still SURFACED in all three states (a user deciding
          // whether to re-enable a rule needs to know it has an outstanding issue); it just never
          // decides what the card says about running.
          <p
            className={
              !rule.enabled
                ? 'mt-2 text-sm text-muted-foreground'
                : rule.quarantined
                  ? 'mt-2 text-sm text-destructive'
                  : 'mt-2 text-sm text-warning-foreground'
            }
            role="status"
          >
            {quarantineSentence(rule.enabled, Boolean(rule.quarantined), rule.quarantine_reason)}
          </p>
        )}
        {rule.description && (
          <p className="mt-2 text-sm text-muted-foreground"><span data-ph-mask>{rule.description}</span></p>
        )}
      </div>

      <div className="mb-4">
        {/* Conditions */}
        <div className="mb-3">
          <h4 className="m-0 mb-2 text-xs font-semibold text-muted-foreground uppercase tracking-wide">
            Conditions ({rule.condition_logic})
          </h4>
          <ul className="list-none m-0 p-0 flex flex-wrap gap-2">
            {rule.conditions.map((condition, index) => (
              <li key={index} className="flex items-center gap-1 bg-muted px-2 py-1 rounded text-[0.8125rem]">
                <span className="text-muted-foreground">
                  {conditionLabel(condition)}
                </span>
                <span className="text-foreground font-medium" data-ph-mask>
                  {formatConditionValue(condition)}
                </span>
              </li>
            ))}
          </ul>
        </div>

        {/* Actions */}
        <div className="mb-3">
          <h4 className="m-0 mb-2 text-xs font-semibold text-muted-foreground uppercase tracking-wide">Actions</h4>
          <div className="flex flex-col gap-2">
            {rule.target_kind === 'pfc' && rule.target_pfc_primary && (
              <div className="flex items-center gap-2 text-sm">
                <span className="text-muted-foreground">Set category:</span>
                <span className="text-foreground font-medium flex flex-wrap gap-1">
                  {rule.target_pfc_detailed_display
                    ?? rule.target_pfc_primary_display
                    ?? formatPFCEnum(rule.target_pfc_detailed ?? rule.target_pfc_primary ?? '')}
                </span>
              </div>
            )}
            {rule.target_kind === 'custom' && rule.target_custom_category_id && (
              <div className="flex items-center gap-2 text-sm">
                <span className="text-muted-foreground">Set custom category:</span>
                <span className="text-foreground font-medium" data-ph-mask>{rule.target_custom_category_display ?? rule.target_custom_category_id}</span>
              </div>
            )}
            {rule.target_kind === 'clear' && (
              <div className="flex items-center gap-2 text-sm">
                <span className="text-muted-foreground">Action:</span>
                <span className="text-foreground font-medium">Clear category override</span>
              </div>
            )}
            {rule.target_tags && rule.target_tags.length > 0 && (
              <div className="flex items-center gap-2 text-sm">
                <span className="text-muted-foreground">
                  {rule.tag_behavior === 'replace' ? 'Replace tags:' : 'Add tags:'}
                </span>
                <span className="text-foreground font-medium flex flex-wrap gap-1">
                  {rule.target_tags.map((tag) => (
                    <span key={tag} data-ph-mask className="bg-info/10 text-info px-1.5 py-0.5 rounded text-xs font-medium">{tag}</span>
                  ))}
                </span>
              </div>
            )}
          </div>
        </div>

        {/* Stats */}
        <div className="flex flex-wrap gap-4 pt-3 border-t border-border">
          <span className="text-xs text-muted-foreground">
            Matched: {rule.match_count}
          </span>
          <span className="text-xs text-muted-foreground">
            Overridden: {rule.override_count}
          </span>
          {rule.last_matched_at && (
            <span className="text-xs text-muted-foreground">
              Last match: {new Date(rule.last_matched_at).toLocaleDateString()}
            </span>
          )}
        </div>
      </div>

      <div className="flex items-center justify-between pt-4 border-t border-border">
        <div className="text-xs text-muted-foreground">
          Priority: {rule.priority}
        </div>
        <div className="flex gap-2">
          <button
            type="button"
            className="px-3 py-1.5 text-[0.8125rem] font-medium bg-background border border-border text-foreground rounded cursor-pointer hover:bg-muted transition-colors disabled:opacity-60 disabled:cursor-not-allowed"
            onClick={handleToggle}
            disabled={toggling}
          >
            {toggling ? 'Saving...' : rule.enabled ? 'Disable' : 'Enable'}
          </button>
          <button
            type="button"
            className="px-3 py-1.5 text-[0.8125rem] font-medium bg-primary text-primary-foreground rounded cursor-pointer hover:bg-primary/90 transition-colors"
            onClick={onEdit}
          >
            Edit
          </button>
          <button
            type="button"
            className="px-3 py-1.5 text-[0.8125rem] font-medium text-destructive border border-destructive rounded cursor-pointer hover:bg-destructive hover:text-destructive-foreground transition-colors disabled:opacity-60 disabled:cursor-not-allowed"
            onClick={() => setShowDeleteConfirm(true)}
            disabled={deleting}
          >
            Delete
          </button>
        </div>
      </div>

      {/* Delete confirmation dialog — Issue #960 */}
      <ConfirmationDialog
        open={showDeleteConfirm}
        onOpenChange={(open) => !open && !deleting && setShowDeleteConfirm(false)}
        onConfirm={handleDeleteConfirm}
        title="Delete Rule"
        description={<>Are you sure you want to delete &ldquo;<span data-ph-mask>{rule.name}</span>&rdquo;?</>}
        variant="destructive"
        loading={deleting}
      />
    </Card>
  )
}
