/**
 * LandingFooter Component
 *
 * Simple footer for the landing page with copyright and essential links.
 * Issue #699: Landing Page Design Improvements
 */

import { Link } from 'react-router-dom'
import { Rocket } from 'lucide-react'

const footerLinks = [
  { label: 'Privacy Policy', href: '/privacy' },
  { label: 'Pricing', href: '/pricing' },
]

export function LandingFooter() {
  const currentYear = new Date().getFullYear()

  return (
    <footer className="border-t border-slate-200 bg-white py-12 dark:border-slate-800 dark:bg-slate-900">
      <div className="mx-auto max-w-7xl px-4 sm:px-6 lg:px-8">
        <div className="flex flex-col items-center justify-between gap-6 sm:flex-row">
          {/* Logo and Copyright */}
          <div className="flex flex-col items-center gap-3 sm:flex-row sm:gap-4">
            <div className="flex items-center gap-2">
              <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-blue-600">
                <Rocket className="h-4 w-4 text-white" />
              </div>
              <span className="font-semibold text-slate-900 dark:text-white">
                BoostedBudget
              </span>
            </div>
            <span className="text-sm text-slate-500 dark:text-slate-400">
              © {currentYear} BoostedBudget. All rights reserved.
            </span>
          </div>

          {/* Links */}
          <div className="flex items-center gap-6">
            {footerLinks.map((link) => (
              <Link
                key={link.label}
                to={link.href}
                className="text-sm text-slate-500 transition-colors hover:text-slate-900 dark:text-slate-400 dark:hover:text-white"
              >
                {link.label}
              </Link>
            ))}
          </div>
        </div>

        {/* GDPR Compliance Note */}
        <div className="mt-8 text-center">
          <p className="text-xs text-slate-400 dark:text-slate-500">
            GDPR Compliant • Your data is protected with bank-level encryption
          </p>
        </div>
      </div>
    </footer>
  )
}
