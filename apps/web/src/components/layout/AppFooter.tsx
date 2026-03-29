/**
 * AppFooter Component
 *
 * Footer for the authenticated app with legal links and GDPR note.
 * Issue #837: Footer cleanup - remove internal jargon, add useful links
 * Displayed below all authenticated pages (see App.tsx line 313).
 */

import { Link } from 'react-router-dom'

const footerLinks = [
  { label: 'Privacy Policy', href: '/legal/privacy-policy' },
  { label: 'Terms of Service', href: '/legal/terms' },
  { label: 'Contact', href: 'mailto:support@boostedbudget.com' },
]

export function AppFooter() {
  const currentYear = new Date().getFullYear()

  return (
    <footer className="border-t border-border mt-16 px-4 md:px-6 py-6">
      <div className="flex flex-col items-center justify-between gap-4 sm:flex-row">
        <span className="text-sm text-muted-foreground">
          © {currentYear} BoostedBudget
        </span>
        <div className="flex items-center gap-6">
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
      <div className="mt-4 text-center">
        <p className="text-xs text-muted-foreground">
          GDPR Compliant • Your data is protected with bank-level encryption
        </p>
      </div>
    </footer>
  )
}
