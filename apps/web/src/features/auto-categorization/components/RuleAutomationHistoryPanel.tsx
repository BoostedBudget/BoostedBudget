/**
 * RuleAutomationHistoryPanel — Issue #1569 Task 9.4.
 *
 * `rule_execution_history` is the automation audit trail: one row each time a categorization rule
 * actually CHANGED a transaction's category or tags. Deleting a rule is a HARD
 * `DELETE FROM categorization_rules` (the table has no `deleted_at`), and
 * `fk_rule_execution_history_rule_id` is ON DELETE SET NULL — so the row survives the rule,
 * carrying `rule_name` and `definition_snapshot` frozen at execution time precisely so an orphaned
 * row can still answer "what did this automation do, and why".
 *
 * `GET /api/v1/rule-executions` was built to return those rows, and its own justification comment
 * called it a mirror of `GET /api/v1/boost-executions` — which gained a UI in round 7 for exactly
 * this defect. `useGetUserRuleExecutions` had ZERO consumers in `apps/web/src`. Retention nothing
 * can reach is worth nothing; this panel is the seat.
 *
 * WHAT TRANSFERS FROM BoostAutomationHistoryPanel, AND WHAT DOES NOT.
 *
 * Transfers: the collapsed-by-default disclosure with `aria-controls`, all four states named
 * (loading / error / empty / data), pagination over the server's honest `total`, the view event,
 * and per-ELEMENT masking inside mixed cells.
 *
 * Does NOT transfer, and this is the substantive difference:
 *
 *  1. `BoostExecutionTable` is boost-specific — time / status / duration / trigger, a boost status
 *     badge map, `getTriggerType`. A rule execution carries `rule_name`, `definition_snapshot`,
 *     `action_type` and `result` instead. The columns here are built for THIS data on the shared
 *     `DataTable`; nothing is reused by force.
 *
 *  2. ⚠ THERE IS NO DEGRADED MODE HERE, AND ITS ABSENCE IS A PROPERTY OF THE DATA, NOT AN OMISSION.
 *     The boost panel has to fetch the live boost list and diff it, because a tombstoned boost
 *     KEEPS its `boost_id` — so "is this boost deleted" is a client-side inference that can fail,
 *     and a failure has to be announced (`markersUnavailable`). Rules are hard-deleted, so
 *     `rule_id === null` IS the server's own statement that the rule is gone. One field, one
 *     query, nothing to diff, nothing that can silently fail open. Adding a second request here
 *     would manufacture the very failure mode the boost panel had to write a banner for.
 *
 * WHAT THE SNAPSHOT SHOWS. `definition_snapshot` is a JSON blob (rule_id, name, enabled, priority,
 * condition_logic, conditions, tag_behavior, source, captured_at, captured_by, target_*). Dumping
 * it raw would be unreadable AND would spray user-typed text through the DOM with no mask. Only
 * the CONDITIONS are rendered — they are the entire answer to "why did this fire", the one
 * question an orphaned row exists to answer — through the same `formatConditionValue` the live
 * rule card uses, so "what it did then" and "what it does now" read identically. The rest of the
 * blob is deliberately not shown: it is either already a column on the row, or metadata about the
 * capture rather than about what happened.
 *
 * ⚠ PII. `DataTable` masks a `<td>` only when the column id is in `maskedColumnIds`, which masks
 * the WHOLE cell — and both cells that carry PII here are MIXED. The rule cell pairs a user-typed
 * name with the "Deleted rule" marker; the why cell pairs a fixed condition LABEL with a user-typed
 * VALUE. Masking those cells wholesale would erase the signal that makes the surface readable in
 * replay, so the mask goes on the inner element, as `BoostExecutionTable` and
 * `PlaidOrphanedItemsPage` both do. No `aria-label` on this surface carries a value: attributes are
 * recorded verbatim by session replay and no mask covers them.
 */

