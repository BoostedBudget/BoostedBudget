/**
 * RulePreviewPanel — the dry-run result for a candidate rule (Issue #1625).
 *
 * The reporting session that opened #1625 ended with speculative writes fired at a live
 * financial account, because there was no way to find out what a rule would do before saving
 * it. This is that way: it calls POST /auto-categorization/preview with the rule currently in
 * the editor and shows which recent transactions it would match, saving nothing.
 *
 * Preview validates identically to create, so a rule that previews cleanly saves cleanly — the
 * panel doubles as a validation oracle and surfaces the 422 here rather than on save.
 *
 * ⚠ EVERY match cell except the count is user financial data — a raw description, a merchant,
 * an amount. All of them carry `data-ph-mask` so session replay keeps the SHAPE of the result
 * without keeping its contents. The column headers and the field/operator vocabulary are fixed
 * strings and deliberately stay outside the mask.
 */

import { useState } from 'react'
import { usePreviewRule } from '@boostedbudget/api-client/endpoints/auto-categorization/auto-categorization'
import type { RulePreviewResult } from '@boostedbudget/api-client/models/rulePreviewResult'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics/events'
import type { RuleCondition, RuleTargetKind } from '../types'

interface RulePreviewPanelProps {
  name: string
  conditions: RuleCondition[]
  conditionLogic: string
  targetKind?: RuleTargetKind
  targetPfcPrimary?: string
  targetPfcDetailed?: string
  targetCustomCategoryId?: string
  targetTags?: string[]
  disabled?: boolean
}

function formatAmount(amount: number): string {
  return new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD' }).format(amount)
}

export function RulePreviewPanel({
  name,
  conditions,
  conditionLogic,
  targetKind,
  targetPfcPrimary,
  targetPfcDetailed,
  targetCustomCategoryId,
  targetTags,
  disabled = false,
}: RulePreviewPanelProps) {
  const [result, setResult] = useState<RulePreviewResult | null>(null)
  const [error, setError] = useState<string | null>(null)
  const trackEvent = useTrackEvent()
  const previewMutation = usePreviewRule()

  const handlePreview = async () => {
    setError(null)
    try {
      const data = await previewMutation.mutateAsync({
        data: {
          name: name.trim() || 'Untitled rule',
          conditions,
          condition_logic: conditionLogic,
          target_kind: targetKind,
          target_pfc_primary: targetPfcPrimary,
          target_pfc_detailed: targetPfcDetailed,
          target_custom_category_id: targetCustomCategoryId,
          target_tags: targetTags,
        },
      } as never)
      const preview = data as unknown as RulePreviewResult
      setResult(preview)
      // Counts and the fixed vocabulary only — never a condition value.
      trackEvent(ANALYTICS_EVENTS.RULE_PREVIEWED, {
        condition_count: conditions.length,
        match_count: preview.match_count ?? 0,
        scanned_count: preview.scanned_count ?? 0,
        fields_used: conditions.map((c) => c.field ?? c.type).join(','),
      })
    } catch (err) {
      // Preview shares create's validation, so this is the same message a save would produce.
      const detail =
        (err as { response?: { data?: { detail?: string } } })?.response?.data?.detail ??
        (err as Error)?.message ??
        'Preview failed'
      setError(detail)
      setResult(null)
    }
  }

  const matches = result?.matches ?? []

  return (
    <div className="rounded-lg border border-border p-3">
      <div className="flex items-center justify-between gap-2">
        <p className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
          Test this rule
        </p>
        <button
          type="button"
          className="px-3 py-1.5 text-xs border border-input rounded bg-card text-foreground cursor-pointer hover:bg-muted disabled:cursor-not-allowed disabled:opacity-60"
          onClick={handlePreview}
          disabled={disabled || previewMutation.isPending || conditions.length === 0}
        >
          {previewMutation.isPending ? 'Checking…' : 'Preview matches'}
        </button>
      </div>

      {error && (
        // ⚠ MASKED. Codex review: a validation error is not a fixed string — the mapper returns
        // the domain error verbatim, and several of those ECHO THE USER'S VALUE (a malformed
        // date is quoted back, and an unknown-field message names what was sent). So this line
        // can carry user-typed financial detail and must be masked like any other value.
        <p role="alert" className="mt-2 text-sm text-destructive" data-ph-mask>
          {error}
        </p>
      )}

      {result && !error && (
        <div className="mt-3">
          <p className="text-sm text-muted-foreground">
            Matched <strong className="text-foreground">{result.match_count}</strong> of the last{' '}
            {result.scanned_count} transactions.
          </p>

          {matches.length === 0 ? (
            <p className="mt-2 text-sm text-muted-foreground">
              Nothing matched. The rule will still apply to future transactions that do.
            </p>
          ) : (
            <div className="mt-2 overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="text-left text-xs uppercase tracking-wide text-muted-foreground">
                    <th className="py-1 pr-3 font-medium">Date</th>
                    <th className="py-1 pr-3 font-medium">Description</th>
                    <th className="py-1 pr-3 font-medium">Amount</th>
                    <th className="py-1 font-medium">Category now</th>
                  </tr>
                </thead>
                <tbody>
                  {matches.map((m) => (
                    <tr key={m.transaction_id} className="border-t border-border">
                      <td className="py-1 pr-3 whitespace-nowrap" data-ph-mask>
                        {String(m.date).slice(0, 10)}
                      </td>
                      <td className="py-1 pr-3" data-ph-mask>
                        {m.merchant_name || m.name}
                      </td>
                      <td className="py-1 pr-3 whitespace-nowrap" data-ph-mask>
                        {formatAmount(m.amount)}
                      </td>
                      <td className="py-1" data-ph-mask>
                        {m.current_category}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
