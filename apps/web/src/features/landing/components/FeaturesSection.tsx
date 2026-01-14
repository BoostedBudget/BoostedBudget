/**
 * FeaturesSection Component
 *
 * Displays key product features with benefit-focused copy in a 2x2 grid.
 * Issue #699: Create Marketing Landing Page for First-Time Visitors
 */

import { Landmark, BarChart3, Zap, LayoutDashboard } from 'lucide-react'

const features = [
  {
    icon: Landmark,
    title: 'All Your Accounts in One Place',
    description:
      'Connect all your bank accounts, credit cards, and investments. Get a complete picture of your finances at a glance.',
  },
  {
    icon: LayoutDashboard,
    title: 'Customizable Dashboard',
    description:
      'Personalize your financial overview with a diverse library of widgets. Visualize and analyze your data the way you want.',
  },
  {
    icon: BarChart3,
    title: 'Smart Categorization',
    description:
      'Our intelligent system automatically categorizes your transactions and learns from your corrections over time.',
  },
  {
    icon: Zap,
    title: 'Powerful Automation',
    description:
      'Create automation rules that suggest transfers, categorize transactions, and alert you when budgets are at risk.',
  },
]

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

        {/* Features Grid - 2x2 */}
        <div className="mt-20 grid gap-8 sm:grid-cols-2 lg:gap-12">
          {features.map((feature, index) => (
            <div
              key={index}
              className="group relative rounded-3xl bg-slate-50 p-8 text-center shadow-sm transition-all hover:bg-slate-100 hover:shadow-md dark:bg-slate-800/50 dark:hover:bg-slate-800 lg:p-10"
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
