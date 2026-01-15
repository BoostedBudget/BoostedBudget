/**
 * HowItWorks Component
 *
 * 3-step process visualization showing how to get started.
 * Issue #699: Create Marketing Landing Page for First-Time Visitors
 */

import { Link2, FolderKanban, Sparkles } from 'lucide-react'

const steps = [
  {
    number: '1',
    icon: Link2,
    title: 'Connect',
    description:
      'Securely link your bank accounts, credit cards, and investment accounts in seconds using Plaid.',
  },
  {
    number: '2',
    icon: FolderKanban,
    title: 'Organize',
    description:
      'Set up budgets, categorize spending, and define your financial goals. We make it easy to get started.',
  },
  {
    number: '3',
    icon: Sparkles,
    title: 'Automate',
    description:
      'Create automation rules that work while you sleep. Get smart suggestions, alerts, and stay on track effortlessly.',
  },
]

export function HowItWorks() {
  return (
    <section
      className="how-it-works-section bg-slate-50 py-24 dark:bg-slate-800/50 sm:py-32"
      aria-labelledby="how-it-works-heading"
    >
      <div className="mx-auto max-w-7xl px-4 sm:px-6 lg:px-8">
        {/* Section Header */}
        <div className="mx-auto max-w-2xl text-center">
          <h2
            id="how-it-works-heading"
            className="text-3xl font-bold tracking-tight text-slate-900 dark:text-white sm:text-4xl lg:text-5xl"
          >
            How It Works
          </h2>
          <p className="mt-6 text-lg leading-relaxed text-slate-600 dark:text-slate-400">
            Get started in minutes. No complicated setup required.
          </p>
        </div>

        {/* Steps */}
        <div className="mt-20">
          <div className="flex flex-col items-center gap-12 lg:flex-row lg:items-start lg:justify-center lg:gap-8 xl:gap-16">
            {steps.map((step, index) => (
              <div
                key={index}
                className="relative flex max-w-sm flex-col items-center text-center"
              >
                {/* Connector line - desktop only */}
                {index < steps.length - 1 && (
                  <div
                    className="absolute left-[calc(50%+4rem)] top-12 hidden h-0.5 w-32 bg-gradient-to-r from-blue-400 to-blue-200 lg:block xl:w-48"
                    aria-hidden="true"
                  />
                )}

                {/* Step Circle with Icon */}
                <div className="relative mb-8">
                  <div className="flex h-24 w-24 items-center justify-center rounded-full bg-gradient-to-br from-blue-50 to-blue-100 dark:from-blue-900/30 dark:to-blue-800/30">
                    <div className="flex h-16 w-16 items-center justify-center rounded-full bg-blue-600 text-white shadow-lg shadow-blue-500/30">
                      <step.icon className="h-8 w-8" aria-hidden="true" />
                    </div>
                  </div>
                  {/* Step Number Badge */}
                  <span className="absolute -right-1 -top-1 flex h-8 w-8 items-center justify-center rounded-full bg-emerald-500 text-sm font-bold text-white shadow-md">
                    {step.number}
                  </span>
                </div>

                {/* Title */}
                <h3 className="text-2xl font-semibold text-slate-900 dark:text-white">
                  {step.title}
                </h3>

                {/* Description */}
                <p className="mt-4 leading-relaxed text-slate-600 dark:text-slate-400">
                  {step.description}
                </p>
              </div>
            ))}
          </div>
        </div>
      </div>
    </section>
  )
}
