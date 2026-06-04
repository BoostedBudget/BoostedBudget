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

import { useState, useCallback, useEffect } from 'react'
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
import type { TransactionFilters, Transaction } from '../types'

const INBOX_PAGE_LIMIT = 30

const formatTransactionCount = (count: number) =>
  `${count} unreviewed transaction${count === 1 ? '' : 's'}`

const CURRENCY_FORMATTER = new Intl.NumberFormat('en-US', {
  style: 'currency',
  currency: 'USD',
  minimumFractionDigits: 2,
})

export function ReviewInboxPage() {
  const trackEvent = useTrackEvent()

  // ── Summary (nav badge + header) ──────────────────────────────────────────
  const { summary } = useReviewSummary()

  // ── Filters ───────────────────────────────────────────────────────────────
  const [activePreset, setActivePreset] = useState<ReviewPreset>('unreviewed')
  const [filters, setFilters] = useState<TransactionFilters>({ reviewed: false })

  // ── Transactions ──────────────────────────────────────────────────────────
  const { transactions: apiTransactions, isLoading, hasNextPage, fetchNextPage, isFetchingNextPage } =
    useInfiniteTransactions({
      filters,
      sortBy: 'date',
      sortDir: 'desc',
      limit: INBOX_PAGE_LIMIT,
    })
  // Cast API transactions to local Transaction type (compatible shape, narrower string literals)
  const transactions = apiTransactions as unknown as Transaction[]

  // ── Mutations ─────────────────────────────────────────────────────────────
  const { setReviewed, bulkSetReviewed, bulkRecategorize, isPending } = useReviewMutations()

  // ── Row selection ─────────────────────────────────────────────────────────
  const { selectedIds, selectedCount, isSelected, toggle, selectAll, clearSelection } =
    useRowSelection()

  // ── Triage ────────────────────────────────────────────────────────────────
  const [triageIndex, setTriageIndex] = useState<number | null>(null)
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

  const handleBulkMarkReviewed = useCallback(async () => {
    const ids = [...selectedIds]
    try {
      await bulkSetReviewed(ids, true)
      clearSelection()
      toast.success(`Marked ${formatTransactionCount(ids.length)} as reviewed`, {
        action: {
          label: 'Undo',
          onClick: async () => {
            await bulkSetReviewed(ids, false)
            trackEvent(ANALYTICS_EVENTS.REVIEW_UNDO, { count: ids.length })
          },
        },
      })
    } catch {
      toast.error('Failed to mark as reviewed. Please try again.')
    }
  }, [selectedIds, bulkSetReviewed, clearSelection, trackEvent])

  const handleBulkMarkUnreviewed = useCallback(async () => {
    const ids = [...selectedIds]
    try {
      await bulkSetReviewed(ids, false)
      clearSelection()
      toast.success(`Moved ${formatTransactionCount(ids.length)} back to inbox`)
    } catch {
      toast.error('Failed to mark as unreviewed. Please try again.')
    }
  }, [selectedIds, bulkSetReviewed, clearSelection])

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

  const handleTriageApprove = useCallback(async () => {
    if (!triageTx) return
    try {
      await handleApprove(triageTx)
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
  }, [triageTx, triageIndex, transactions.length, handleApprove, filters.reviewed])

  const handleTriageSkip = useCallback(() => {
    if (triageIndex === null) return
    if (triageIndex < transactions.length - 1) {
      setTriageIndex(triageIndex + 1)
    } else {
      setTriageIndex(null)
    }
    if (triageTx) trackEvent(ANALYTICS_EVENTS.TRANSACTION_SKIPPED, { transaction_id: triageTx.id })
  }, [triageIndex, transactions.length, triageTx, trackEvent])

  const handleLoadMore = useCallback(() => {
    if (hasNextPage && !isFetchingNextPage) {
      fetchNextPage()
    }
  }, [hasNextPage, isFetchingNextPage, fetchNextPage])

  // ── All caught up ─────────────────────────────────────────────────────────
  const allCaughtUp = !isLoading && transactions.length === 0 && activePreset === 'unreviewed'

  return (
    <div className="flex flex-col gap-6">
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
          <div className="overflow-hidden rounded-xl border border-border">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border bg-muted/50">
                  <th className="w-10 px-4 py-2 text-left">
                    <input
                      type="checkbox"
                      checked={selectedCount === transactions.length && transactions.length > 0}
                      onChange={handleSelectAll}
                      aria-label="Select all transactions"
                      className="size-4 rounded border-border"
                    />
                  </th>
                  <th className="px-4 py-2 text-left font-medium text-muted-foreground">Date</th>
                  <th className="px-4 py-2 text-left font-medium text-muted-foreground">Transaction</th>
                  <th className="px-4 py-2 text-left font-medium text-muted-foreground">Status</th>
                  <th className="px-4 py-2 text-right font-medium text-muted-foreground">Amount</th>
                </tr>
              </thead>
              <tbody>
                {transactions.map((tx, idx) => (
                  <tr
                    key={tx.id}
                    className="border-b border-border/50 hover:bg-muted/30 cursor-pointer"
                    onClick={() => setTriageIndex(idx)}
                    onKeyDown={(e) => { if (e.key === 'Enter') setTriageIndex(idx) }}
                    tabIndex={0}
                    aria-label={`Open triage for ${tx.merchant_name || tx.name}`}
                  >
                    <td className="px-4 py-3">
                      <input
                        type="checkbox"
                        checked={isSelected(tx.id)}
                        onChange={() => toggle(tx.id)}
                        onClick={(e) => e.stopPropagation()}
                        aria-label={`Select ${tx.merchant_name || tx.name}`}
                        className="size-4 rounded border-border"
                      />
                    </td>
                    <td className="px-4 py-3 text-muted-foreground">{tx.date}</td>
                    <td className="px-4 py-3 font-medium text-foreground" data-ph-mask>
                      {tx.merchant_name || tx.name}
                    </td>
                    <td className="px-4 py-3">
                      <ReviewBadge variant={reviewBadgeVariantFromTransaction(tx)} />
                    </td>
                    <td className="px-4 py-3 text-right" data-ph-mask>
                      {CURRENCY_FORMATTER.format(Math.abs(tx.amount))}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

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
            onApprove={handleTriageApprove}
            onSkip={handleTriageSkip}
            onRecategorize={handleTriageRecategorize}
            onToggleSelect={() => triageTx && toggle(triageTx.id)}
            onNext={() => triageIndex !== null && setTriageIndex(Math.min(triageIndex + 1, transactions.length - 1))}
            onPrev={() => triageIndex !== null && setTriageIndex(Math.max(0, triageIndex - 1))}
            onClose={() => setTriageIndex(null)}
            isSelected={triageTx ? isSelected(triageTx.id) : false}
            isPending={isPending}
          />
        </div>
      )}

      {/* Bulk action bar */}
      <ReviewBulkActionBar
        selectedCount={selectedCount}
        onMarkReviewed={handleBulkMarkReviewed}
        onMarkUnreviewed={handleBulkMarkUnreviewed}
        onClear={clearSelection}
        isLoading={isPending}
      />
    </div>
  )
}
