/**
 * TermsOfService Page
 *
 * Placeholder page for Terms of Service with link back to dashboard.
 * Full content coming soon.
 * Issue #837: Footer cleanup - add Terms of Service page
 * Route: /legal/terms (see App.tsx line 273).
 */

import { Link } from 'react-router-dom'

export function TermsOfService() {
  return (
    <div className="mx-auto max-w-2xl px-4 py-16 text-center">
      <h1 className="text-3xl font-bold text-foreground mb-4">Terms of Service</h1>
      <p className="text-muted-foreground mb-8">
        Our Terms of Service are coming soon. We're working on making them clear and easy to understand.
      </p>
      <p className="text-muted-foreground mb-8">
        In the meantime, if you have any questions, please{' '}
        <a
          href="mailto:support@boostedbudget.com"
          className="text-blue-600 hover:underline"
        >
          contact our support team
        </a>
        .
      </p>
      <Link
        to="/dashboard"
        className="inline-flex items-center justify-center rounded-lg bg-blue-600 px-6 py-2.5 text-sm font-medium text-white transition-colors hover:bg-blue-700"
      >
        Go to Dashboard
      </Link>
    </div>
  )
}
