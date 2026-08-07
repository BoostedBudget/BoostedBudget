import { useEffect, useState, type ComponentType } from 'react'
import { AlertTriangle, CheckCircle, Info, RefreshCw, Unplug, UserCheck } from 'lucide-react'
import { toast } from 'sonner'
import {
  useListAdminPlaidOrphanedItems,
  useRecordAdminPlaidOrphanedItemOperatorResolution,
} from '@boostedbudget/api-client/endpoints/admin/admin'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { EmptyState } from '@/components/ui/empty-state'
import { cn } from '@/lib/utils'
import { DataTable, TablePagination, useTablePreferences } from '@/shared/components/DataTable'
import type { ColumnDef } from '@/shared/components/DataTable'
import { mutationFailureMessage } from '../lib/mutationFailureMessage'
import { SummaryCard } from './SummaryCard'

/**
 * PlaidOrphanedItemsPage — the operator surface for `plaid_orphaned_items` (Issue #1497).
 *
 * A row here is a Plaid Item that may still exist, and may still bill, whose local record was
 * destroyed by a completed GDPR erasure before its removal could be proved. The table carries no
 * user id and no user PII by design (migration 000134) — it has to outlive the person it concerns.
 *
 * ⚠ THE SCREEN'S WHOLE JOB IS KEEPING TWO STATES APART. `auto_retrying` needs nobody;
 * `manual_intervention_required` needs a human in Plaid's own dashboard. Conflating them is how an
 * Item silently rots behind a green label, so they differ in wording AND icon AND colour — never
 * colour alone (WCAG 1.4.1), and never a shared neutral label.
 *
 * ⚠ THE STATE IS THE SERVER'S, NOT OURS. `admin_state` is derived by domain.PlaidOrphanedItem
 * .AdminState against one `as_of` instant for the whole response, and it DECAYS a row into manual
 * intervention when no retry has been observed for domain.OrphanedItemRetryStaleAfter. Recomputing
 * it in the browser from the timestamps would quietly undo that decay and hide real work, so this
 * component only ever renders what came back — including states it does not recognise.
 *
 * ⚠ NO RE-ATTEMPT BUTTON, DELIBERATELY, and Issue #1497 item 2 adding a write did not change that.
 * The retry sweep already re-attempts every unresolved record that still holds a live credential on
 * its own cadence, and a record past its retention deadline has nothing left to retry with — which
 * is exactly what `manual_intervention_required` is telling the operator. A button that could only
 * ever be a duplicate or a no-op would imply the system can still act when it cannot.
 *
 * ⚠ THE ONE ACTION THAT EXISTS IS THE OPPOSITE SHAPE: "I removed this at Plaid" records something
 * only the human knows. It is an UNVERIFIED ATTESTATION and the UI must keep saying so — it does not
 * set `resolved_at` (which means Plaid itself confirmed the removal), does not destroy the retained
 * credential, and does not stop the automated sweep. So `operator_attested` renders as its own state,
 * never merged into `Resolved`, and the attested row stays in the table with the attester, the
 * instant and the note visible: an attestation whose evidence is hidden is an anonymous green tick,
 * which is the thing this whole feature exists not to produce.
 */

// eslint-disable-next-line react-refresh/only-export-components
export const ADMIN_ORPHANED_PLAID_COLUMN_IDS = [
  'plaid_item_id',
  'institution',
  'admin_state',
  'erasure_error_code',
  'last_retry_error_code',
  'retry_attempt_count',
  'orphaned_at',
  'credential',
  'token_expires_at',
  'operator_resolution',
  'actions',
] as const

const PAGE_SIZE = 50

type StateMeta = {
  label: string
  variant: 'destructive' | 'info' | 'success' | 'warning'
  Icon: ComponentType<{ size?: number; className?: string; 'aria-hidden'?: boolean }>
}

// Keys are domain.OrphanedItemAdminState's wire values. Anything else falls through to the
// unknown branch below rather than being guessed at.
const STATE_META: Record<string, StateMeta> = {
  manual_intervention_required: {
    label: 'Manual intervention required',
    variant: 'destructive',
    Icon: AlertTriangle,
  },
  auto_retrying: { label: 'Auto-retrying', variant: 'info', Icon: RefreshCw },
  resolved: { label: 'Resolved', variant: 'success', Icon: CheckCircle },
  // ⚠ NOT 'success' AND NOT THE SAME ICON AS `resolved`. This is a colleague's word, not Plaid's
  // answer, and the wording says "unverified" out loud so the two are told apart by TEXT and ICON as
  // well as by colour — WCAG 1.4.1, and also plain honesty on a compliance surface.
  operator_attested: {
    label: 'Operator-attested (unverified)',
    variant: 'warning',
    Icon: UserCheck,
  },
}

