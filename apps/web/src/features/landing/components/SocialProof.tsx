/**
 * SocialProof Component
 *
 * Compact trust bar with hover tooltips for expanded descriptions.
 * Issue #699: Create Marketing Landing Page for First-Time Visitors
 */

import { Shield, Lock, CheckCircle2, Users } from 'lucide-react'

const trustItems = [
  {
    icon: Shield,
    label: 'Bank-Level Security',
    tooltip: '256-bit encryption protects all your data at rest and in transit',
  },
  {
    icon: Lock,
    label: 'Powered by Plaid',
    tooltip: 'Secure, read-only connections to 12,000+ financial institutions',
  },
  {
    icon: CheckCircle2,
    label: 'GDPR Compliant',
    tooltip: 'Your privacy is our priority. Export or delete your data anytime',
  },
  {
    icon: Users,
    label: 'Trusted by Users',
    tooltip: 'Join thousands of people taking control of their finances',
  },
]

export function SocialProof() {
  return (
    <div
      className="social-proof-bar border-y border-slate-200 bg-slate-50 py-6 dark:border-slate-700 dark:bg-slate-800/50"
      role="region"
      aria-label="Trust badges"
    >
      <div className="mx-auto max-w-7xl px-4 sm:px-6 lg:px-8">
        <div className="flex flex-wrap items-center justify-center gap-6 sm:gap-8 lg:gap-12">
          {trustItems.map((item, index) => (
            <div
              key={index}
              className="trust-item group relative flex items-center gap-2 text-sm text-slate-600 dark:text-slate-400"
            >
              <div className="flex h-8 w-8 items-center justify-center rounded-full bg-emerald-100 text-emerald-600 transition-colors group-hover:bg-emerald-200 dark:bg-emerald-900/40 dark:text-emerald-400 dark:group-hover:bg-emerald-900/60">
                <item.icon className="h-4 w-4" aria-hidden="true" />
              </div>
              <span className="font-medium text-slate-700 dark:text-slate-300">
                {item.label}
              </span>

              {/* Hover Tooltip */}
              <div className="trust-tooltip max-w-xs text-center text-xs text-slate-700 dark:text-slate-200">
                {item.tooltip}
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}
