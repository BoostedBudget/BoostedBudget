import { useContext, type ReactNode } from 'react'
import {
  Modal,
  Platform,
  View,
  useWindowDimensions,
  type ViewProps,
} from 'react-native'
import { SafeAreaInsetsContext } from 'react-native-safe-area-context'
import { nativeTokens } from '@boostedbudget/design-tokens/native'
import { Surface } from './Surface'
import { useReducedMotion } from './Spinner'

const BASE_PADDING = nativeTokens.light.spacing['spacing-bb-5']

/**
 * `useSafeAreaInsets()` throws without a `<SafeAreaProvider>` ancestor, and `jest.setup.ts` is
 * off-limits, so its jest mock cannot be registered globally. Reading the context directly with a
 * zero fallback is the same pattern the package's own `jest/mock.tsx` uses; real navigation and
 * production supply real insets.
 */
const FALLBACK_INSETS = { top: 0, right: 0, bottom: 0, left: 0 }

export interface BottomSheetProps {
  visible: boolean
  /** Fired by the Android hardware back button and, on web, by Escape (react-native-web's Modal). */
  onClose: () => void
  children?: ReactNode
  /**
   * Optional fractions (0..1) of the window height. The first is the sheet's resting height; the
   * sheet is always additionally capped so it can never exceed the safe area.
   */
  snapPoints?: number[]
  testID?: string
}

/**
 * A bottom-anchored modal sheet built on React Native's `Modal`.
 *
 * The overlay and the sheet surface both carry `accessibilityViewIsModal`, so the platform treats
 * the sheet as modal and ignores the background. The surface carries the ARIA `role` (`dialog`).
 * The backdrop is a plain, non-focusable `View`: it must not become a tab stop, or the web focus
 * trap would land on it instead of the sheet's first action.
 *
 * The surface is padded by the safe-area insets and its height is capped at `windowHeight -
 * insets.top`, so the sheet can never render under a notch or the home indicator. Under reduced
 * motion the slide transition is disabled rather than merely shortened.
 *
 * ⚠ The focus TRAP and focus RESTORE are browser behaviours owned by react-native-web's
 * `ModalFocusTrap` and are asserted in Todo 22(e), not here — jest has no DOM and RN `Modal` does
 * not implement web focus management.
 */
export function BottomSheet({
  visible,
  onClose,
  children,
  snapPoints,
  testID = 'bottom-sheet',
}: BottomSheetProps) {
  const insets = useContext(SafeAreaInsetsContext) ?? FALLBACK_INSETS
  const { height } = useWindowDimensions()
  const reduceMotion = useReducedMotion()

  const snapFraction = snapPoints?.[0]
  const maxHeight = Math.min(
    snapFraction !== undefined ? snapFraction * height : height,
    height - insets.top,
  )

  // RN 0.86's legacy `accessibilityRole` union omits `dialog` (it lives on the ARIA `role` prop).
  // The native shim still forwards the legacy prop; on web only `role` is set, because
  // react-native-web deprecates `accessibilityRole`.
  const legacyRoleProps =
    Platform.OS === 'web' ? {} : ({ accessibilityRole: 'dialog' } as unknown as ViewProps)

  return (
    <Modal
      visible={visible}
      transparent
      animationType={reduceMotion ? 'none' : 'slide'}
      onRequestClose={onClose}
      testID={testID}
    >
      <View
        testID={`${testID}-overlay`}
        accessibilityViewIsModal
        className="flex-1 justify-end"
      >
        <View
          testID={`${testID}-backdrop`}
          accessible={false}
          importantForAccessibility="no-hide-descendants"
          className="absolute inset-0 bg-black/50"
          onStartShouldSetResponder={() => true}
          onResponderRelease={onClose}
        />
        <Surface
          testID={`${testID}-surface`}
          variant="overlay"
          role="dialog"
          accessibilityViewIsModal
          className="w-full gap-bb-3"
          style={{
            maxHeight,
            paddingTop: BASE_PADDING,
            paddingHorizontal: BASE_PADDING,
            paddingBottom: insets.bottom + BASE_PADDING,
          }}
          {...legacyRoleProps}
        >
          {children}
        </Surface>
      </View>
    </Modal>
  )
}