import { useId, useMemo, useState } from 'react'
import { ChevronDown, History } from 'lucide-react'
import { useGetUserRuleExecutions } from '@boostedbudget/api-client/endpoints/auto-categorization/auto-categorization'
import type { RuleExecution } from '@boostedbudget/api-client/models'
import { Badge } from '@/components/ui/badge'
import type { BadgeProps } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { EmptyState } from '@/components/ui/empty-state'
import { DataTable, TablePagination, useTablePreferences } from '@/shared/components/DataTable'
import type { ColumnDef } from '@/shared/components/DataTable'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics'
import { CONDITION_TYPE_LABELS, type ConditionType } from '../types'
import { formatConditionValue } from '../utils/formatConditionValue'
import { useRuleAccountLabels } from '../hooks/useRuleAccountLabels'

const PAGE_SIZE = 20
const TABLE_ID = 'rule-user-executions'
const COLUMN_IDS = ['rule', 'time', 'change', 'result', 'why']

/** How many conditions are spelled out before the cell says "+N more". */
const MAX_CONDITIONS_SHOWN = 3

/**
 * `action_type` is pinned by the live `valid_action_type` CHECK to exactly these three. An
 * unrecognised value falls through to the raw string rather than to a blank cell — a row that
 * cannot be labelled is still a row the user is entitled to see.
 */
const ACTION_LABELS: Record<string, string> = {
  category: 'Category',
  tags: 'Tags',
  both: 'Category and tags',
}

/** `result` is pinned by the live `valid_execution_result` CHECK to exactly these two. */
const RESULT_LABELS: Record<string, string> = {
  applied: 'Applied',
  skipped_manual: 'Skipped (manual)',
}

const RESULT_BADGE_VARIANT: Record<string, BadgeProps['variant']> = {
  applied: 'success',
  skipped_manual: 'secondary',
}

interface SnapshotCondition {
  type: ConditionType
  value: unknown
  operator?: string
  value_display?: string
}

interface SnapshotSummary {
  logic: string
  conditions: SnapshotCondition[]
}

/**
 * Reads the two fields of the snapshot this surface renders. Returns null for every shape it
 * cannot vouch for — absent (rows written before migration 000164 added the columns), a string
 * that will not parse, or an object with no usable `conditions` array. The caller says "Not
 * recorded" for null, which is honest; guessing would not be.
 */
function readSnapshot(snapshot: unknown): SnapshotSummary | null {
  let parsed: unknown = snapshot
  if (typeof parsed === 'string') {
    try {
      parsed = JSON.parse(parsed)
    } catch {
      return null
    }
  }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return null

  const obj = parsed as Record<string, unknown>
  const raw = obj.conditions
  if (!Array.isArray(raw) || raw.length === 0) return null

  const conditions = raw.filter(
    (c): c is SnapshotCondition => !!c && typeof c === 'object' && typeof (c as SnapshotCondition).type === 'string'
  )
  if (conditions.length === 0) return null

  return { logic: obj.condition_logic === 'OR' ? 'OR' : 'AND', conditions }
}

