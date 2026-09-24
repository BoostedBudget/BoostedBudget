/**
 * FeaturesSection Component
 *
 * Displays key product features with benefit-focused copy in a 2-column grid
 * (the last card spans full width only when the card count is odd, #1411/#1666).
 * Issue #699: Create Marketing Landing Page for First-Time Visitors
 */

import { Landmark, BarChart3, Zap, LayoutDashboard, Users, Bot } from 'lucide-react'
import { MARKETING_FEATURES, type FeatureIconKey } from '../data/marketingContent'

// The ICONS live here, not in the data module: the generator imports marketingContent.ts
// and must never pull `lucide-react` (an entire icon set) into a Node-side build.
const FEATURE_ICONS: Record<FeatureIconKey, typeof Landmark> = {
  accounts: Landmark,
  dashboard: LayoutDashboard,
  categorization: BarChart3,
  automation: Zap,
  household: Users,
  agent: Bot,
}

// eslint-disable-next-line react-refresh/only-export-components
export const features = MARKETING_FEATURES.map(({ key, title, description }) => ({
  icon: FEATURE_ICONS[key],
  title,
  description,
}))

/** Last card spans both columns only for an odd card count, so the grid closes cleanly. */
// eslint-disable-next-line react-refresh/only-export-components
export function cardSpanClass(index: number, total: number): string {
  return index === total - 1 && total % 2 === 1 ? 'sm:col-span-2' : ''
}

export function FeaturesSection() {
  return (
    <section
      className="features-section bg-white py-24 dark:bg-slate-900 sm:py-32"
      aria-labelledby="features-heading"
    >
      <div className="mx-auto max-w-7xl px-4 sm:px-6 lg:px-8">
        {/* Section Header */}
        <div className="mx-auto max-w-2xl text-center">
          <h2
            id="features-heading"
            className="text-3xl font-bold tracking-tight text-slate-900 dark:text-white sm:text-4xl lg:text-5xl"
          >
            Why Choose BoostedBudget?
          </h2>
          <p className="mt-6 text-lg leading-relaxed text-slate-600 dark:text-slate-400">
            Everything you need to take control of your finances, all in one place.
          </p>
        </div>

        {/* Features Grid */}
        <div className="mt-20 grid gap-8 sm:grid-cols-2 lg:gap-12">
          {features.map((feature, index) => (
            <div
              key={index}
              className={`group relative rounded-3xl bg-slate-50 p-8 text-center shadow-sm transition-all hover:bg-slate-100 hover:shadow-md dark:bg-slate-800/50 dark:hover:bg-slate-800 lg:p-10 ${cardSpanClass(
                index,
                features.length
              )}`}
            >
              {/* Icon */}
              <div className="mx-auto mb-6 inline-flex h-14 w-14 items-center justify-center rounded-2xl bg-blue-600 text-white shadow-lg shadow-blue-500/25">
                <feature.icon className="h-7 w-7" aria-hidden="true" />
              </div>

              {/* Title */}
              <h3 className="text-xl font-semibold text-slate-900 dark:text-white lg:text-2xl">
                {feature.title}
              </h3>

              {/* Description */}
              <p className="mt-3 text-slate-600 dark:text-slate-400 lg:text-lg">
                {feature.description}
              </p>
            </div>
          ))}
        </div>
      </div>
    </section>
  )
}
