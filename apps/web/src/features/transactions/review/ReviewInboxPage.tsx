/**
 * ReviewInboxPage — transaction review/triage inbox (#1210).
 *
 * Layout:
 *   - Header: title + summary counts (unreviewed total)
 *   - Filter presets (segmented control)
 *   - Desktop: DataTable with selection column + ReviewBadge column
 *   - Mobile: TriageDetailView (one-at-a-time)
 *   - Sticky ReviewBulkActionBar when ≥1 rows selected
 *
 * The nav badge and summary header both read the ['review-summary'] query key,
 * which is invalidated by every review/category mutation in useReviewMutations.
 */

import { useState, useCallback, useEffect, useMemo, useRef } from 'react'
import { getMerchantDisplayName } from '@/shared/transactions/merchant'
import { ClipboardCheck, Inbox } from 'lucide-react'
import { toast } from 'sonner'
import { EmptyState } from '@/components/ui/empty-state'
import { useInfiniteTransactions } from '../hooks/useInfiniteTransactions'
import { useReviewMutations } from '../hooks/useReviewMutations'
import { useReviewSummary } from '../hooks/useReviewSummary'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics/events'
import { ReviewBadge, reviewBadgeVariantFromTransaction } from '@/shared/components/ReviewBadge'
import { useRowSelection } from './hooks/useRowSelection'
import { ReviewFilterPresets } from './components/ReviewFilterPresets'
import { ReviewBulkActionBar } from './components/ReviewBulkActionBar'
import { TriageDetailView } from './components/TriageDetailView'
import type { ReviewPreset } from './components/ReviewFilterPresets'
import type { TransactionFilters, Transaction, FeedRow } from '../types'
import { isInvestmentRow } from '../types'
import { DataTable, useTablePreferences } from '@/shared/components/DataTable'
import type { ColumnDef } from '@/shared/components/DataTable'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { CategoryEditor } from '../../categories/components/CategoryEditor'
import { useGetUserCategories, useGetPlaidPrimaryCategories } from '@boostedbudget/api-client/endpoints/categories/categories'
import type { CustomCategory } from '../../categories/types'
import { formatDate } from '@/shared/transactions/dates'
import type { Primary } from '@boostedbudget/api-client/models/primary'

const INBOX_PAGE_LIMIT = 30

const formatTransactionCount = (count: number) =>
  `${count} unreviewed transaction${count === 1 ? '' : 's'}`

const CURRENCY_FORMATTER = new Intl.NumberFormat('en-US', {
  style: 'currency',
  currency: 'USD',
  minimumFractionDigits: 2,
})

const REVIEW_INBOX_COLUMN_IDS = ['select', 'date', 'merchant', 'status', 'amount'] as const

