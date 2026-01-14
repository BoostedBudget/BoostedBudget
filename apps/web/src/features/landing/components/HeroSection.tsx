/**
 * HeroSection Component
 *
 * Above-the-fold hero section with compelling headline, CTAs, and trust badges.
 * Issue #699: Create Marketing Landing Page for First-Time Visitors
 */

import { useClerk } from '@clerk/react-router'
import { Button } from '@/components/ui/button'
import { Rocket, Shield, CreditCard } from 'lucide-react'

interface HeroSectionProps {
  onScrollToHowItWorks?: () => void
}

export function HeroSection({ onScrollToHowItWorks }: HeroSectionProps) {
  const { openSignUp } = useClerk()

  const handleGetStarted = () => {
    openSignUp()
  }

  const handleScrollToHowItWorks = () => {
    if (onScrollToHowItWorks) {
      onScrollToHowItWorks()
    } else {
      // Fallback: scroll to how-it-works section
      const element = document.getElementById('how-it-works')
      if (element) {
        element.scrollIntoView({ behavior: 'smooth' })
      }
    }
  }

  return (
    <section
      className="hero-section relative overflow-hidden bg-gradient-to-br from-slate-900 via-slate-800 to-slate-900 pb-24 pt-32 sm:pb-32 sm:pt-40 lg:pb-40 lg:pt-48"
      aria-labelledby="hero-heading"
    >
      {/* Decorative background blobs */}
      <div
        className="hero-blob absolute -left-40 -top-40 h-96 w-96 rounded-full bg-blue-600/20 blur-3xl"
        aria-hidden="true"
      />
      <div
        className="hero-blob-2 absolute -right-40 top-1/3 h-80 w-80 rounded-full bg-emerald-600/15 blur-3xl"
        aria-hidden="true"
      />
      <div
        className="hero-blob absolute bottom-0 left-1/3 h-64 w-64 rounded-full bg-blue-500/10 blur-3xl"
        aria-hidden="true"
      />

      {/* Background gradient overlays */}
      <div className="absolute inset-0 bg-[radial-gradient(ellipse_at_top_right,_var(--tw-gradient-stops))] from-blue-600/20 via-transparent to-transparent" />
      <div className="absolute inset-0 bg-[radial-gradient(ellipse_at_bottom_left,_var(--tw-gradient-stops))] from-emerald-600/10 via-transparent to-transparent" />

      <div className="relative mx-auto max-w-7xl px-4 sm:px-6 lg:px-8">
        <div className="text-center">
          {/* Badge */}
          <div className="mb-8 inline-flex items-center gap-2 rounded-full bg-blue-500/10 px-4 py-2 text-sm text-blue-400 ring-1 ring-blue-500/20">
            <Rocket className="h-4 w-4" aria-hidden="true" />
            <span>Smart budgeting for modern life</span>
          </div>

          {/* Main Headline */}
          <h1
            id="hero-heading"
            className="mx-auto max-w-4xl text-4xl font-bold tracking-tight text-white sm:text-5xl lg:text-6xl xl:text-7xl"
          >
            Take Control of Your{' '}
            <span className="bg-gradient-to-r from-blue-400 to-emerald-400 bg-clip-text text-transparent">
              Money
            </span>
          </h1>

          {/* Subheadline */}
          <p className="mx-auto mt-8 max-w-2xl text-lg leading-relaxed text-slate-300 sm:text-xl">
            Automate your budget, connect all your accounts, and achieve your financial
            goals. Join thousands who have transformed their relationship with money.
          </p>

          {/* CTA Buttons */}
          <div className="mt-12 flex flex-col items-center justify-center gap-4 sm:flex-row">
            <Button
              size="lg"
              onClick={handleGetStarted}
              className="w-full bg-blue-600 px-10 py-6 text-lg font-semibold shadow-lg shadow-blue-500/25 hover:bg-blue-500 hover:shadow-blue-500/30 sm:w-auto"
            >
              Start Free Today
            </Button>
            <Button
              variant="outline"
              size="lg"
              onClick={handleScrollToHowItWorks}
              className="w-full border-slate-600 px-10 py-6 text-lg text-slate-300 hover:bg-slate-800 hover:text-white sm:w-auto"
            >
              See How It Works
            </Button>
          </div>

          {/* Trust Badges */}
          <div className="mt-16 flex flex-wrap items-center justify-center gap-8 text-sm text-slate-400">
            <div className="flex items-center gap-2">
              <CreditCard className="h-4 w-4" aria-hidden="true" />
              <span>No credit card required</span>
            </div>
            <div className="flex items-center gap-2">
              <Shield className="h-4 w-4" aria-hidden="true" />
              <span>GDPR compliant</span>
            </div>
            <div className="flex items-center gap-2">
              <span className="font-semibold text-emerald-400">2-month</span>
              <span>free trial</span>
            </div>
          </div>
        </div>
      </div>
    </section>
  )
}
