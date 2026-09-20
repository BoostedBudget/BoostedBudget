import { View } from 'react-native'
import { Button } from './Button'
import { Dialog } from './Dialog'

export interface ConfirmDialogProps {
  visible: boolean
  title: string
  message: string
  confirmLabel: string
  cancelLabel?: string
  /** Renders the confirm action with the danger token. The safe action then takes initial focus. */
  destructive?: boolean
  onConfirm: () => void
  onCancel: () => void
  loading?: boolean
  testID?: string
}

/**
 * A confirmation dialog built on `Dialog`.
 *
 * The confirm action is the primary focus target — except when `destructive`, where the safe
 * (cancel) action is rendered first so it receives the initial focus. A destructive confirm always
 * carries a visible text label as well as the danger token, so the danger is never conveyed by
 * colour alone.
 */
export function ConfirmDialog({
  visible,
  title,
  message,
  confirmLabel,
  cancelLabel = 'Cancel',
  destructive = false,
  onConfirm,
  onCancel,
  loading = false,
  testID = 'confirm-dialog',
}: ConfirmDialogProps) {
  const handleRequestClose = () => {
    if (!loading) onCancel()
  }

  const confirmAction = (
    <Button
      key="confirm"
      testID={`${testID}-confirm`}
      variant={destructive ? 'destructive' : 'primary'}
      label={confirmLabel}
      loading={loading}
      onPress={onConfirm}
    />
  )
  const cancelAction = (
    <Button
      key="cancel"
      testID={`${testID}-cancel`}
      variant="outline"
      label={cancelLabel}
      disabled={loading}
      onPress={onCancel}
    />
  )

  // react-native-web's `ModalFocusTrap` focuses the FIRST focusable descendant on open, so DOM
  // order decides the initial focus. A destructive confirm must never be the initial focus — the
  // safe action is rendered first so it receives focus.
  const actions = destructive ? [cancelAction, confirmAction] : [confirmAction, cancelAction]

  return (
    <Dialog
      visible={visible}
      onRequestClose={handleRequestClose}
      title={title}
      description={message}
      role="alertdialog"
      testID={testID}
    >
      <View className="flex-row justify-end gap-bb-2">{actions}</View>
    </Dialog>
  )
}