function StateBadge({ state }: { state: string }) {
  const meta = STATE_META[state]
  if (!meta) {
    // An unrecognised state is reported verbatim. Defaulting it to either real state would be a
    // guess about whether a human is needed — the one thing this screen must never guess.
    return (
      <Badge variant="outline" className="gap-1.5 whitespace-normal">
        <Info size={14} aria-hidden={true} />
        {state || 'unknown'}
      </Badge>
    )
  }
  const { label, variant, Icon } = meta
  return (
    <Badge variant={variant} className="gap-1.5 whitespace-normal text-left">
      <Icon size={14} aria-hidden={true} />
      {label}
    </Badge>
  )
}

const buttonClass =
  'inline-flex items-center gap-1.5 px-3.5 py-2 border border-border rounded-md bg-background text-foreground text-sm font-medium cursor-pointer transition-colors hover:bg-muted disabled:opacity-45 disabled:cursor-not-allowed'

function formatInstant(value: string | undefined): string {
  if (!value) return '—'
  const parsed = new Date(value)
  return Number.isNaN(parsed.getTime()) ? value : parsed.toLocaleString()
}

export function PlaidOrphanedItemsPage() {
  const [page, setPage] = useState(1)
  // The record currently being attested, plus the note being written for it. Held here rather than in
  // the dialog so the dialog is a pure form and the row identity cannot drift from what gets POSTed.
  const [attestingId, setAttestingId] = useState<string | null>(null)
  const [note, setNote] = useState('')
  const [noteError, setNoteError] = useState<string | null>(null)
  const trackEvent = useTrackEvent()

  useEffect(() => {
    trackEvent(ANALYTICS_EVENTS.ADMIN_PAGE_VIEWED, { page: 'plaid-orphaned-items' })
  }, [trackEvent])

  const offset = (page - 1) * PAGE_SIZE
  const { data, isLoading, error, refetch } = useListAdminPlaidOrphanedItems({
    limit: PAGE_SIZE,
    offset,
  })
  const attestMutation = useRecordAdminPlaidOrphanedItemOperatorResolution()

  const closeAttestDialog = () => {
    setAttestingId(null)
    setNote('')
    setNoteError(null)
  }

  const submitAttestation = () => {
    const recordId = attestingId
    if (!recordId) return
    // A note is REQUIRED, and refusing here rather than letting the server 422 is not mere
    // convenience: an attestation is accepted on no evidence at all, so the note is the only thing
    // that makes it checkable later. The server enforces the same rule — this is the message that
    // explains WHY, next to the field.
    const trimmed = note.trim()
    if (!trimmed) {
      setNoteError('Say what you actually did at Plaid — this attestation is unverified, so the note is the only way anyone can check it later.')
      return
    }
    setNoteError(null)

    attestMutation.mutate(
      { id: recordId, data: { note: trimmed } },
      {
        onSuccess: () => {
          trackEvent(ANALYTICS_EVENTS.ADMIN_ORPHANED_PLAID_ITEM_ATTESTED, { record_id: recordId })
          toast.success('Attestation recorded. The record now shows your claim, and the automated sweep keeps checking it.')
          closeAttestDialog()
          // The row's state, the note and the four page counts all change, and the mutation refetches
          // none of them. Without this the operator stares at the state they just acted on.
          void refetch()
        },
        // ⚠ onError IS NOT OPTIONAL. Issue #1496's UI half was exactly this: a mutation wired with
        // onSuccess only, so a rejected write said nothing and the operator believed it had worked.
        // The reachable rejections here are the 409 (already attested — the first attestation stands
        // and THIS note was discarded) and the 404. The dialog stays OPEN on failure, because the
        // note is still unsaved and closing it would throw away what they wrote.
        onError: (err) => {
          const message = mutationFailureMessage(err)
          setNoteError(message)
          toast.error(`Could not record the attestation: ${message}`)
        },
      }
    )
  }

  const rows = data?.items ?? []
  const total = data?.total ?? 0
  const totalPages = total > 0 ? Math.ceil(total / PAGE_SIZE) : 0
  const hasMore = offset + rows.length < total
  const isEmpty = !isLoading && !error && rows.length === 0

  const { sizing, onSizingChange, columnVisibility, onColumnVisibilityChange } =
    useTablePreferences('admin-plaid-orphaned-items', [...ADMIN_ORPHANED_PLAID_COLUMN_IDS])

  type OrphanRow = (typeof rows)[0]

  const columns: ColumnDef<OrphanRow>[] = [
    {
      id: 'plaid_item_id',
      header: 'Plaid Item ID',
      resizable: true,
      defaultSize: 220,
      minSize: 160,
      render: (item) => (
        <span className="font-mono text-[0.8125rem] font-medium break-all">{item.plaid_item_id}</span>
      ),
    },
    {
      id: 'institution',
      header: 'Institution',
      resizable: true,
      defaultSize: 200,
      minSize: 140,
      render: (item) => (
        <span className="text-[0.8125rem]">
          {item.institution_name || item.institution_id || '—'}
        </span>
      ),
    },
    {
      id: 'admin_state',
      header: 'Action required',
      defaultSize: 240,
      minSize: 180,
      render: (item) => <StateBadge state={item.admin_state} />,
    },
    {
      id: 'erasure_error_code',
      header: 'Erasure error',
      hideable: true,
      defaultSize: 200,
      minSize: 140,
      render: (item) => (
        <span className="font-mono text-[0.8125rem] text-muted-foreground break-all">
          {item.erasure_error_code || '—'}
        </span>
      ),
    },
    {
      id: 'last_retry_error_code',
      header: 'Last retry error',
      hideable: true,
      defaultSize: 220,
      minSize: 140,
      render: (item) => (
        <span className="font-mono text-[0.8125rem] text-muted-foreground break-all">
          {item.last_retry_error_code || '—'}
        </span>
      ),
    },
    {
      id: 'retry_attempt_count',
      header: 'Attempts',
      align: 'right',
      numeric: true,
      defaultSize: 110,
      minSize: 80,
      maxSize: 140,
      render: (item) => <span className="text-[0.8125rem]">{item.retry_attempt_count}</span>,
    },
    {
      id: 'orphaned_at',
      header: 'Orphaned',
      defaultSize: 180,
      minSize: 140,
      render: (item) => (
        <span className="text-[0.8125rem] whitespace-nowrap">{formatInstant(item.orphaned_at)}</span>
      ),
    },
    {
      id: 'credential',
      header: 'Credential',
      defaultSize: 200,
      minSize: 140,
      // "Retained" answers the only question a human has about the stored token — is anything still
      // able to try? It reports a credential that is STORED, including one this deployment cannot
      // decrypt, because understating what the system holds is the one direction a compliance
      // surface must never err in. The token itself is never returned by the API.
      render: (item) =>
        item.token_retained ? (
          <span className="text-[0.8125rem]" title="A credential is still stored for this Item.">
            Retained
          </span>
        ) : item.token_purged_at ? (
          <span
            className="text-[0.8125rem] whitespace-nowrap text-muted-foreground"
            title="The stored credential was destroyed by the purge sweep."
          >
            {`Purged ${formatInstant(item.token_purged_at)}`}
          </span>
        ) : (
          <span
            className="text-[0.8125rem] text-muted-foreground"
            title="No credential was ever stored for this Item."
          >
            None held
          </span>
        ),
    },
    {
      id: 'token_expires_at',
      header: 'Unusable from',
      defaultSize: 190,
      minSize: 150,
      // ⚠ WORDING IS LOAD-BEARING — see domain.OrphanedItemTokenRetention. The deadline guarantees
      // two things at different strengths: the credential is UNUSABLE from this exact instant
      // (enforced at every point of use, independent of any sweep), while its EXISTENCE ends at the
      // first purge sweep at or after it — normally within an hour, but that is a bound per sweep,
      // not an absolute ceiling. So this column is "Unusable from", never "Deleted at"; and it shows
      // the instant itself, never a countdown, which would imply an event scheduled for that moment.
      render: (item) => (
        <span
          className="text-[0.8125rem] whitespace-nowrap"
          title="The credential is unusable from this instant, whatever else has run. The stored copy is destroyed by the first purge sweep at or after it — normally within the hour, though a sweep that does not run extends that."
        >
          {formatInstant(item.token_expires_at)}
        </span>
      ),
    },
    {
      id: 'operator_resolution',
      header: 'Operator attestation',
      hideable: true,
      resizable: true,
      defaultSize: 280,
      minSize: 180,
      // ⚠ THE EVIDENCE IS SHOWN, NOT SUMMARISED AWAY. An attestation is accepted on no proof, so the
      // only thing that makes it checkable is WHO said it, WHEN, and WHAT they said — a bare tick
      // would be the anonymous green label this feature exists to prevent. The note is rendered in
      // full (the API caps it at 2,000 characters) rather than truncated to an ellipsis.
      render: (item) =>
        item.operator_resolved_at ? (
          <span className="flex flex-col gap-0.5 text-[0.8125rem]">
            <span className="font-medium">
              {item.operator_resolved_by || 'unattributed'}
            </span>
            <span className="text-muted-foreground whitespace-nowrap">
              {formatInstant(item.operator_resolved_at)}
            </span>
            {item.operator_resolution_note && (
              <span className="text-muted-foreground break-words">
                {item.operator_resolution_note}
              </span>
            )}
          </span>
        ) : (
          <span className="text-[0.8125rem] text-muted-foreground">—</span>
        ),
    },
    {
      id: 'actions',
      header: 'Action',
      defaultSize: 210,
      minSize: 170,
      // Offered ONLY where it can mean something: a row Plaid has already confirmed removed needs no
      // human claim, and a row that already carries one is first-wins, so a second click could only
      // ever produce a 409 and discard the note. Absent beats a button that cannot succeed.
      render: (item) =>
        item.admin_state === 'resolved' || item.operator_resolved_at ? (
          <span className="text-[0.8125rem] text-muted-foreground">—</span>
        ) : (
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => {
              setAttestingId(item.id)
              setNote('')
              setNoteError(null)
            }}
          >
            I removed this at Plaid
          </Button>
        ),
    },
  ]

  return (
    <div className="flex flex-col gap-6 p-6 min-h-full">
      <header className="flex items-start justify-between flex-wrap gap-4 border-b border-border pb-4">
        <div>
          <h1 className="text-2xl font-bold text-foreground flex items-center gap-2.5 m-0">
            <Unplug size={22} className="text-warning" aria-hidden={true} />
            Orphaned Plaid Items
            {total > 0 && (
              <span className="text-sm font-medium text-muted-foreground bg-muted border border-border rounded-full px-2.5 py-0.5">
                {total} records
              </span>
            )}
          </h1>
          <p className="text-sm text-muted-foreground mt-1.5 mb-0 max-w-[70ch]">
            Plaid Items whose local record was destroyed by a completed GDPR erasure before removal
            could be proved.{' '}
            <strong className="text-foreground font-semibold">Manual intervention required</strong>{' '}
            means nothing automated will finish the job — remove the Item in Plaid&rsquo;s dashboard
            using its Plaid Item ID, or it may keep billing.{' '}
            <strong className="text-foreground font-semibold">Auto-retrying</strong> means a stored
            credential and the retry sweep are still working on it; acting now would duplicate work
            in flight. If you do remove an Item by hand, record it with{' '}
            <strong className="text-foreground font-semibold">I removed this at Plaid</strong> — that
            is an <em>unverified</em> attestation kept separate from{' '}
            <strong className="text-foreground font-semibold">Resolved</strong>, which means Plaid
            itself confirmed the removal.
          </p>
        </div>
        <div className="flex items-center gap-2 shrink-0">
          <button type="button" className={buttonClass} onClick={() => void refetch()}>
            <RefreshCw size={16} aria-hidden={true} />
            Refresh
          </button>
        </div>
      </header>

      {data && (
        <>
          <div className="grid gap-4 [grid-template-columns:repeat(auto-fit,minmax(200px,1fr))]">
            <div data-testid="orphan-count-manual">
              <SummaryCard
                title="Manual intervention required"
                value={String(data.manual_intervention_required)}
                icon={<AlertTriangle size={24} />}
                status={data.manual_intervention_required > 0 ? 'error' : 'success'}
              />
            </div>
            <div data-testid="orphan-count-auto">
              <SummaryCard
                title="Auto-retrying"
                value={String(data.auto_retrying)}
                icon={<RefreshCw size={24} />}
                status="neutral"
              />
            </div>
            <div data-testid="orphan-count-attested">
              {/* ⚠ ITS OWN CARD, NEVER ADDED INTO "Resolved". One of these two numbers is what Plaid
                  confirmed; the other is what a colleague said. A single "done" figure would be a
                  number whose accuracy nobody could state, on the surface whose job is to say what is
                  actually outstanding. Status is neutral, not success: nothing has been verified. */}
              <SummaryCard
                title="Operator-attested (unverified)"
                value={String(data.operator_attested ?? 0)}
                icon={<UserCheck size={24} />}
                status="neutral"
              />
            </div>
            <div data-testid="orphan-count-resolved">
              <SummaryCard
                title="Resolved (proved at Plaid)"
                value={String(data.resolved)}
                icon={<CheckCircle size={24} />}
                status="success"
              />
            </div>
          </div>
          {/* The API counts the records IN THIS PAGE, not the whole table — saying so keeps a paged
              slice from being read as a total. `as_of` is the single instant every state above was
              derived against. */}
          <p className="text-xs text-muted-foreground m-0">
            Counts describe the {rows.length} record{rows.length === 1 ? '' : 's'} on this page, as
            of {formatInstant(data.as_of)}.
          </p>
        </>
      )}

      {isLoading ? (
        <div
          className="flex flex-col items-center justify-center gap-3 py-16 text-muted-foreground"
          role="status"
        >
          Loading orphaned Plaid Items...
        </div>
      ) : error ? (
        <div className={cn('flex items-center gap-3 py-16 justify-center text-destructive')} role="alert">
          <AlertTriangle size={20} aria-hidden={true} />
          <span>Failed to load orphaned Plaid Items. Please try again.</span>
        </div>
      ) : isEmpty ? (
        <EmptyState
          size="compact"
          icon={<Unplug />}
          title="No orphaned Plaid Items"
          description="Every erasure so far has been able to prove its Plaid Items were removed."
          className="py-16"
        />
      ) : (
        <>
          <DataTable
            data={rows}
            columns={columns}
            rowKey={(item) => item.id}
            sort={null}
            onSortChange={() => {}}
            filters={{}}
            onFiltersChange={() => {}}
            loading={isLoading}
            // The Plaid Item id is a vendor resource identifier and the institution names an erased
            // person's bank — both are masked for PostHog session replay.
            maskedColumnIds={['plaid_item_id', 'institution']}
            tableId="admin-plaid-orphaned-items"
            columnSizing={sizing}
            onColumnSizingChange={onSizingChange}
            columnVisibility={columnVisibility}
            onColumnVisibilityChange={onColumnVisibilityChange}
          />
          <TablePagination
            currentPage={page}
            totalPages={totalPages}
            hasMore={hasMore}
            onPageChange={setPage}
          />
        </>
      )}

      <Dialog
        open={attestingId !== null}
        onOpenChange={(open) => {
          if (!open) closeAttestDialog()
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Record that you removed this Item at Plaid</DialogTitle>
          </DialogHeader>

          {/* ⚠ THE WORDING IS THE FEATURE. An operator clicking this is being asked to understand
              that they are filing a claim, not closing a ticket: nothing verifies it, the credential
              is untouched, and the automated sweep keeps trying — which is what can eventually turn
              the claim into a confirmed removal. Softening any of that would make the button feel
              like "mark done", which is precisely the conflation the separate columns exist for. */}
          <p className="text-sm text-muted-foreground m-0">
            This is an <strong className="text-foreground font-semibold">unverified</strong>{' '}
            attestation recorded against your admin identity. It is deliberately{' '}
            <em>not</em> the same as <strong className="text-foreground font-semibold">Resolved</strong>,
            which means Plaid itself confirmed the removal. It does not destroy the stored credential,
            and the automated retry sweep keeps re-attempting this Item — that is the only thing that
            can ever confirm what you are recording here.
          </p>

          <label className="flex flex-col gap-1.5 text-sm font-medium text-foreground">
            What you did at Plaid
            <textarea
              className="min-h-24 rounded-md border border-border bg-background p-2.5 text-sm text-foreground"
              value={note}
              maxLength={2000}
              data-ph-mask
              onChange={(e) => {
                setNote(e.target.value)
                setNoteError(null)
              }}
            />
          </label>

          {noteError && (
            <p className="text-sm text-destructive m-0" role="alert">
              {noteError}
            </p>
          )}

          <DialogFooter>
            <Button type="button" variant="outline" onClick={closeAttestDialog}>
              Cancel
            </Button>
            <Button type="button" onClick={submitAttestation} disabled={attestMutation.isPending}>
              Record attestation
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
