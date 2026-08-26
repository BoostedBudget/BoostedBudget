/**
 * BoostsManagePage Component
 * Issue #39: Boost Builder UI
 *
 * Main page for viewing and managing automation boosts.
 * Supports card and table views.
 *
 * Accepts optional `showTypeSegment` + `boostTypeFilter` props used by BoostsTab
 * (inside Boosts page) to scope the default view to transfer-generating boost types
 * while still allowing "All boosts" navigation to avoid orphaning other types.
 */

import { useCallback, useMemo, useState } from 'react'
import { useNavigate } from 'react-router'
import { useTrackEvent } from '../../../shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '../../../shared/analytics'
import { useQueryClient } from '@tanstack/react-query'
import { AlertCircle, Plus, Zap } from 'lucide-react'
import { EmptyState } from '@/components/ui/empty-state'
import { ConfirmationDialog } from '@/components/ui/confirmation-dialog'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import {
  useGetBoosts,
  useToggleBoost,
  useDeleteBoost,
  useTestBoost,
  getGetBoostsQueryKey,
} from '@boostedbudget/api-client/endpoints/boosts/boosts'
import type { Boost } from '@boostedbudget/api-client/models'
import { ListViewToggle } from '@/shared/components/ListViewToggle'
import type { ListView } from '@/shared/components/ListViewToggle'
import { STORAGE_KEY_LIST_VIEW } from '../boostConstants'
import { ReadOnlyGuard } from '@/auth/ReadOnlyGuard'
import { BoostAutomationHistoryPanel } from './BoostAutomationHistoryPanel'
import { BoostCard } from './BoostCard'
import { BoostTable, type SortColumn } from './BoostTable'
import { useBoostFilters } from '../hooks/useBoostFilters'
import { sortBoosts } from '../hooks/utils/sortBoosts'

