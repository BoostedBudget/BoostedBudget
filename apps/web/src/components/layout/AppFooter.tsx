/**
 * AppFooter Component
 *
 * Unified site-wide footer — rendered on every page.
 * Issue #950: Unify footer component — single template used across all pages
 * Issue #1411: Restructure the flat link row into four grouped columns
 * (Product, Developers, Legal, Support) and give the MCP server a named home.
 */

import { Link } from 'react-router'
import { Rocket } from 'lucide-react'

interface FooterLink {
  label: string
  href: string
  /** External destinations (mailto:, tel:, etc.) render as a plain anchor. */
  external?: boolean
}

interface FooterGroup {
  heading: string
  links: FooterLink[]
}

const footerGroups: FooterGroup[] = [
  {
    heading: 'Product',
    links: [
      { label: 'Features', href: '/features' },
      { label: 'Pricing', href: '/pricing' },
      { label: 'FAQ', href: '/faq' },
    ],
  },
  {
    heading: 'Developers',
    links: [{ label: 'MCP Server', href: '/mcp' }],
  },
  {
    heading: 'Legal',
    links: [
      { label: 'Privacy Policy', href: '/legal/privacy-policy' },
      { label: 'Terms of Service', href: '/legal/terms' },
      { label: 'Cookie Policy', href: '/legal/cookie-policy' },
    ],
  },
  {
    heading: 'Support',
    links: [{ label: 'Contact', href: 'mailto:support@boostedbudget.com', external: true }],
  },
]

const footerLinkClassName = 'text-sm text-muted-foreground transition-colors hover:text-foreground'

export function AppFooter() {
  const currentYear = new Date().getFullYear()

  return (
    <footer className="border-t border-border py-10">
      <div className="mx-auto max-w-7xl px-4 sm:px-6 lg:px-8">
        <div className="flex items-center gap-2">
          <div className="flex h-7 w-7 items-center justify-center rounded-lg bg-primary">
            <Rocket className="h-3.5 w-3.5 text-primary-foreground" />
          </div>
          <span className="font-semibold text-foreground">BoostedBudget</span>
        </div>
        <p className="mt-2 max-w-xs text-sm text-muted-foreground">
          Smart budgeting for modern life — with a remote MCP server for your agent.
        </p>

        <div className="mt-8 grid grid-cols-1 gap-8 sm:grid-cols-2 md:grid-cols-4">
          {footerGroups.map((group) => (
            <div key={group.heading}>
              <h3 className="text-sm font-semibold text-foreground">{group.heading}</h3>
              <ul className="mt-3 space-y-2">
                {group.links.map((link) => (
                  <li key={link.label}>
                    {link.external ? (
                      <a href={link.href} className={footerLinkClassName}>
                        {link.label}
                      </a>
                    ) : (
                      <Link to={link.href} className={footerLinkClassName}>
                        {link.label}
                      </Link>
                    )}
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </div>

        <div className="mt-8 flex flex-col items-center justify-between gap-2 border-t border-border pt-6 text-center sm:flex-row sm:text-left">
          <p className="text-xs text-muted-foreground">
            © {currentYear} BoostedBudget. All rights reserved.
          </p>
          <p className="text-xs text-muted-foreground">
            GDPR Compliant • Your data is protected with bank-level encryption
          </p>
        </div>
      </div>
    </footer>
  )
}
