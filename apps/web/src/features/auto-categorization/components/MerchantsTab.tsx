/**
 * MerchantsTab Component
 * Issue #957: Merchant Name Handling — per-user overrides + merge/dedupe UI
 *
 * Renders a DataTable of the user's distinct merchants with rename (display_name override),
 * clear-override, and duplicate-merge actions. A "Suggested Duplicates" panel surfaces
 * fuzzy-matched groups from getMerchantDuplicateSuggestions.
 */

import { useState, useCallback, useMemo } from 'react'
import { Pencil, X, Merge } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { DataTable, useTablePreferences } from '@/shared/components/DataTable'
import type { ColumnDef, SortState, FilterState } from '@/shared/components/DataTable'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics'
import {
  useGetMerchants,
  useGetMerchantDuplicateSuggestions,
  useSetMerchantOverride,
  useClearMerchantOverride,
  useMergeMerchants,
} from '@boostedbudget/api-client/endpoints/merchants/merchants'
import type { MerchantSummary } from '@boostedbudget/api-client/models/merchantSummary'

const TABLE_ID = 'merchants-tab'
const COLUMN_IDS = ['merchant', 'category', 'txn_count', 'total_spend', 'override']

export function MerchantsTab() {
  const trackEvent = useTrackEvent()

  const { data: merchantsRaw, isLoading, error: merchantsError } = useGetMerchants()
  const { data: duplicateGroupsRaw } = useGetMerchantDuplicateSuggestions()

  const { mutateAsync: setOverride } = useSetMerchantOverride()
  const { mutateAsync: clearOverride } = useClearMerchantOverride()
  const { mutateAsync: mergeMerchants } = useMergeMerchants()

  const merchants = useMemo(() => (merchantsRaw as MerchantSummary[] | null) ?? [], [merchantsRaw])
  const duplicateGroups = useMemo(() => (duplicateGroupsRaw as MerchantSummary[][] | null) ?? [], [duplicateGroupsRaw])

  const [renameTarget, setRenameTarget] = useState<MerchantSummary | null>(null)
  const [renameValue, setRenameValue] = useState('')
  const [sort, setSort] = useState<SortState>(null)
  const [filters, setFilters] = useState<FilterState>({})

  const { columnVisibility, onColumnVisibilityChange, columnOrder, onColumnOrderChange } =
    useTablePreferences(TABLE_ID, COLUMN_IDS)

  const handleRename = useCallback((merchant: MerchantSummary) => {
    setRenameTarget(merchant)
    setRenameValue(merchant.effective_display_name)
  }, [])

  const handleRenameSubmit = useCallback(async () => {
    if (!renameTarget) return
    await setOverride({
      merchantId: renameTarget.merchant_id,
      data: { display_name: renameValue },
    })
    trackEvent(ANALYTICS_EVENTS.MERCHANT_OVERRIDE_SET)
    setRenameTarget(null)
  }, [renameTarget, renameValue, setOverride, trackEvent])

  const handleClearOverride = useCallback(async (merchant: MerchantSummary) => {
    await clearOverride({ merchantId: merchant.merchant_id })
    trackEvent(ANALYTICS_EVENTS.MERCHANT_OVERRIDE_CLEARED)
  }, [clearOverride, trackEvent])

  const handleMerge = useCallback(async (group: MerchantSummary[]) => {
    if (group.length < 2) return
    await mergeMerchants({
      data: {
        merchant_ids: group.map((m) => m.merchant_id),
        target_merchant_id: group[0].merchant_id,
      },
    })
    trackEvent(ANALYTICS_EVENTS.MERCHANTS_MERGED)
  }, [mergeMerchants, trackEvent])

  const columns: ColumnDef<MerchantSummary>[] = [
    {
      id: 'merchant',
      header: 'Merchant',
      render: (merchant) => (
        <div>
          <span className="font-medium">{merchant.effective_display_name}</span>
          {merchant.has_override && (
            <Badge variant="secondary" className="ml-2 text-xs">renamed</Badge>
          )}
          {merchant.normalized_name !== merchant.effective_display_name && (
            <p className="text-xs text-muted-foreground mt-0.5">{merchant.normalized_name}</p>
          )}
        </div>
      ),
    },
    {
      id: 'category',
      header: 'Category',
      render: (merchant) => (
        <span className="text-sm text-muted-foreground">
          {merchant.dominant_category?.replace(/_/g, ' ') ?? '—'}
        </span>
      ),
    },
    {
      id: 'txn_count',
      header: 'Transactions',
      render: (merchant) => (
        <span className="text-sm tabular-nums">{merchant.transaction_count}</span>
      ),
    },
    {
      id: 'total_spend',
      header: 'Total Spend',
      render: (merchant) => (
        <span className="text-sm tabular-nums" data-ph-mask>
          ${merchant.total_spend.toFixed(2)}
        </span>
      ),
    },
    {
      id: 'override',
      header: 'Override',
      render: (merchant) =>
        merchant.has_override ? (
          <Badge variant="outline" className="text-xs">custom</Badge>
        ) : null,
    },
  ]

  const rowActions = useCallback((merchant: MerchantSummary) => (
    <div className="flex items-center gap-1">
      <button
        type="button"
        data-testid={`action-rename-${merchants.indexOf(merchant)}`}
        className="inline-flex items-center gap-1.5 rounded px-2 py-1 text-xs font-medium text-muted-foreground hover:bg-muted hover:text-foreground transition-colors"
        onClick={() => handleRename(merchant)}
        aria-label={`Rename ${merchant.effective_display_name}`}
      >
        <Pencil className="h-3 w-3" />
        Rename
      </button>
      {merchant.has_override && (
        <button
          type="button"
          data-testid={`action-clear-override-${merchants.indexOf(merchant)}`}
          className="inline-flex items-center gap-1.5 rounded px-2 py-1 text-xs font-medium text-muted-foreground hover:bg-muted hover:text-foreground transition-colors"
          onClick={() => handleClearOverride(merchant)}
          aria-label={`Clear override for ${merchant.effective_display_name}`}
        >
          <X className="h-3 w-3" />
          Clear override
        </button>
      )}
    </div>
  ), [merchants, handleRename, handleClearOverride])

  if (merchantsError) {
    return (
      <div className="py-12 text-center text-sm text-destructive" data-testid="merchants-error">
        Failed to load merchants. Please try again.
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {duplicateGroups.length > 0 && (
        <div data-testid="duplicate-suggestions" className="rounded-lg border bg-card p-4 space-y-3">
          <div className="flex items-center gap-2">
            <Merge className="h-4 w-4 text-muted-foreground" />
            <h3 className="text-sm font-semibold">Suggested Duplicates</h3>
            <Badge variant="secondary">{duplicateGroups.length}</Badge>
          </div>
          <p className="text-xs text-muted-foreground">
            These merchants look similar. Merging them collapses all transactions under one name.
          </p>
          <div className="space-y-2">
            {duplicateGroups.map((group, i) => (
              <div key={i} className="flex items-center justify-between rounded-md border bg-background px-3 py-2">
                <div className="flex flex-wrap gap-1.5">
                  {group.map((m) => (
                    <Badge key={m.merchant_id} variant="outline" className="text-xs">
                      {m.effective_display_name}
                    </Badge>
                  ))}
                </div>
                <Button
                  size="sm"
                  variant="outline"
                  data-testid={`merge-group-${i}`}
                  onClick={() => handleMerge(group)}
                  className="ml-3 shrink-0"
                >
                  Merge
                </Button>
              </div>
            ))}
          </div>
        </div>
      )}

      <DataTable
        data={merchants}
        columns={columns}
        rowKey={(m) => m.merchant_id}
        rowActions={rowActions}
        sort={sort}
        onSortChange={setSort}
        filters={filters}
        onFiltersChange={setFilters}
        emptyState={
          <div className="py-12 text-center text-sm text-muted-foreground">
            No merchants found. Sync transactions to see your merchants here.
          </div>
        }
        maskedColumnIds={['total_spend']}
        columnVisibility={columnVisibility}
        onColumnVisibilityChange={onColumnVisibilityChange}
        columnOrder={columnOrder}
        onColumnOrderChange={onColumnOrderChange}
        loading={isLoading}
      />

      {renameTarget && (
        <div
          data-testid="rename-modal"
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/40"
          role="dialog"
          aria-modal
          aria-label="Rename merchant"
        >
          <div className="rounded-xl border bg-card shadow-lg w-full max-w-sm p-6 space-y-4">
            <h2 className="text-base font-semibold">Rename merchant</h2>
            <p className="text-sm text-muted-foreground">
              Set a custom display name for{' '}
              <span className="font-medium">{renameTarget.normalized_name}</span>.
            </p>
            <input
              data-testid="rename-input"
              type="text"
              value={renameValue}
              onChange={(e) => setRenameValue(e.target.value)}
              className="w-full rounded-md border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
              placeholder="Custom display name"
              autoFocus
            />
            <div className="flex justify-end gap-2">
              <Button variant="outline" size="sm" onClick={() => setRenameTarget(null)}>
                Cancel
              </Button>
              <Button
                data-testid="rename-save-btn"
                size="sm"
                onClick={handleRenameSubmit}
                disabled={!renameValue.trim()}
              >
                Save
              </Button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
