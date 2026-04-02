/**
 * AppFooter Component
 *
 * Unified site-wide footer — rendered on every page.
 * Issue #950: Unify footer component — single template used across all pages
 */

import { Link } from 'react-router-dom'
import { Rocket } from 'lucide-react'

const footerLinks = [
  { label: 'Privacy Policy', href: '/legal/privacy-policy' },
  { label: 'Terms of Service', href: '/legal/terms' },
  { label: 'Cookie Policy', href: '/legal/cookie-policy' },
  { label: 'Pricing', href: '/pricing' },
  { label: 'Contact', href: 'mailto:support@boostedbudget.com' },
]

export function AppFooter() {
  const currentYear = new Date().getFullYear()

  return (
    <footer className="border-t border-border py-8">
      <div className="mx-auto max-w-7xl px-4 sm:px-6 lg:px-8">
        <div className="flex flex-col items-center justify-between gap-4 sm:flex-row">
          <div className="flex items-center gap-2">
            <div className="flex h-7 w-7 items-center justify-center rounded-lg bg-blue-600">
              <Rocket className="h-3.5 w-3.5 text-white" />
            </div>
            <span className="font-semibold text-foreground">BoostedBudget</span>
            <span className="text-sm text-muted-foreground">
              © {currentYear} BoostedBudget. All rights reserved.
            </span>
          </div>
          <div className="flex flex-wrap items-center justify-center gap-x-6 gap-y-2">
            {footerLinks.map((link) => (
              <Link
                key={link.label}
                to={link.href}
                className="text-sm text-muted-foreground transition-colors hover:text-foreground"
              >
                {link.label}
              </Link>
            ))}
          </div>
        </div>
        <div className="mt-6 text-center">
          <p className="text-xs text-muted-foreground">
            GDPR Compliant • Your data is protected with bank-level encryption
          </p>
        </div>
      </div>
    </footer>
  )
}
