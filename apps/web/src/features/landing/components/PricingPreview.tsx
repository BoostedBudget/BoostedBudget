/**
 * PricingPreview Component
 *
 * Compact pricing tier comparison with link to full pricing page.
 * Issue #699: Create Marketing Landing Page for First-Time Visitors
 */

import { Link } from 'react-router-dom'
import { Check, ArrowRight } from 'lucide-react'
import { useSubscriptionTiers } from '@/features/subscription/hooks/useSubscriptionTiers'
import type { TierName } from '@/features/subscription/types'

// Tiers to display in the preview
const previewTiers: TierName[] = ['premium', 'household_3', 'household_5']

// Key features to highlight for each tier
const tierHighlights: Record<TierName, string[]> = {
  premium: ['Unlimited bank accounts', 'Unlimited automation rules', 'Ad-free experience'],
  household_2: ['Unlimited bank accounts', '2 household members', 'AI-powered insights'],
  household_3: ['Unlimited bank accounts', '3 household members', 'AI-powered insights'],
  household_4: ['Unlimited bank accounts', '4 household members', 'Priority support'],
  household_5: [
    'Unlimited bank accounts',
    '5+ household members',
    'All premium features',
  ],
}

export function PricingPreview() {
  const { tiers, loading, error } = useSubscriptionTiers()

  if (loading) {
    return (
      <section className="pricing-preview-section bg-slate-50 py-20 dark:bg-slate-800/50">
        <div className="mx-auto max-w-7xl px-4 text-center">
          <div className="animate-pulse">
            <div className="mx-auto h-8 w-48 rounded bg-slate-200 dark:bg-slate-700" />
            <div className="mx-auto mt-4 h-4 w-64 rounded bg-slate-200 dark:bg-slate-700" />
          </div>
        </div>
      </section>
    )
  }

  if (error || !tiers || !tiers.tiers) {
    return null // Silently fail - pricing page is still accessible
  }

  return (
    <section
      className="pricing-preview-section bg-slate-50 py-24 dark:bg-slate-800/50 sm:py-32"
      aria-labelledby="pricing-preview-heading"
    >
      <div className="mx-auto max-w-7xl px-4 sm:px-6 lg:px-8">
        {/* Section Header */}
        <div className="text-center">
          <h2
            id="pricing-preview-heading"
            className="text-3xl font-bold tracking-tight text-slate-900 dark:text-white sm:text-4xl lg:text-5xl"
          >
            Simple, Transparent Pricing
          </h2>
          <p className="mx-auto mt-6 max-w-2xl text-lg leading-relaxed text-slate-600 dark:text-slate-400">
            Start with a 2-month free trial. No credit card required.
          </p>
        </div>

        {/* Pricing Cards */}
        <div className="mt-16 grid gap-8 lg:grid-cols-3">
          {previewTiers.map((tierKey) => {
            const tierData = tiers.tiers[tierKey]
            if (!tierData) return null

            const isPopular = tierKey === 'household_5'
            const highlights = tierHighlights[tierKey]

            return (
              <div
                key={tierKey}
                className={`relative rounded-3xl p-8 shadow-sm transition-shadow hover:shadow-md lg:p-10 ${
                  isPopular
                    ? 'border-2 border-blue-600 bg-white shadow-md dark:bg-slate-800'
                    : 'bg-white dark:bg-slate-800'
                }`}
              >
                {/* Popular Badge */}
                {isPopular && (
                  <div className="absolute -top-4 left-1/2 -translate-x-1/2 rounded-full bg-blue-600 px-4 py-1 text-sm font-medium text-white">
                    Best Value
                  </div>
                )}

                {/* Tier Name */}
                <h3 className="text-xl font-semibold text-slate-900 dark:text-white">
                  {tierData.display_name}
                </h3>

                {/* Price */}
                <div className="mt-4 flex items-baseline">
                  <span className="text-4xl font-bold text-slate-900 dark:text-white">
                    ${tierData.pricing.price_monthly.toFixed(2)}
                  </span>
                  <span className="ml-1 text-slate-600 dark:text-slate-400">/month</span>
                </div>

                {/* Description */}
                <p className="mt-2 text-sm text-slate-600 dark:text-slate-400">
                  {tierData.description}
                </p>

                {/* Highlights */}
                <ul className="mt-6 space-y-3">
                  {highlights.map((feature, index) => (
                    <li key={index} className="flex items-center gap-2 text-sm">
                      <Check
                        className="h-4 w-4 flex-shrink-0 text-emerald-500"
                        aria-hidden="true"
                      />
                      <span className="text-slate-700 dark:text-slate-300">
                        {feature}
                      </span>
                    </li>
                  ))}
                </ul>
              </div>
            )
          })}
        </div>

        {/* Link to Full Pricing */}
        <div className="mt-12 text-center">
          <Link
            to="/pricing"
            className="inline-flex items-center gap-2 text-blue-600 hover:text-blue-500 dark:text-blue-400 dark:hover:text-blue-300"
          >
            Compare All Plans
            <ArrowRight className="h-4 w-4" aria-hidden="true" />
          </Link>
        </div>
      </div>
    </section>
  )
}
