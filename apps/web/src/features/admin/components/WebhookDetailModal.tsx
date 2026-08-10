/**
 * WebhookDetailModal Component
 * Issue #747: Paddle Webhook Admin UI
 *
 * Modal dialog displaying Paddle webhook event details with replay capability.
 */

import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Loader2 } from 'lucide-react'
import {
  useGetAdminPaddleWebhookDetail,
  useReplayAdminPaddleWebhook,
} from '@boostedbudget/api-client/endpoints/admin/admin'

interface WebhookDetailModalProps {
  webhookId: string | null
  onClose: () => void
}

const formatPayload = (payload: unknown): string => {
  if (!payload) return ''
  try {
    return JSON.stringify(typeof payload === 'string' ? JSON.parse(payload) : payload, null, 2)
  } catch {
    return String(payload)
  }
}

export function WebhookDetailModal({ webhookId, onClose }: WebhookDetailModalProps) {
  const { data: event, isLoading, error } = useGetAdminPaddleWebhookDetail(webhookId ?? '', {
    query: { enabled: !!webhookId },
  })

  const replayMutation = useReplayAdminPaddleWebhook()

  return (
    <Dialog open={!!webhookId} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-w-[680px] max-h-[85vh] overflow-y-auto">
        {isLoading ? (
          <div className="flex flex-col items-center justify-center py-12 gap-3 text-muted-foreground">
            <Loader2 className="animate-spin" />
            <span>Loading webhook details...</span>
          </div>
        ) : error ? (
          <div className="flex items-center justify-center py-12 text-destructive">
            Failed to load webhook.
          </div>
        ) : event ? (
          <>
            <DialogHeader>
              <DialogTitle>{event.event_type}</DialogTitle>
            </DialogHeader>

            <div className="flex flex-col gap-3.5 my-4">
              <div className="flex items-start gap-2">
                <span className="text-[0.8125rem] font-semibold text-muted-foreground min-w-[110px] shrink-0">Event ID:</span>
                {/* Masked at the ELEMENT, not the row: the label stays legible so an operator
                    can still see which field is hidden. WebhooksPage masks the same field in
                    its table (maskedColumnIds={['event_id', 'error']}) — the modal one click
                    deeper must not be the weaker surface. */}
                <span className="text-sm text-foreground break-all" data-ph-mask>{event.event_id}</span>
              </div>
              <div className="flex items-start gap-2">
                <span className="text-[0.8125rem] font-semibold text-muted-foreground min-w-[110px] shrink-0">Status:</span>
                <Badge variant={event.processed ? 'success' : 'warning'}>
                  {event.processed ? 'Processed' : 'Pending'}
                </Badge>
              </div>
              <div className="flex items-start gap-2">
                <span className="text-[0.8125rem] font-semibold text-muted-foreground min-w-[110px] shrink-0">Attempts:</span>
                <span className="text-sm text-foreground break-all">{event.processing_attempts}</span>
              </div>
              {event.processing_error && (
                <div className="flex items-start gap-2">
                  <span className="text-[0.8125rem] font-semibold text-muted-foreground min-w-[110px] shrink-0">Error:</span>
                  {/* A handler error message is free text that routinely quotes the payload it
                      failed on — customer email, amounts, Paddle ids. Untruncated here, unlike
                      the table. Masked at the element so the "Error:" label stays readable. */}
                  <span className="text-sm text-destructive break-all" data-ph-mask>{event.processing_error}</span>
                </div>
              )}
              <div className="flex items-start gap-2">
                <span className="text-[0.8125rem] font-semibold text-muted-foreground min-w-[110px] shrink-0">Created:</span>
                <span className="text-sm text-foreground break-all">
                  {event.created_at ? new Date(event.created_at).toLocaleString() : '—'}
                </span>
              </div>
              {event.processed_at && (
                <div className="flex items-start gap-2">
                  <span className="text-[0.8125rem] font-semibold text-muted-foreground min-w-[110px] shrink-0">Processed At:</span>
                  <span className="text-sm text-foreground break-all">
                    {new Date(event.processed_at).toLocaleString()}
                  </span>
                </div>
              )}

              <div className="flex flex-col gap-2 mt-2">
                <span className="text-[0.8125rem] font-semibold text-muted-foreground min-w-[110px] shrink-0">Payload:</span>
                {/* A Paddle payload is a verbatim vendor document: it carries the customer's
                    email, name and billed amounts. Masked for the same reason
                    DeadLetterDetailModal masks its raw-args and error blocks — the whole point of
                    showing it is that nobody knows in advance what is inside it. */}
                <pre className="bg-muted border border-border rounded-md px-4 py-3.5 text-[0.8125rem] font-mono overflow-x-auto whitespace-pre text-foreground max-h-[280px] overflow-y-auto" data-ph-mask>
                  {formatPayload(event.payload)}
                </pre>
              </div>
            </div>

            <DialogFooter>
              <Button
                variant="outline"
                onClick={() => replayMutation.mutate({ id: event.id ?? '' })}
                disabled={replayMutation.isPending}
              >
                {replayMutation.isPending ? 'Replaying...' : 'Replay'}
              </Button>
              <Button variant="outline" onClick={onClose}>
                Close
              </Button>
            </DialogFooter>
          </>
        ) : null}
      </DialogContent>
    </Dialog>
  )
}
