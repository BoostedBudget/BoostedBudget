/**
 * WebhookDetailModal Component
 * Issue #747: Paddle Webhook Admin UI
 *
 * Modal dialog displaying Paddle webhook event details with replay capability.
 */

import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Loader2 } from 'lucide-react'
import {
  useGetAdminPaddleWebhookDetail,
  useReplayAdminPaddleWebhook,
} from '@boostedbudget/api-client/endpoints/admin/admin'
import './WebhookDetailModal.css'

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
      <DialogContent className="webhook-detail-modal">
        {isLoading ? (
          <div className="webhook-detail-modal__loading">
            <Loader2 className="spinning" />
            <span>Loading webhook details...</span>
          </div>
        ) : error ? (
          <div className="webhook-detail-modal__error">Failed to load webhook.</div>
        ) : event ? (
          <>
            <DialogHeader>
              <DialogTitle>{event.event_type}</DialogTitle>
            </DialogHeader>

            <div className="webhook-detail-modal__content">
              <div className="webhook-detail-modal__field">
                <span className="webhook-detail-modal__label">Event ID:</span>
                <span className="webhook-detail-modal__value">{event.event_id}</span>
              </div>
              <div className="webhook-detail-modal__field">
                <span className="webhook-detail-modal__label">Status:</span>
                <span
                  className={`webhook-detail-modal__status webhook-detail-modal__status--${event.processed ? 'processed' : 'pending'}`}
                >
                  {event.processed ? 'Processed' : 'Pending'}
                </span>
              </div>
              <div className="webhook-detail-modal__field">
                <span className="webhook-detail-modal__label">Attempts:</span>
                <span className="webhook-detail-modal__value">{event.processing_attempts}</span>
              </div>
              {event.processing_error && (
                <div className="webhook-detail-modal__field">
                  <span className="webhook-detail-modal__label">Error:</span>
                  <span className="webhook-detail-modal__error-text">{event.processing_error}</span>
                </div>
              )}
              <div className="webhook-detail-modal__field">
                <span className="webhook-detail-modal__label">Created:</span>
                <span className="webhook-detail-modal__value">
                  {event.created_at ? new Date(event.created_at).toLocaleString() : '—'}
                </span>
              </div>
              {event.processed_at && (
                <div className="webhook-detail-modal__field">
                  <span className="webhook-detail-modal__label">Processed At:</span>
                  <span className="webhook-detail-modal__value">
                    {new Date(event.processed_at).toLocaleString()}
                  </span>
                </div>
              )}

              <div className="webhook-detail-modal__payload">
                <span className="webhook-detail-modal__label">Payload:</span>
                <pre className="webhook-detail-modal__payload-pre">
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
