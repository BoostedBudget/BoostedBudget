/**
 * LandingPage Component
 *
 * Marketing landing page for first-time visitors.
 * Displays a professional, conversion-optimized page with:
 * - Sticky header with navigation
 * - Hero section with CTAs
 * - Feature highlights (4 in 2x2 grid)
 * - Trust bar with hover tooltips
 * - How it works visualization
 * - Pricing preview
 * - FAQ section
 * - Final call-to-action
 * - Footer
 *
 * Issue #699: Create Marketing Landing Page for First-Time Visitors
 */

import { useRef, useCallback } from 'react'
import { useDocumentHead } from '@/shared/hooks/useDocumentHead'
import { LandingHeader } from './components/LandingHeader'
import { HeroSection } from './components/HeroSection'
import { FeaturesSection } from './components/FeaturesSection'
import { McpBand } from './components/McpBand'
import { SocialProof } from './components/SocialProof'
import { HowItWorks } from './components/HowItWorks'
import { PricingPreview } from './components/PricingPreview'
import { FAQSection } from './components/FAQSection'
import { FinalCTA } from './components/FinalCTA'
import './LandingPage.css'

const PAGE_TITLE = 'BoostedBudget - Smart Budgeting & Expense Tracking | Free Trial'
const PAGE_DESCRIPTION =
  'Take control of your finances with BoostedBudget. Connect all your bank accounts, automate your budget, and achieve your financial goals. Start your free 60-day trial today.'
const CANONICAL_URL = 'https://boostedbudget.com/'

/**
 * Wave SVG divider component for smooth section transitions
 */
function WaveDivider({ flip = false, className = '' }: { flip?: boolean; className?: string }) {
  return (
    <div className={`wave-divider ${flip ? 'wave-divider--flip' : ''} ${className}`} aria-hidden="true">
      <svg
        viewBox="0 0 1440 120"
        fill="none"
        xmlns="http://www.w3.org/2000/svg"
        preserveAspectRatio="none"
        className="h-16 w-full sm:h-20 lg:h-24"
      >
        <path
          d="M0 120L48 108C96 96 192 72 288 60C384 48 480 48 576 54C672 60 768 72 864 78C960 84 1056 84 1152 78C1248 72 1344 60 1392 54L1440 48V120H1392C1344 120 1248 120 1152 120C1056 120 960 120 864 120C768 120 672 120 576 120C480 120 384 120 288 120C192 120 96 120 48 120H0Z"
          fill="currentColor"
        />
      </svg>
    </div>
  )
}

export function LandingPage() {
  useDocumentHead(PAGE_TITLE, PAGE_DESCRIPTION, CANONICAL_URL)

  const howItWorksRef = useRef<HTMLDivElement>(null)

  const scrollToHowItWorks = useCallback(() => {
    if (howItWorksRef.current) {
      howItWorksRef.current.scrollIntoView({ behavior: 'smooth' })
    }
  }, [])

  return (
    <div className="landing-page">
      {/* Sticky Header */}
      <LandingHeader />

      <main role="main">
        {/* Hero Section - Above the fold */}
        <HeroSection onScrollToHowItWorks={scrollToHowItWorks} />

        {/* Wave transition from dark hero to light features */}
        <WaveDivider className="text-white dark:text-slate-900 -mt-1" />

        {/* Features Section - Benefits-first approach (4 features in 2x2) */}
        <div id="features">
          <FeaturesSection />
        </div>

        {/* MCP Band - Introduces the remote MCP server and links to /mcp (#1411) */}
        <McpBand />

        {/* Compact Trust Bar with hover tooltips */}
        <SocialProof />

        {/* How It Works - 3-step process */}
        <div id="how-it-works" ref={howItWorksRef}>
          <HowItWorks />
        </div>

        {/* Pricing Preview - Tier comparison teaser */}
        <PricingPreview />

        {/* FAQ Section - Common questions with schema */}
        <div id="faq">
          <FAQSection />
        </div>

        {/* Wave transition to final CTA */}
        <WaveDivider flip className="text-blue-600 -mb-1" />

        {/* Final CTA - Risk reversal messaging */}
        <FinalCTA />
      </main>

    </div>
  )
}