interface BoostsManagePageProps {
  /** When true, renders a "Transfer & Savings / All boosts" segment above the list. */
  showTypeSegment?: boolean
  /**
   * Initial boost-type filter passed to the backend query when the segment is shown.
   * Accepts readonly arrays (e.g. `as const` tuples from constants files).
   * Defaults to showing all boosts when omitted (no segment).
   */
  boostTypeFilter?: readonly string[]
}
export function BoostsManagePage({ showTypeSegment = false, boostTypeFilter }: BoostsManagePageProps = {}) {
  const trackEvent = useTrackEvent()
  const navigate = useNavigate()
  const queryClient = useQueryClient()

  // Copy to mutable array so the state setter and query params receive the right type.
  const [activeBoostTypeFilter, setActiveBoostTypeFilter] = useState<string[] | undefined>(
    boostTypeFilter ? [...boostTypeFilter] : undefined
  )

  const [view, setView] = useState<ListView>(() => {
    try {
      const stored = localStorage.getItem(STORAGE_KEY_LIST_VIEW)
      if (stored === 'cards' || stored === 'table') {
        return stored
      }
    } catch {
      // Ignore localStorage errors
    }
    return 'cards'
  })
  const [deleteConfirm, setDeleteConfirm] = useState<Boost | null>(null)

  const { sort, setSort } = useBoostFilters()
  const sortColumn = sort?.columnId as SortColumn | undefined
  const sortDirection = sort?.direction ?? 'asc'

  // Fetch boosts — pass boost_type filter when segment is active
  const queryParams = activeBoostTypeFilter ? { boost_type: activeBoostTypeFilter } : undefined
  const {
    data,
    isLoading,
    error,
    refetch,
  } = useGetBoosts(queryParams)

  const boosts = useMemo(() => data ?? [], [data])

  // Mutations — Issue #579: Optimistic updates with boost_type filter awareness
  const toggleMutation = useToggleBoost({
    mutation: {
      onMutate: async ({ id, data }) => {
        await queryClient.cancelQueries({ queryKey: getGetBoostsQueryKey(queryParams) })
        const previousBoosts = queryClient.getQueryData(getGetBoostsQueryKey(queryParams))
        queryClient.setQueryData(
          getGetBoostsQueryKey(queryParams),
          (old: Boost[] | undefined) =>
            old?.map((b) =>
              b.id === id ? { ...b, enabled: data.enabled } : b
            )
        )
        return { previousBoosts }
      },
      onError: (_err, _variables, context) => {
        if (context?.previousBoosts) {
          queryClient.setQueryData(
            getGetBoostsQueryKey(queryParams),
            context.previousBoosts
          )
        }
      },
      onSettled: () => {
        queryClient.invalidateQueries({ queryKey: getGetBoostsQueryKey() })
      },
    },
  })
  const deleteMutation = useDeleteBoost()
  const testMutation = useTestBoost()

  // Handlers
  const handleCreateBoost = useCallback(() => {
    navigate('/boosts/new')
  }, [navigate])

  const handleEditBoost = useCallback(
    (boost: Boost) => {
      navigate(`/boosts/${boost.id}/edit`)
    },
    [navigate]
  )

  const handleToggleBoost = useCallback(
    (boost: Boost, enabled: boolean) => {
      toggleMutation.mutate({
        id: boost.id,
        data: { enabled },
      })
    },
    [toggleMutation]
  )

  const handleDeleteClick = useCallback((boost: Boost) => {
    setDeleteConfirm(boost)
  }, [])

  const handleDeleteConfirm = useCallback(() => {
    if (deleteConfirm) {
      deleteMutation.mutate(
        { id: deleteConfirm.id },
        {
          onSuccess: () => {
            trackEvent(ANALYTICS_EVENTS.BOOST_DELETED)
            setDeleteConfirm(null)
            refetch()
          },
        }
      )
    }
  }, [deleteConfirm, deleteMutation, trackEvent, refetch])

  const handleDeleteCancel = useCallback(() => {
    setDeleteConfirm(null)
  }, [])

  const sortedBoosts = useMemo(
    () => sortBoosts(boosts, sortColumn, sortDirection),
    [boosts, sortColumn, sortDirection]
  )

  const handleViewChange = useCallback((newView: ListView) => {
    setView(newView)
  }, [])

  const handleSegmentChange = useCallback(
    (filter: readonly string[] | undefined) => {
      setActiveBoostTypeFilter(filter ? [...filter] : undefined)
      trackEvent(ANALYTICS_EVENTS.BOOSTS_FILTER_CHANGED, {
        filter: filter ? 'transfer_savings' : 'all',
      })
    },
    [trackEvent]
  )

  const handleTestBoost = useCallback(
    (boost: Boost) => {
      testMutation.mutate({ id: boost.id, data: {} })
    },
    [testMutation]
  )

  // Error state
  if (error) {
    return (
      <div className="mx-auto max-w-[1200px] p-6">
        <div className="flex flex-col items-center justify-center px-6 py-16 text-center">
          <AlertCircle className="mb-4 h-12 w-12 text-destructive" />
          <h2 className="mb-2 text-xl font-semibold text-foreground">Error loading boosts</h2>
          <p className="mb-6 text-muted-foreground">There was a problem loading your boosts.</p>
          <Button
            variant="outline"
            onClick={() => refetch()}
            aria-label="Retry loading boosts"
          >
            Retry
          </Button>
        </div>
      </div>
    )
  }

  // Boost-type segment: shown on Boosts → Boosts tab (#1267 R8-1c).
  // Defined before the empty-state early-return so it is always reachable, even when
  // the filtered query returns zero results (R10-a: prevents orphaning non-transfer boosts).
  const boostTypeSegment = showTypeSegment ? (
    <div
      role="group"
      aria-label="Boost type filter"
      className="mb-4 flex gap-2"
    >
      <Button
        variant={activeBoostTypeFilter ? 'default' : 'outline'}
        size="sm"
        onClick={() => handleSegmentChange(boostTypeFilter)}
      >
        Transfer &amp; Savings
      </Button>
      <Button
        variant={!activeBoostTypeFilter ? 'default' : 'outline'}
        size="sm"
        onClick={() => handleSegmentChange(undefined)}
      >
        All boosts
      </Button>
    </div>
  ) : null

  // Empty state (guarded: during initial load, show skeleton in table instead)
  if (!isLoading && boosts.length === 0) {
    return (
      <div className="mx-auto max-w-[1200px] p-6">
        {boostTypeSegment}
        <header className="mb-6 flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
          <h1 className="text-2xl font-semibold text-foreground">Automation Boosts</h1>
        </header>
        <EmptyState
          icon={<Zap />}
          title={
            activeBoostTypeFilter
              ? 'No transfer or savings boosts'
              : 'No automation boosts yet'
          }
          description={
            activeBoostTypeFilter
              ? 'You have no transfer suggestion or savings goal boosts. Switch to "All boosts" to see your other boost types, or create a new one.'
              : 'Create your first automation boost to automatically manage your budgets, transfers, and savings.'
          }
          action={
            <ReadOnlyGuard>
              <Button onClick={handleCreateBoost}>
                <Plus />
                Create Your First Boost
              </Button>
            </ReadOnlyGuard>
          }
        />
        {/*
          Issue #1569: deliberately rendered in the EMPTY branch too. "Every boost I had is
          deleted" is exactly the state in which the retained execution history is the only thing
          left to look at, and it is the state this early return produces.
        */}
        <BoostAutomationHistoryPanel />
      </div>
    )
  }

  return (
    <div className="mx-auto max-w-[1200px] p-6">
      {boostTypeSegment}

      <header className="mb-6 flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex items-center gap-4">
          <h1 className="text-2xl font-semibold text-foreground">Automation Boosts</h1>
          <Badge variant="secondary">
            {boosts.length} {boosts.length === 1 ? 'boost' : 'boosts'}
          </Badge>
        </div>

        <div className="flex items-center gap-4">
          <ListViewToggle
            view={view}
            onChange={handleViewChange}
            storageKey={STORAGE_KEY_LIST_VIEW}
          />
          <ReadOnlyGuard>
            <Button
              onClick={handleCreateBoost}
              aria-label="Create new boost"
            >
              <Plus aria-hidden="true" />
              Create Boost
            </Button>
          </ReadOnlyGuard>
        </div>
      </header>

      <div className="min-h-[200px]">
        {view === 'cards' ? (
          isLoading && boosts.length === 0 ? (
            <div role="status">
              <span className="sr-only">Loading boosts…</span>
              <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3" aria-hidden="true">
                {Array.from({ length: 3 }).map((_, i) => (
                  <div key={i} className="h-48 rounded-lg bg-skeleton motion-safe:animate-pulse motion-reduce:animate-none" />
                ))}
              </div>
            </div>
          ) : (
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
              {boosts.map((boost) => (
                <BoostCard
                  key={boost.id}
                  boost={boost}
                  onEdit={handleEditBoost}
                  onDelete={handleDeleteClick}
                  onToggle={handleToggleBoost}
                  onTest={handleTestBoost}
                  onViewExecutions={(b) => navigate(`/boosts/${b.id}`)}
                  loading={toggleMutation.isPending || deleteMutation.isPending || testMutation.isPending}
                />
              ))}
            </div>
          )
        ) : (
          <BoostTable
            boosts={sortedBoosts}
            onEdit={handleEditBoost}
            onDelete={handleDeleteClick}
            onToggle={handleToggleBoost}
            onTest={handleTestBoost}
            onRowClick={(boost) => navigate(`/boosts/${boost.id}`)}
            loading={isLoading || toggleMutation.isPending || deleteMutation.isPending || testMutation.isPending}
            sort={sort}
            onSortChange={setSort}
          />
        )}
      </div>

      {/* Delete Confirmation Dialog — Issue #960 */}
      <BoostAutomationHistoryPanel />

      {/*
        Issue #1569: `boostsRepository.Delete` tombstones the boost (`deleted_at = NOW()`) and
        touches no execution row, so the runs are kept. This dialog used to be silent about that.
        It can now name where they are read, because BoostAutomationHistoryPanel — rendered just
        above — actually reaches them; naming a surface that did not exist would have replaced one
        false claim with another.

        "This action cannot be undone" stays: it is separately TRUE. Nothing in this system sets
        `deleted_at = NULL`, so retained is not recoverable and the copy promises no restore.
      */}
      <ConfirmationDialog
        open={deleteConfirm !== null}
        onOpenChange={(open) => !open && handleDeleteCancel()}
        onConfirm={handleDeleteConfirm}
        title="Delete Boost"
        description={deleteConfirm ? (
          <>Are you sure you want to delete &ldquo;<span data-ph-mask>{deleteConfirm.name}</span>&rdquo;? It stops running and leaves your list, but nothing is thrown away &mdash; every run it already made is kept, and you can still read them under &ldquo;Automation history&rdquo; below. This action cannot be undone.</>
        ) : ''}
        variant="destructive"
        loading={deleteMutation.isPending}
      />
    </div>
  )
}
