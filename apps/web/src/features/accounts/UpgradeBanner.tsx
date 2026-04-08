/**
 * UpgradeBanner Component
 *
 * Displays a warning banner when user has reached their account limit.
 * Extracted from AccountsPage for reusability and cleaner component structure.
 *
 * TDD REFACTOR: Extracted during account limit enforcement implementation.
 */

export interface UpgradeBannerProps {
  /** Message to display in the banner */
  message?: string
}

/**
 * Banner component to inform users they've hit their account limit
 *
 * @param message - Optional custom message (defaults to account limit message)
 *
 * @example
 * ```tsx
 * function AccountsPage() {
 *   const { shouldShowUpgradeBanner } = useTierLimits(...)
 *
 *   return (
 *     <>
 *       {shouldShowUpgradeBanner && <UpgradeBanner />}
 *     </>
 *   )
 * }
 * ```
 */
export function UpgradeBanner({ message }: UpgradeBannerProps) {
  const defaultMessage = 'Upgrade to a higher tier to connect more accounts.'

  return (
    <div className="bg-warning/10 border border-warning/30 rounded-lg px-6 py-4 mb-6">
      <p className="m-0 text-warning-foreground text-[0.9375rem]">
        <strong className="text-warning">Account limit reached.</strong> {message || defaultMessage}
      </p>
    </div>
  )
}