function formatExecutedAt(dateStr: string): string {
  return new Date(dateStr).toLocaleDateString('en-US', {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

/** Hard delete + ON DELETE SET NULL: the server has already answered this. */
function isFromDeletedRule(execution: RuleExecution): boolean {
  return !execution.rule_id
}

export function RuleAutomationHistoryPanel() {
  const [isOpen, setIsOpen] = useState(false)
  // ⚠ KNOWN, DELIBERATELY UNFIXED (#1569 round 9, recorded so it is not rediscovered as new).
  // `total > PAGE_SIZE` gates the pager, but `offset` is independent state. Page to offset 20,
  // then have a refetch return a `total` of 20 or fewer (rows aged out, a rule deleted), and the
  // request still asks for offset 20: the body renders empty while the only control that could
  // send the reader back to page 1 has just been removed. `total === 0` is safe — the whole panel
  // returns null above — so the stranded window is 1..PAGE_SIZE.
  // ⚠ FIX BOTH PANELS OR NEITHER: boosts/components/BoostAutomationHistoryPanel.tsx has the identical shape (same
  // PAGE_SIZE, same independent offset, same `total > PAGE_SIZE` gate). Fixing one alone leaves
  // the same bug live on the sibling surface and makes the pair inconsistent — a sibling task
  // this round declined to fix one in isolation for exactly that reason.
  const [offset, setOffset] = useState(0)
  const panelId = useId()
  const trackEvent = useTrackEvent()
  const { sizing, onSizingChange } = useTablePreferences(TABLE_ID, COLUMN_IDS)
  // #1626: the same index the live rules list resolves account conditions through, so a frozen
  // snapshot and the rule it was frozen from name an account identically.
  const accountLabels = useRuleAccountLabels()

  const { data, isLoading, error, refetch } = useGetUserRuleExecutions({
    limit: PAGE_SIZE,
    offset,
  })

  const executions = useMemo<RuleExecution[]>(() => data?.executions ?? [], [data])
  const total = data?.total ?? executions.length
  const deletedRowCount = executions.filter(isFromDeletedRule).length

  if (!isLoading && !error && total === 0) return null

  function handleToggle() {
    const opening = !isOpen
    setIsOpen(opening)
    if (opening) {
      trackEvent(ANALYTICS_EVENTS.RULE_EXECUTION_HISTORY_VIEWED, {
        has_deleted_rule_rows: deletedRowCount > 0,
        deleted_row_count: deletedRowCount,
      })
    }
  }

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))
  const currentPage = Math.floor(offset / PAGE_SIZE) + 1

  const columns: ColumnDef<RuleExecution>[] = [
    {
      id: 'rule',
      header: 'Rule',
      resizable: true,
      defaultSize: 220,
      minSize: 140,
      render: (execution) => (
        <span className="flex flex-wrap items-center gap-1.5">
          <span className="truncate font-medium" data-ph-mask>
            {execution.rule_name ?? 'Unnamed rule'}
          </span>
          {isFromDeletedRule(execution) && (
            <Badge variant="outline" size="sm">
              Deleted rule
            </Badge>
          )}
        </span>
      ),
    },
    {
      id: 'time',
      header: 'When',
      resizable: true,
      defaultSize: 180,
      minSize: 140,
      maxSize: 220,
      render: (execution) => formatExecutedAt(execution.executed_at),
    },
    {
      id: 'change',
      header: 'Changed',
      defaultSize: 150,
      minSize: 110,
      maxSize: 200,
      render: (execution) => ACTION_LABELS[execution.action_type] ?? execution.action_type,
    },
    {
      id: 'result',
      header: 'Result',
      defaultSize: 150,
      minSize: 110,
      maxSize: 200,
      render: (execution) => (
        <Badge variant={RESULT_BADGE_VARIANT[execution.result] ?? 'secondary'}>
          {RESULT_LABELS[execution.result] ?? execution.result}
        </Badge>
      ),
    },
    {
      id: 'why',
      header: 'Why it fired',
      resizable: true,
      defaultSize: 320,
      minSize: 200,
      render: (execution) => {
        const snapshot = readSnapshot(execution.definition_snapshot)
        if (!snapshot) {
          return <span className="text-xs text-muted-foreground">Not recorded</span>
        }
        const shown = snapshot.conditions.slice(0, MAX_CONDITIONS_SHOWN)
        const hidden = snapshot.conditions.length - shown.length
        return (
          <span className="flex flex-col gap-0.5 text-xs">
            {snapshot.conditions.length > 1 && (
              <span className="text-muted-foreground">
                {snapshot.logic === 'OR' ? 'Any of these matched:' : 'All of these matched:'}
              </span>
            )}
            {shown.map((condition, index) => (
              <span key={index} className="flex flex-wrap items-baseline gap-1">
                <span className="text-muted-foreground">
                  {CONDITION_TYPE_LABELS[condition.type] ?? condition.type}
                </span>
                {/* User-identifying text: a merchant substring, an amount, a date, an account name. */}
                <span className="font-medium text-foreground" data-ph-mask>
                  {formatConditionValue(condition, { accountLabels })}
                </span>
              </span>
            ))}
            {hidden > 0 && <span className="text-muted-foreground">+{hidden} more</span>}
          </span>
        )
      },
    },
  ]

  return (
    <section aria-labelledby="rule-automation-history-heading" className="mt-8">
      <h2 id="rule-automation-history-heading" className="sr-only">
        Rule run history
      </h2>
      <button
        type="button"
        onClick={handleToggle}
        aria-expanded={isOpen}
        aria-controls={panelId}
        className="flex w-full items-center justify-between rounded-lg border border-border px-4 py-3 text-left transition-colors motion-reduce:transition-none hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      >
        <span className="text-sm font-medium text-foreground">
          Rule run history{!isLoading && !error ? ` (${total})` : ''}
        </span>
        <ChevronDown
          aria-hidden="true"
          className={`size-4 text-muted-foreground transition-transform motion-reduce:transition-none ${isOpen ? 'rotate-180' : ''}`}
        />
      </button>

      {isOpen && (
        <div id={panelId} className="mt-2 space-y-2">
          {/*
            ⚠ THIS USED TO SAY "EVERY TIME", AND THAT WAS FALSE (#1569 T9-FIX item 8). The backend
            withholds one class deliberately: an execution against a PENDING transaction the
            provider removed when it settled, whose posted twin carries its own execution — listing
            both would report one charge as two automation runs (operations/rule_executions.go).
            The OpenAPI description has documented that exception correctly since the endpoint
            shipped; this panel, added in the same round, overstated it. An unscoped completeness
            claim on a retention surface is the defect class this issue exists to remove, so the
            sentence names the exception rather than being softened into vagueness.
          */}
          <p className="px-1 text-xs text-muted-foreground">
            What your rules changed, including rules you have since deleted &mdash; deleting a rule
            stops it running but keeps the record of what it already did. Those rows are marked
            &ldquo;Deleted rule&rdquo; and show the name and conditions the rule had when it ran.
            One kind of run is left out: a rule that changed a pending charge your bank then
            replaced when it settled, because the settled charge carries the same run and showing
            both would count it twice.
          </p>

          {isLoading && (
            <div role="status" className="px-4 py-6 text-xs text-muted-foreground">
              Loading rule run history&hellip;
            </div>
          )}

          {!isLoading && error && (
            <div
              role="alert"
              className="flex flex-col items-start gap-2 px-4 py-6 text-xs text-destructive"
            >
              <p className="m-0">
                We could not load your rule run history just now. It is still kept &mdash; try again
                in a moment.
              </p>
              <Button variant="outline" size="sm" onClick={() => void refetch()}>
                Retry
              </Button>
            </div>
          )}

          {!isLoading && !error && (
            <>
              <DataTable<RuleExecution>
                data={executions}
                columns={columns}
                rowKey={(e) => e.id}
                tableId={TABLE_ID}
                sort={null}
                onSortChange={() => {}}
                filters={{}}
                onFiltersChange={() => {}}
                columnSizing={sizing}
                onColumnSizingChange={onSizingChange}
                emptyState={
                  <EmptyState
                    size="compact"
                    icon={<History />}
                    title="No runs on this page"
                    className="py-8"
                  />
                }
              />
              {total > PAGE_SIZE && (
                <div className="border-t border-border p-4">
                  <TablePagination
                    currentPage={currentPage}
                    totalPages={totalPages}
                    hasMore={currentPage < totalPages}
                    onPageChange={(page) => setOffset((page - 1) * PAGE_SIZE)}
                  />
                </div>
              )}
            </>
          )}
        </div>
      )}
    </section>
  )
}
