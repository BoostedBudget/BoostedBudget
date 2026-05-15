import { useState } from 'react'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
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
} from '@boostedbudget/api-client/endpoints/admin/admin'

interface DeadLetterDetailModalProps {
  jobId: number | null
  onClose: () => void
  onDeleted?: () => void
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

export function DeadLetterDetailModal({ jobId, onClose, onDeleted }: DeadLetterDetailModalProps) {
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

  const retryMutation = useRetryAdminDeadLetterJob()
  const notesMutation = useAddAdminDeadLetterJobNotes()
  const deleteMutation = useDeleteAdminDeadLetterJob()

  const handleRevealArgs = () => {
    setShowRawArgs(true)
    fetchArgs()
  }

  const handleRetry = () => {
    if (!jobId) return
    retryMutation.mutate({ id: jobId }, {
      onSuccess: () => {
        trackEvent(ANALYTICS_EVENTS.ADMIN_DLQ_RETRY, { job_id: jobId })
      },
    })
  }

  const handleSaveNotes = () => {
    if (!jobId) return
    notesMutation.mutate(
      { id: jobId, data: { notes: notesValue } },
      { onSuccess: () => setIsEditingNotes(false) }
    )
  }

  const closeModal = () => {
    setShowRawArgs(false)
    setIsEditingNotes(false)
    setConfirmDelete(false)
    onClose()
  }

  const handleDelete = () => {
    if (!jobId) return
    deleteMutation.mutate({ id: jobId }, {
      onSuccess: () => {
        trackEvent(ANALYTICS_EVENTS.ADMIN_DLQ_DELETE, { job_id: jobId })
        onDeleted?.()
        closeModal()
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
                <span
                  className={cn(
                    'inline-flex items-center px-2 py-0.5 rounded text-xs font-medium',
                    job.retried_at
                      ? 'bg-success/10 text-success'
                      : 'bg-destructive/10 text-destructive'
                  )}
                >
                  {job.retried_at ? 'Retried' : 'Pending Retry'}
                </span>
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
                <pre className={cn(preClass, 'text-destructive')} data-ph-mask>
                  {formatJson(job.errors) || '(none)'}
                </pre>
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
                  <pre className={cn(preClass, 'text-muted-foreground')}>
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
                    {job.notes || <span className="text-muted-foreground italic">No notes yet.</span>}
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
