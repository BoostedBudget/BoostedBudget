import type { ReactNode } from 'react'
import { Modal, Platform, View, type ViewProps } from 'react-native'
import { Surface } from './Surface'
import { Text } from './Text'

export type DialogRole = 'dialog' | 'alertdialog'

export interface DialogProps {
  visible: boolean
  /** Fired by the Android hardware back button and, on web, by Escape (react-native-web's Modal). */
  onRequestClose: () => void
  title?: string
  description?: string
  children?: ReactNode
  /**
   * When true, a tap on the backdrop requests close. Defaults to false: dismissal is explicit, so
   * a destructive confirm can never be dismissed by an accidental tap outside the dialog.
   */
  dismissOnBackdrop?: boolean
  /** ARIA role of the dialog surface. `ConfirmDialog` uses `alertdialog`. */
  role?: DialogRole
  testID?: string
}

/**
 * A modal dialog built on React Native's `Modal`.
 *
 * The overlay and the dialog surface both carry `accessibilityViewIsModal`, so the platform treats
 * the dialog as modal and ignores the background. The surface carries the ARIA `role` (`dialog` by
 * default, `alertdialog` for a confirm). The backdrop is a plain, non-focusable `View`: it must not
 * become a tab stop, or the web focus trap would land on it instead of the dialog's first action.
 *
 * ⚠ The focus TRAP and focus RESTORE are browser behaviours owned by react-native-web's
 * `ModalFocusTrap` and are asserted in Todo 22(e), not here — jest has no DOM and RN `Modal` does
 * not implement web focus management.
 */
export function Dialog({
  visible,
  onRequestClose,
  title,
  description,
  children,
  dismissOnBackdrop = false,
  role = 'dialog',
  testID = 'dialog',
}: DialogProps) {
  // RN 0.86's legacy `accessibilityRole` union omits `dialog`/`alertdialog` (they live on the ARIA
  // `role` prop). The native shim still forwards the legacy prop so older consumers see it; on web
  // only `role` is set, because react-native-web deprecates `accessibilityRole`.
  const legacyRoleProps =
    Platform.OS === 'web' ? {} : ({ accessibilityRole: role } as unknown as ViewProps)

  const handleBackdropRelease = () => {
    if (dismissOnBackdrop) onRequestClose()
  }

  return (
    <Modal
      visible={visible}
      transparent
      animationType="fade"
      onRequestClose={onRequestClose}
      testID={testID}
    >
      <View
        testID={`${testID}-overlay`}
        accessibilityViewIsModal
        className="flex-1 items-center justify-center p-bb-4"
      >
        <View
          testID={`${testID}-backdrop`}
          accessible={false}
          importantForAccessibility="no-hide-descendants"
          className="absolute inset-0 bg-black/50"
          onStartShouldSetResponder={() => dismissOnBackdrop}
          onResponderRelease={handleBackdropRelease}
        />
        <Surface
          testID={`${testID}-surface`}
          variant="overlay"
          role={role}
          accessibilityViewIsModal
          className="w-full gap-bb-3 p-bb-5"
          {...legacyRoleProps}
        >
          {title ? (
            <Text testID={`${testID}-title`} variant="heading">
              {title}
            </Text>
          ) : null}
          {description ? (
            <Text testID={`${testID}-description`} variant="bodySmall" tone="muted">
              {description}
            </Text>
          ) : null}
          {children}
        </Surface>
      </View>
    </Modal>
  )
}