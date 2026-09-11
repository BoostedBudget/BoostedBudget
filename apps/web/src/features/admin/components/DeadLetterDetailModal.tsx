import { useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { ConfirmationDialog } from '@/components/ui/confirmation-dialog'
import { Loader2 } from 'lucide-react'
import { cn } from '@/lib/utils'
import { useTrackEvent } from '../../../shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '../../../shared/analytics'
import {
  useGetAdminDeadLetterJobDetail,
  useGetAdminDeadLetterJobArgs,
  useRetryAdminDeadLetterJob,
  useAddAdminDeadLetterJobNotes,
  useDeleteAdminDeadLetterJob,
  getListAdminDeadLetterJobsQueryKey,
  getGetAdminDeadLetterJobStatsQueryKey,
  getGetAdminDeadLetterJobDetailQueryKey,
} from '@boostedbudget/api-client/endpoints/admin/admin'
import { mutationFailureMessage } from '../lib/mutationFailureMessage'
import { JobErrorList } from './JobErrorList'

interface DeadLetterDetailModalProps {
  jobId: number | null
  onClose: () => void
}

const formatJson = (value: unknown): string => {
  if (value === null || value === undefined) return ''
  try {
    return JSON.stringify(typeof value === 'string' ? JSON.parse(value) : value, null, 2)
  } catch {
    return String(value)
  }
}

const fieldClass = 'text-[0.8125rem] font-semibold text-muted-foreground min-w-[120px] shrink-0'
const valueClass = 'text-sm text-foreground break-all'
const preClass = 'bg-muted border border-border rounded-md px-4 py-3.5 text-[0.8125rem] font-mono overflow-x-auto whitespace-pre max-h-[200px] overflow-y-auto'

export function DeadLetterDetailModal({ jobId, onClose }: DeadLetterDetailModalProps) {
  const [notesValue, setNotesValue] = useState('')
  const [isEditingNotes, setIsEditingNotes] = useState(false)
  const [showRawArgs, setShowRawArgs] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const trackEvent = useTrackEvent()

  const { data: job, isLoading, error } = useGetAdminDeadLetterJobDetail(jobId ?? 0, {
    query: { enabled: !!jobId },
  })

  const { data: rawArgsData, isLoading: isLoadingArgs, refetch: fetchArgs } = useGetAdminDeadLetterJobArgs(
    jobId ?? 0,
    { query: { enabled: false } }
  )

  const queryClient = useQueryClient()
  const retryMutation = useRetryAdminDeadLetterJob()
  const notesMutation = useAddAdminDeadLetterJobNotes()
  const deleteMutation = useDeleteAdminDeadLetterJob()

  const handleRevealArgs = () => {
    setShowRawArgs(true)
    fetchArgs()
  }

  // Issue #1496: this used to pass `onSuccess` only, so a rejected replay left the modal looking
  // exactly like a successful one. The reachable rejections are the 409 (deduplicated away by the
  // kind's uniqueness options, so nothing was enqueued) and the 422 (unknown job kind); the
  // already-retried case can't get here because the button is disabled and relabelled.
  const handleRetry = () => {
    if (!jobId) return
    retryMutation.mutate({ id: jobId }, {
      onSuccess: () => {
        trackEvent(ANALYTICS_EVENTS.ADMIN_DLQ_RETRY, { job_id: jobId })
        toast.success(`Job #${jobId} re-enqueued`)
        // Refetch what a successful replay changes: this job's retried_at/retry_job_id (so the
        // footer button flips to "Already Retried" instead of inviting a second click), plus the
        // list row and the retried/unretried counts behind the modal.
        void queryClient.invalidateQueries({ queryKey: getGetAdminDeadLetterJobDetailQueryKey(jobId) })
        void queryClient.invalidateQueries({ queryKey: getListAdminDeadLetterJobsQueryKey() })
        void queryClient.invalidateQueries({ queryKey: getGetAdminDeadLetterJobStatsQueryKey() })
      },
      onError: (err) => {
        toast.error(`Failed to retry job #${jobId}: ${mutationFailureMessage(err)}`)
      },
    })
  }

  // Issue #1496: this used to pass `onSuccess` only, so a rejected save left the modal looking
  // as if nothing happened. Unlike delete, a failed save-notes keeps the editor open on
  // failure — closing it would discard the operator's unsaved text (the textarea only shows
  // `notesValue`; the read-only view underneath falls back to the last-saved `job.notes`).
  const handleSaveNotes = () => {
    if (!jobId) return
    notesMutation.mutate(
      { id: jobId, data: { notes: notesValue } },
      {
        onSuccess: () => {
          toast.success('Notes saved')
          setIsEditingNotes(false)
          // Without this the editor closes onto the read-only view, which renders the stale
          // `job.notes` from the comment above — the detail query has no `refetchInterval`,
          // `staleTime` is 5 minutes and `refetchOnWindowFocus` is false (main.tsx), so the
          // operator sees the PREVIOUS note (or "No notes yet") and retypes what they just saved.
          // The list row behind the modal carries a `notes` column and is stale for the same
          // reason. The stats counts are untouched by a note, so they are deliberately not
          // invalidated here.
          void queryClient.invalidateQueries({ queryKey: getGetAdminDeadLetterJobDetailQueryKey(jobId) })
          void queryClient.invalidateQueries({ queryKey: getListAdminDeadLetterJobsQueryKey() })
        },
        onError: (err) => {
          toast.error(`Failed to save notes: ${mutationFailureMessage(err)}`)
        },
      }
    )
  }

  const closeModal = () => {
    setShowRawArgs(false)
    setIsEditingNotes(false)
    setConfirmDelete(false)
    onClose()
  }

  // Issue #1496: this used to pass `onSuccess` only, so a rejected delete left the confirmation
  // sub-dialog open with no explanation — the same defect as DeadLetterPage's per-row delete.
  // On failure, only the confirm sub-dialog is dismissed (`setConfirmDelete(false)`); the
  // detail modal itself stays open (no `closeModal()`, no invalidation) because the job
  // still exists and the operator needs to see the error toast against it, not have the whole
  // view yanked away.
  const handleDelete = () => {
    if (!jobId) return
    deleteMutation.mutate({ id: jobId }, {
      onSuccess: () => {
        trackEvent(ANALYTICS_EVENTS.ADMIN_DLQ_DELETE, { job_id: jobId })
        toast.success(`Job #${jobId} deleted`)
        // Mirrors the retry success path above: the deleted job no longer belongs in the list
        // or the stats counts, and its detail query should stop being cached. Without this the
        // page's 30s poll is the only thing that ever removes the row.
        void queryClient.invalidateQueries({ queryKey: getGetAdminDeadLetterJobDetailQueryKey(jobId) })
        void queryClient.invalidateQueries({ queryKey: getListAdminDeadLetterJobsQueryKey() })
        void queryClient.invalidateQueries({ queryKey: getGetAdminDeadLetterJobStatsQueryKey() })
        closeModal()
      },
      onError: (err) => {
        toast.error(`Failed to delete job #${jobId}: ${mutationFailureMessage(err)}`)
        setConfirmDelete(false)
      },
    })
  }

  const handleOpen = (open: boolean) => {
    if (!open) closeModal()
  }

  return (
    <>
    <Dialog open={!!jobId} onOpenChange={handleOpen}>
      <DialogContent className="max-w-[720px] max-h-[85vh] overflow-y-auto">
        {isLoading ? (
          <div className="flex flex-col items-center justify-center py-12 gap-3 text-muted-foreground">
            <Loader2 className="animate-spin" />
            <span>Loading job details...</span>
          </div>
        ) : error ? (
          <div className="flex items-center justify-center py-12 text-destructive">
            Failed to load job details.
          </div>
        ) : job ? (
          <>
            <DialogHeader>
              <DialogTitle className="font-mono">{job.kind}</DialogTitle>
            </DialogHeader>

            <div className="flex flex-col gap-3.5 my-4">
              <div className="flex items-start gap-2">
                <span className={fieldClass}>ID:</span>
                <span className={valueClass}>{job.id}</span>
              </div>
              <div className="flex items-start gap-2">
                <span className={fieldClass}>Queue:</span>
                <span className={cn(valueClass, 'font-mono')}>{job.queue}</span>
              </div>
              {job.user_id && (
                <div className="flex items-start gap-2">
                  <span className={fieldClass}>User ID:</span>
                  <span className={cn(valueClass, 'font-mono')} data-ph-mask>{job.user_id}</span>
                </div>
              )}
              <div className="flex items-start gap-2">
                <span className={fieldClass}>Max Attempts:</span>
                <span className={valueClass}>{job.max_attempts}</span>
              </div>
              <div className="flex items-start gap-2">
                <span className={fieldClass}>Failed At:</span>
                <span className={valueClass}>{new Date(job.failed_at).toLocaleString()}</span>
              </div>
              <div className="flex items-start gap-2">
                <span className={fieldClass}>Status:</span>
                <Badge variant={job.retried_at ? 'success' : 'destructive'}>
                  {job.retried_at ? 'Retried' : 'Pending Retry'}
                </Badge>
              </div>
              {job.retried_at && (
                <div className="flex items-start gap-2">
                  <span className={fieldClass}>Retried At:</span>
                  <span className={valueClass}>{new Date(job.retried_at).toLocaleString()}</span>
                </div>
              )}
              {job.retry_job_id && (
                <div className="flex items-start gap-2">
                  <span className={fieldClass}>Retry Job ID:</span>
                  <span className={cn(valueClass, 'font-mono')}>{job.retry_job_id}</span>
                </div>
              )}

              <div className="flex flex-col gap-2 mt-2">
                <span className={fieldClass}>Errors:</span>
                {/*
                  #1631: this was a single JSON dump of the whole column, which flattened the
                  wrapped error chain into one line and never surfaced the Postgres DETAIL —
                  the field that answered the motivating holdings-sync investigation. JobErrorList
                  renders old {attempt, message, timestamp} rows unchanged and masks every SQL
                  free-text field it shows; see its header comment.
                */}
                <JobErrorList errors={job.errors} />
              </div>

              <div className="flex flex-col gap-2 mt-2">
                <div className="flex items-center justify-between">
                  <span className={fieldClass}>Args:</span>
                  {!showRawArgs && (
                    <button
                      type="button"
                      className="text-xs text-primary hover:underline font-medium"
                      onClick={handleRevealArgs}
                    >
                      Reveal
                    </button>
                  )}
                </div>
                {showRawArgs ? (
                  isLoadingArgs ? (
                    <div className="flex items-center gap-2 text-sm text-muted-foreground py-2">
                      <Loader2 size={14} className="animate-spin" />
                      Loading raw args...
                    </div>
                  ) : (
                    <pre className={preClass} data-ph-mask>
                      {formatJson(rawArgsData) || '(empty)'}
                    </pre>
                  )
                ) : (
                  // Server-side masking (services.MaskJobArgs) only redacts a fixed denylist of
                  // TOP-LEVEL string keys — real job args carry user_id, clerk_user_id,
                  // linked_item_id and free-text `reason` that survive it. So the "masked" view
                  // is not PII-free and must be masked in replay just like the raw view above.
                  <pre className={cn(preClass, 'text-muted-foreground')} data-ph-mask>
                    {formatJson(job.args) || '[REDACTED]'}
                  </pre>
                )}
              </div>

              <div className="flex flex-col gap-2 mt-2">
                <div className="flex items-center justify-between">
                  <span className={fieldClass}>Notes:</span>
                  {!isEditingNotes && (
                    <button
                      type="button"
                      className="text-xs text-primary hover:underline font-medium"
                      onClick={() => {
                        setNotesValue(job.notes ?? '')
                        setIsEditingNotes(true)
                      }}
                    >
                      {job.notes ? 'Edit' : 'Add'}
                    </button>
                  )}
                </div>
                {isEditingNotes ? (
                  <div className="flex flex-col gap-2">
                    <textarea
                      className={cn('w-full px-3 py-2 border border-border rounded-md bg-background text-foreground text-sm resize-y min-h-[80px]', 'focus:outline-none focus:border-primary')}
                      value={notesValue}
                      onChange={(e) => setNotesValue(e.target.value)}
                      placeholder="Add investigation notes..."
                    />
                    <div className="flex gap-2">
                      <Button
                        size="sm"
                        onClick={handleSaveNotes}
                        disabled={notesMutation.isPending}
                      >
                        {notesMutation.isPending ? 'Saving...' : 'Save'}
                      </Button>
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => setIsEditingNotes(false)}
                        disabled={notesMutation.isPending}
                      >
                        Cancel
                      </Button>
                    </div>
                  </div>
                ) : (
                  <p className="text-sm text-foreground">
                    {job.notes
                      ? (
                        // Free text a staff member typed about ONE failing job, on a screen
                        // filtered by user_id — the same shape as the orphaned-item operator
                        // note. DeadLetterPage masks the `notes` column; this untruncated
                        // detail view must not be the weaker surface. Masked at the element,
                        // never on the row, so the surrounding labels stay legible.
                        <span data-ph-mask>{job.notes}</span>
                      )
                      : <span className="text-muted-foreground italic">No notes yet.</span>}
                  </p>
                )}
              </div>
            </div>

            <DialogFooter className="flex-wrap gap-2">
              <Button
                variant="outline"
                size="sm"
                onClick={handleRetry}
                disabled={!!job.retried_at || retryMutation.isPending}
                title={job.retried_at ? `Retried at ${new Date(job.retried_at).toLocaleString()}` : undefined}
              >
                {retryMutation.isPending ? 'Retrying...' : job.retried_at ? 'Already Retried' : 'Retry'}
              </Button>
              <Button
                variant="outline"
                size="sm"
                className="text-destructive hover:bg-destructive/10 hover:text-destructive border-destructive/30"
                onClick={() => setConfirmDelete(true)}
              >
                Delete
              </Button>
              <Button variant="outline" size="sm" onClick={closeModal}>
                Close
              </Button>
            </DialogFooter>
          </>
        ) : null}
      </DialogContent>
    </Dialog>

    <ConfirmationDialog
      open={confirmDelete}
      onOpenChange={setConfirmDelete}
      onConfirm={handleDelete}
      title="Delete Job"
      description={job ? `Delete job #${job.id} (${job.kind})? This action cannot be undone.` : ''}
      variant="destructive"
      loading={deleteMutation.isPending}
    />
    </>
  )
}
