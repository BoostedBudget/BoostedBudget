/**
 * UpgradeBanner Component
 *
 * Displays a warning banner when user has reached their account limit.
 * Extracted from AccountsPage for reusability and cleaner component structure.
 *
 * TDD REFACTOR: Extracted during account limit enforcement implementation.
 */

import './UpgradeBanner.css'

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
    <div className="upgrade-banner">
      <p>
        <strong>Account limit reached.</strong> {message || defaultMessage}
      </p>
    </div>
  )
}
