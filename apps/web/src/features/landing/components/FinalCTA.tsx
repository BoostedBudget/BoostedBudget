/**
 * FinalCTA Component
 *
 * Final call-to-action section with risk reversal messaging.
 * Issue #699: Create Marketing Landing Page for First-Time Visitors
 */

import { useClerk } from '@clerk/react-router'
import { Button } from '@/components/ui/button'
import { Rocket, CreditCard, Calendar, X } from 'lucide-react'

export function FinalCTA() {
  const { openSignUp } = useClerk()

  const handleGetStarted = () => {
    openSignUp()
  }

  return (
    <section
      className="final-cta-section relative overflow-hidden bg-gradient-to-br from-blue-600 to-blue-700 py-24 sm:py-32"
      aria-labelledby="final-cta-heading"
    >
      {/* Background Pattern */}
      <div className="absolute inset-0 bg-[url('data:image/svg+xml;base64,PHN2ZyB3aWR0aD0iNjAiIGhlaWdodD0iNjAiIHZpZXdCb3g9IjAgMCA2MCA2MCIgeG1sbnM9Imh0dHA6Ly93d3cudzMub3JnLzIwMDAvc3ZnIj48ZyBmaWxsPSJub25lIiBmaWxsLXJ1bGU9ImV2ZW5vZGQiPjxnIGZpbGw9IiNmZmZmZmYiIGZpbGwtb3BhY2l0eT0iMC4wNSI+PHBhdGggZD0iTTM2IDM0djItSDI0di0yaDEyek0zNiAzMHYySDI0di0yaDEyek0zNiAyNnYySDI0di0yaDEyeiIvPjwvZz48L2c+PC9zdmc+')] opacity-30" />

      <div className="relative mx-auto max-w-4xl px-4 text-center sm:px-6 lg:px-8">
        {/* Icon */}
        <div className="mx-auto mb-8 flex h-20 w-20 items-center justify-center rounded-full bg-white/10 backdrop-blur-sm">
          <Rocket className="h-10 w-10 text-white" aria-hidden="true" />
        </div>

        {/* Heading */}
        <h2
          id="final-cta-heading"
          className="text-3xl font-bold tracking-tight text-white sm:text-4xl lg:text-5xl"
        >
          Ready to Take Control of Your Finances?
        </h2>

        {/* Subheading */}
        <p className="mx-auto mt-6 max-w-2xl text-lg leading-relaxed text-blue-100">
          Join thousands of users who have transformed their relationship with money.
          Start your free trial today and see the difference.
        </p>

        {/* CTA Button */}
        <div className="mt-10">
          <Button
            size="lg"
            onClick={handleGetStarted}
            className="bg-white px-12 py-7 text-lg font-semibold text-blue-600 shadow-lg shadow-blue-900/20 hover:bg-blue-50 hover:shadow-xl"
          >
            Start Free Today
          </Button>
        </div>

        {/* Trust/Risk Reversal Points */}
        <div className="mt-12 flex flex-wrap items-center justify-center gap-8 text-sm text-blue-100">
          <div className="flex items-center gap-2">
            <Calendar className="h-4 w-4" aria-hidden="true" />
            <span>2-month free trial</span>
          </div>
          <div className="flex items-center gap-2">
            <CreditCard className="h-4 w-4" aria-hidden="true" />
            <span>No credit card required</span>
          </div>
          <div className="flex items-center gap-2">
            <X className="h-4 w-4" aria-hidden="true" />
            <span>Cancel anytime</span>
          </div>
        </div>
      </div>
    </section>
  )
}