export function ReviewInboxPage() {
  const trackEvent = useTrackEvent()

  // ── Summary (nav badge + header) ──────────────────────────────────────────
  const { summary } = useReviewSummary()

  // ── Filters ───────────────────────────────────────────────────────────────
  const [activePreset, setActivePreset] = useState<ReviewPreset>('unreviewed')
  const [filters, setFilters] = useState<TransactionFilters>({ reviewed: false })

  // ── Transactions ──────────────────────────────────────────────────────────
  const { transactions: apiTransactions, isLoading, isError, hasNextPage, fetchNextPage, isFetchingNextPage } =
    useInfiniteTransactions({
      filters,
      sortBy: 'date',
      sortDir: 'desc',
      limit: INBOX_PAGE_LIMIT,
    })
  // Cast API transactions to local Transaction type (compatible shape, narrower string literals)
  // Investment rows are read-only and never have review state — exclude them from the inbox.
  const castedTransactions = apiTransactions as unknown as Transaction[]
  const transactions = castedTransactions.filter(
    (t) => !isInvestmentRow(t as unknown as FeedRow)
  )

  // ── All caught up (computed early — used in analytics effect below) ────────
  const allCaughtUp = !isLoading && !isError && transactions.length === 0 && activePreset === 'unreviewed'

  // ── Mutations ─────────────────────────────────────────────────────────────
  const { setReviewed, bulkSetReviewed, bulkRecategorize, isPending } = useReviewMutations()

  // ── Category data (for triage + bulk recategorize) ────────────────────────
  const { data: categoriesData } = useGetUserCategories()
  const { data: plaidPrimariesData } = useGetPlaidPrimaryCategories()
  const availableCategories = useMemo(() => (categoriesData as unknown as CustomCategory[]) ?? [], [categoriesData])
  const plaidPrimaries = useMemo(() => (plaidPrimariesData as unknown as Primary[]) ?? [], [plaidPrimariesData])

  // ── Bulk recategorize state ───────────────────────────────────────────────
  const [showBulkRecategorize, setShowBulkRecategorize] = useState(false)
  const [pendingBulkCategory, setPendingBulkCategory] = useState<string[]>([])
  const [bulkCategoryDirty, setBulkCategoryDirty] = useState(false)

  // ── Row selection ─────────────────────────────────────────────────────────
  const { selectedIds, selectedCount, isSelected, toggle, selectAll, clearSelection } =
    useRowSelection()

  // ── Table preferences ─────────────────────────────────────────────────────
  const { sizing, onSizingChange, columnOrder, onColumnOrderChange } =
    useTablePreferences('review-inbox', [...REVIEW_INBOX_COLUMN_IDS])

  // ── Triage ────────────────────────────────────────────────────────────────
  const [triageIndex, setTriageIndex] = useState<number | null>(null)
  const [announcement, setAnnouncement] = useState('')
  const lastTriageApprovedId = useRef<string | null>(null)
  const [isMobile, setIsMobile] = useState(() =>
    typeof window !== 'undefined' && window.innerWidth < 768
  )
  useEffect(() => {
    const mql = window.matchMedia('(max-width: 767px)')
    const onChange = (e: MediaQueryListEvent) => setIsMobile(e.matches)
    mql.addEventListener('change', onChange)
    return () => mql.removeEventListener('change', onChange)
  }, [])

  // Open triage on first load for mobile
  useEffect(() => {
    if (isMobile && transactions.length > 0 && triageIndex === null) {
      setTriageIndex(0)
    }
  }, [isMobile, transactions.length, triageIndex])

  // Clamp triageIndex when list shrinks (e.g. after approved item removed from filtered view)
  useEffect(() => {
    if (triageIndex === null) return
    if (transactions.length === 0) setTriageIndex(null)
    else if (triageIndex >= transactions.length) setTriageIndex(transactions.length - 1)
  }, [transactions.length, triageIndex])

  // ── Analytics ─────────────────────────────────────────────────────────────
  useEffect(() => {
    trackEvent(ANALYTICS_EVENTS.REVIEW_INBOX_VIEWED)
  }, [trackEvent])

  useEffect(() => {
    if (allCaughtUp) trackEvent(ANALYTICS_EVENTS.REVIEW_ALL_CAUGHT_UP)
  }, [allCaughtUp, trackEvent])

  // ── Handlers ──────────────────────────────────────────────────────────────

  const handlePresetChange = useCallback(
    (preset: ReviewPreset, presetFilters: Pick<TransactionFilters, 'reviewed' | 'category_sources'>) => {
      setActivePreset(preset)
      setFilters(presetFilters)
      clearSelection()
      setTriageIndex(null)
      trackEvent(ANALYTICS_EVENTS.REVIEW_PRESET_CHANGED, { preset })
    },
    [clearSelection, trackEvent],
  )

  const handleApprove = useCallback(async (tx: Transaction) => {
    await setReviewed(tx.id, true)
    trackEvent(ANALYTICS_EVENTS.TRANSACTION_APPROVED, { transaction_id: tx.id })
  }, [setReviewed, trackEvent])

  const handleBulkSetReviewed = useCallback(async (reviewed: boolean) => {
    const ids = [...selectedIds]
    const count = ids.length
    const countLabel = `${count} transaction${count === 1 ? '' : 's'}`
    try {
      await bulkSetReviewed(ids, reviewed)
      clearSelection()
      if (reviewed) {
        setAnnouncement(`Marked ${countLabel} as reviewed`)
        toast.success(`Marked ${countLabel} as reviewed`, {
          action: {
            label: 'Undo',
            onClick: async () => {
              await bulkSetReviewed(ids, false)
              setAnnouncement(`Moved ${countLabel} back to inbox`)
              trackEvent(ANALYTICS_EVENTS.REVIEW_UNDO, { count })
            },
          },
        })
      } else {
        setAnnouncement(`Moved ${countLabel} back to inbox`)
        toast.success(`Moved ${countLabel} back to inbox`)
      }
    } catch {
      const action = reviewed ? 'marked as reviewed' : 'marked as unreviewed'
      toast.error(`Failed to ${action}. Please try again.`)
    }
  }, [selectedIds, bulkSetReviewed, clearSelection, trackEvent])

  const handleSelectAll = useCallback(() => {
    if (selectedCount === transactions.length) {
      clearSelection()
    } else {
      selectAll(transactions.map((t) => t.id))
    }
  }, [selectedCount, transactions, clearSelection, selectAll])

  // ── Triage helpers ─────────────────────────────────────────────────────────
  const triageTx = triageIndex !== null ? transactions[triageIndex] : null

  const handleTriageRecategorize = useCallback(async (
    request: Parameters<typeof bulkRecategorize>[1]
  ) => {
    if (!triageTx) return
    try {
      await bulkRecategorize([triageTx.id], request)
    } catch {
      toast.error('Failed to recategorize. Please try again.')
    }
  }, [triageTx, bulkRecategorize])

  // id is explicit for toast (closure-captured); falls back to ref for ⌘Z (most-recent approval)
  const handleTriageUndo = useCallback(async (id?: string) => {
    const targetId = id ?? lastTriageApprovedId.current
    if (!targetId) return
    try {
      await setReviewed(targetId, false)
      lastTriageApprovedId.current = null
      setAnnouncement('Moved back to inbox')
      trackEvent(ANALYTICS_EVENTS.REVIEW_UNDO, { count: 1 })
    } catch {
      toast.error('Failed to undo. Please try again.')
    }
  }, [setReviewed, trackEvent])

  const handleTriageApprove = useCallback(async () => {
    if (!triageTx) return
    try {
      await handleApprove(triageTx)
      const approvedId = triageTx.id
      lastTriageApprovedId.current = approvedId
      setAnnouncement('Marked as reviewed')
      toast.success('Marked as reviewed', {
        action: { label: 'Undo', onClick: () => handleTriageUndo(approvedId) },
      })
      // When filter removes approved items (reviewed:false presets), the list refetch
      // shrinks the array and the clamping effect handles index correction — don't advance.
      // For presets where items stay visible, advance to the next row.
      if (filters.reviewed !== false) {
        if (triageIndex !== null && triageIndex < transactions.length - 1) {
          setTriageIndex(triageIndex + 1)
        } else {
          setTriageIndex(null)
        }
      }
    } catch {
      toast.error('Failed to mark as reviewed. Please try again.')
    }
  }, [triageTx, triageIndex, transactions.length, handleApprove, filters.reviewed, handleTriageUndo])

  const handleTriageSkip = useCallback(() => {
    if (triageIndex === null) return
    if (triageIndex < transactions.length - 1) {
      setTriageIndex(triageIndex + 1)
    } else {
      setTriageIndex(null)
    }
    setAnnouncement('Skipped')
    if (triageTx) trackEvent(ANALYTICS_EVENTS.TRANSACTION_SKIPPED, { transaction_id: triageTx.id })
  }, [triageIndex, transactions.length, triageTx, trackEvent])

  const handleLoadMore = useCallback(() => {
    if (hasNextPage && !isFetchingNextPage) {
      fetchNextPage()
    }
  }, [hasNextPage, isFetchingNextPage, fetchNextPage])

  // ── Bulk recategorize apply ───────────────────────────────────────────────
  const handleApplyBulkRecategorize = useCallback(async () => {
    const ids = [...selectedIds]
    const customCat = availableCategories.find((c) => c.name === pendingBulkCategory[0])
    const plaidMatch = !customCat ? plaidPrimaries?.find((p) => p.display === pendingBulkCategory[0]) : null
    const request =
      pendingBulkCategory.length === 0
        ? { kind: 'clear' as const }
        : customCat
          ? { kind: 'custom' as const, custom_category_id: customCat.id }
          : { kind: 'pfc' as const, pfc_primary: plaidMatch?.enum ?? pendingBulkCategory[0], pfc_detailed: pendingBulkCategory[1] }
    try {
      await bulkRecategorize(ids, request)
      setShowBulkRecategorize(false)
      setPendingBulkCategory([])
      clearSelection()
      toast.success(`Recategorized ${ids.length} transaction${ids.length === 1 ? '' : 's'}`)
    } catch {
      toast.error('Failed to recategorize. Please try again.')
    }
  }, [selectedIds, availableCategories, plaidPrimaries, pendingBulkCategory, bulkRecategorize, clearSelection])

  // ── Column definitions ────────────────────────────────────────────────────
  const columns = useMemo((): ColumnDef<Transaction>[] => [
    {
      id: 'select',
      header: (
        <input
          type="checkbox"
          checked={selectedCount === transactions.length && transactions.length > 0}
          onChange={handleSelectAll}
          aria-label="Select all transactions"
          className="size-4 rounded border-border"
        />
      ),
      widthClass: 'w-10',
      render: (tx) => (
        <input
          type="checkbox"
          checked={isSelected(tx.id)}
          onChange={() => toggle(tx.id)}
          onClick={(e) => e.stopPropagation()}
          aria-label="Select transaction"
          className="size-4 rounded border-border"
        />
      ),
    },
    {
      id: 'date',
      header: 'Date',
      render: (tx) => <span className="text-muted-foreground">{formatDate(tx.date)}</span>,
    },
    {
      id: 'merchant',
      header: 'Transaction',
      render: (tx) => <span className="font-medium text-foreground">{getMerchantDisplayName(tx)}</span>,
    },
    {
      id: 'status',
      header: 'Status',
      render: (tx) => <ReviewBadge variant={reviewBadgeVariantFromTransaction(tx)} />,
    },
    {
      id: 'amount',
      header: 'Amount',
      align: 'right',
      render: (tx) => CURRENCY_FORMATTER.format(Math.abs(tx.amount)),
    },
  ], [selectedCount, transactions.length, handleSelectAll, isSelected, toggle])

  return (
    <div className="flex flex-col gap-6">
      <div className="sr-only" aria-live="polite" aria-atomic="true" role="status">{announcement}</div>

      {/* Page header */}
      <div className="flex items-start justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-foreground">Review Inbox</h1>
          {summary && (
            <p className="mt-1 text-sm text-muted-foreground">
              {summary.unreviewed_count > 0 ? formatTransactionCount(summary.unreviewed_count) : 'All caught up'}
            </p>
          )}
        </div>
      </div>

      {/* Filter presets */}
      <ReviewFilterPresets activePreset={activePreset} onChange={handlePresetChange} />

      {/* All caught up state */}
      {allCaughtUp && (
        <EmptyState
          icon={<ClipboardCheck />}
          title="All caught up!"
          description="You have no unreviewed transactions. Great work!"
        />
      )}

      {/* Empty state for other presets */}
      {!isLoading && transactions.length === 0 && !allCaughtUp && (
        <EmptyState
          icon={<Inbox />}
          title="No transactions found"
          description="No transactions match the selected filter."
        />
      )}

      {/* Desktop: Transaction list table */}
      {transactions.length > 0 && (
        <div className="hidden md:block">
          <DataTable<Transaction>
            data={transactions}
            columns={columns}
            rowKey={(row) => row.id}
            sort={null}
            onSortChange={() => {}}
            filters={{}}
            onFiltersChange={() => {}}
            onRowClick={(row) => setTriageIndex(transactions.indexOf(row))}
            columnSizing={sizing}
            onColumnSizingChange={onSizingChange}
            columnOrder={columnOrder}
            onColumnOrderChange={onColumnOrderChange}
            maskedColumnIds={['merchant', 'amount']}
          />

          {hasNextPage && (
            <div className="mt-4 text-center">
              <button
                type="button"
                onClick={handleLoadMore}
                disabled={isFetchingNextPage}
                className="text-sm text-primary hover:underline disabled:opacity-50"
              >
                {isFetchingNextPage ? 'Loading...' : 'Load more'}
              </button>
            </div>
          )}
        </div>
      )}

      {/* Triage detail view (desktop: sidebar/focus when row clicked; mobile: always) */}
      {triageTx && (
        <div className="fixed inset-x-4 bottom-24 z-40 md:static md:inset-auto md:z-auto">
          <TriageDetailView
            transaction={triageTx}
            currentIndex={triageIndex ?? 0}
            total={transactions.length}
            availableCategories={availableCategories}
            plaidPrimaries={plaidPrimaries}
            onApprove={handleTriageApprove}
            onSkip={handleTriageSkip}
            onRecategorize={handleTriageRecategorize}
            onToggleSelect={() => triageTx && toggle(triageTx.id)}
            onNext={() => triageIndex !== null && setTriageIndex(Math.min(triageIndex + 1, transactions.length - 1))}
            onPrev={() => triageIndex !== null && setTriageIndex(Math.max(0, triageIndex - 1))}
            onClose={() => setTriageIndex(null)}
            onUndo={handleTriageUndo}
            isSelected={triageTx ? isSelected(triageTx.id) : false}
            isPending={isPending}
            externalOverlayOpen={showBulkRecategorize}
          />
        </div>
      )}

      {/* Bulk action bar */}
      <ReviewBulkActionBar
        selectedCount={selectedCount}
        onMarkReviewed={() => handleBulkSetReviewed(true)}
        onMarkUnreviewed={() => handleBulkSetReviewed(false)}
        onBulkRecategorize={() => {
          setPendingBulkCategory([])
          setBulkCategoryDirty(false)
          setShowBulkRecategorize(true)
        }}
        onClear={clearSelection}
        isLoading={isPending}
      />

      {/* Bulk recategorize dialog */}
      <Dialog open={showBulkRecategorize} onOpenChange={setShowBulkRecategorize}>
        <DialogContent aria-describedby={undefined}>
          <DialogHeader>
            <DialogTitle>Recategorize {selectedCount} transaction{selectedCount === 1 ? '' : 's'}</DialogTitle>
          </DialogHeader>
          <CategoryEditor
            selectedCategory={pendingBulkCategory}
            availableCategories={availableCategories}
            plaidPrimaries={plaidPrimaries}
            onChange={(cat) => {
              setPendingBulkCategory(cat)
              setBulkCategoryDirty(true)
            }}
            disabled={isPending}
          />
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowBulkRecategorize(false)}>Cancel</Button>
            <Button onClick={handleApplyBulkRecategorize} disabled={isPending || !bulkCategoryDirty}>
              Apply
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
