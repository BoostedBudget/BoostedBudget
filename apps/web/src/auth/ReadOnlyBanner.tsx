import { Link } from 'react-router-dom'

interface ReadOnlyBannerProps {
  status: string
}

export function ReadOnlyBanner({ status }: ReadOnlyBannerProps) {
  const message = status === 'inactive'
    ? 'Your trial has ended. Your data is preserved in read-only mode.'
    : 'Your subscription has been cancelled. Your data is preserved in read-only mode.'

  return (
    <div
      role="alert"
      className="bg-amber-50 dark:bg-amber-900/20 border-b border-amber-200 dark:border-amber-800 px-4 py-3 flex items-center justify-between gap-4"
    >
      <div className="flex items-center gap-2 text-sm text-amber-800 dark:text-amber-200">
        <span aria-hidden="true">⚠️</span>
        <span>{message}</span>
      </div>
      <Link
        to="/pricing"
        className="shrink-0 px-3 py-1.5 text-sm font-medium bg-amber-600 hover:bg-amber-700 text-white rounded-lg transition-colors"
      >
        Subscribe to restore access
      </Link>
    </div>
  )
}
