import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react'
import { AccessibilityInfo, Animated, StyleSheet, View } from 'react-native'
import { SafeAreaInsetsContext } from 'react-native-safe-area-context'
import { nativeTokens } from '@boostedbudget/design-tokens/native'
import { Toast, type ToastAction, type ToastVariant } from './Toast'
import { useReducedMotion } from './Spinner'

const MOTION = nativeTokens.light.motion

/** Enter transition, taken from the motion scale. */
export const TOAST_ENTER_DURATION_MS = MOTION['transition-duration-bb-normal']
/** Exit transition, taken from the motion scale. */
export const TOAST_EXIT_DURATION_MS = MOTION['transition-duration-bb-fast']
/**
 * Reduced-motion duration. Unlike `Spinner` (which slows, never freezes, because a frozen spinner
 * reads as a hung page) a toast is a discrete appearance, so reduced motion collapses its
 * transition to zero — the toast simply appears and disappears.
 */
export const TOAST_REDUCED_DURATION_MS = 0
/** Default visible duration for auto-dismissible toasts. */
export const TOAST_DEFAULT_DURATION_MS = 5000

export interface ToastOptions {
  message: string
  variant?: ToastVariant
  duration?: number
  action?: ToastAction
}

interface ToastRecord {
  id: number
  message: string
  variant: ToastVariant
  duration: number
  action?: ToastAction
  leaving: boolean
}

export interface ToastContextValue {
  show: (options: ToastOptions) => number
  dismiss: (id: number) => void
}

const ToastContext = createContext<ToastContextValue | null>(null)

export function useToast(): ToastContextValue {
  const context = useContext(ToastContext)
  if (!context) throw new Error('useToast must be used within a ToastProvider')
  return context
}

/**
 * Tracks whether a screen reader is active. Auto-dismiss is suspended while it is: a timer that
 * removes a toast mid-announcement would make the message unreachable.
 */
function useScreenReaderEnabled(): boolean {
  const [enabled, setEnabled] = useState(false)

  useEffect(() => {
    let active = true
    const apply = (next: boolean) => {
      if (active) setEnabled((previous) => (previous === next ? previous : next))
    }
    AccessibilityInfo.isScreenReaderEnabled().then(apply)
    const subscription = AccessibilityInfo.addEventListener('screenReaderChanged', apply)
    return () => {
      active = false
      subscription.remove()
    }
  }, [])

  return enabled
}

const FALLBACK_INSETS = { top: 0, right: 0, bottom: 0, left: 0 }

interface ToastItemProps {
  toast: ToastRecord
  screenReaderEnabled: boolean
  onDismiss: (id: number) => void
  onExited: (id: number) => void
}

function ToastItem({ toast, screenReaderEnabled, onDismiss, onExited }: ToastItemProps) {
  const reduceMotion = useReducedMotion()
  const [progress] = useState(() => new Animated.Value(0))
  const enterDuration = reduceMotion ? TOAST_REDUCED_DURATION_MS : TOAST_ENTER_DURATION_MS

  useEffect(() => {
    const animation = Animated.timing(progress, {
      toValue: 1,
      duration: enterDuration,
      useNativeDriver: true,
    })
    animation.start()
    return () => animation.stop()
  }, [progress, enterDuration])

  useEffect(() => {
    if (toast.leaving) return
    // A danger toast is never auto-dismissed: it must stay until the user has read it.
    if (toast.variant === 'danger') return
    if (screenReaderEnabled) return
    if (toast.duration <= 0) return
    const timer = globalThis.setTimeout(() => onDismiss(toast.id), toast.duration)
    return () => globalThis.clearTimeout(timer)
  }, [toast.leaving, toast.variant, toast.duration, toast.id, screenReaderEnabled, onDismiss])

  useEffect(() => {
    if (!toast.leaving) return
    const exitDuration = reduceMotion ? TOAST_REDUCED_DURATION_MS : TOAST_EXIT_DURATION_MS
    const animation = Animated.timing(progress, {
      toValue: 0,
      duration: exitDuration,
      useNativeDriver: true,
    })
    animation.start(({ finished }) => {
      if (finished) onExited(toast.id)
    })
    return () => animation.stop()
  }, [toast.leaving, toast.id, reduceMotion, progress, onExited])

  const translateY = progress.interpolate({ inputRange: [0, 1], outputRange: [8, 0] })

  return (
    <Animated.View style={{ opacity: progress, transform: [{ translateY }] }}>
      <Toast
        testID={`toast-item-${toast.id}`}
        variant={toast.variant}
        message={toast.message}
        action={toast.action}
        onDismiss={() => onDismiss(toast.id)}
      />
    </Animated.View>
  )
}

/**
 * Owns the toast queue and renders it above the rest of the app. The queue is appended in call
 * order, so the oldest toast's timer is the first to fire. The viewport is `box-none` (touches
 * pass through to the app; only the toast surfaces themselves are interactive) and is padded by
 * the safe-area insets so no toast renders under a notch or the home indicator.
 */
export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<ToastRecord[]>([])
  const nextId = useRef(0)
  const screenReaderEnabled = useScreenReaderEnabled()
  const insets = useContext(SafeAreaInsetsContext) ?? FALLBACK_INSETS

  const dismiss = useCallback((id: number) => {
    setToasts((previous) =>
      previous.map((toast) => (toast.id === id ? { ...toast, leaving: true } : toast)),
    )
  }, [])

  const remove = useCallback((id: number) => {
    setToasts((previous) => previous.filter((toast) => toast.id !== id))
  }, [])

  const show = useCallback((options: ToastOptions) => {
    const id = (nextId.current += 1)
    setToasts((previous) => [
      ...previous,
      {
        id,
        message: options.message,
        variant: options.variant ?? 'info',
        duration: options.duration ?? TOAST_DEFAULT_DURATION_MS,
        action: options.action,
        leaving: false,
      },
    ])
    return id
  }, [])

  const value = useMemo(() => ({ show, dismiss }), [show, dismiss])

  return (
    <ToastContext.Provider value={value}>
      {children}
      <View
        testID="toast-viewport"
        pointerEvents="box-none"
        style={[
          StyleSheet.absoluteFill,
          {
            justifyContent: 'flex-end',
            paddingTop: insets.top,
            paddingBottom: insets.bottom,
            paddingHorizontal: nativeTokens.light.spacing['spacing-bb-4'],
            gap: nativeTokens.light.spacing['spacing-bb-2'],
          },
        ]}
      >
        {toasts.map((toast) => (
          <ToastItem
            key={toast.id}
            toast={toast}
            screenReaderEnabled={screenReaderEnabled}
            onDismiss={dismiss}
            onExited={remove}
          />
        ))}
      </View>
    </ToastContext.Provider>
  )
}
