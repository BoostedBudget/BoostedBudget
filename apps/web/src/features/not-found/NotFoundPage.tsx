import { useLayoutEffect } from 'react'
import { Link } from 'react-router-dom'
import { Compass } from 'lucide-react'
import { Button } from '@/components/ui/button'

export function NotFoundPage() {
  useLayoutEffect(() => {
    document.title = '404 — Page Not Found | BoostedBudget'
  }, [])

  return (
    <div className="flex flex-col items-center justify-center min-h-[60vh] px-4 text-center">
      <Compass
        className="h-16 w-16 text-gray-300 dark:text-gray-700 mb-6"
        aria-hidden="true"
      />
      <p className="text-7xl font-bold text-gray-300 dark:text-gray-700 mb-4">404</p>
      <h1 className="text-3xl font-bold text-gray-900 dark:text-white mb-4">
        Page Not Found
      </h1>
      <p className="text-gray-600 dark:text-gray-400 max-w-md mb-8">
        The page you&apos;re looking for doesn&apos;t exist or has been moved. Let us help you
        find your way back.
      </p>
      <div className="flex flex-col sm:flex-row gap-4 items-center">
        <Button asChild size="lg">
          <Link to="/dashboard">Go to Dashboard</Link>
        </Button>
        <a
          href="mailto:support@boostedbudget.com"
          className="text-sm text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-white underline underline-offset-4"
        >
          Contact Support
        </a>
      </div>
    </div>
  )
}
