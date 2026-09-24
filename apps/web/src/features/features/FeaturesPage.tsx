/**
 * FeaturesPage — the public /features page (Issue #1666).
 *
 * A real routed page, not a landing anchor: App.tsx computes
 * `isLandingPage = pathname === '/' && !isAuthenticated`, so a signed-in user is
 * redirected from `/` to /dashboard and the landing's `#features` hash dies
 * before a feature card is ever scrolled to. This page is the destination the
 * footer links to instead — reachable signed out and signed in.
 *
 * Content is the landing page's core feature set expanded into its own page,
 * plus a dedicated section for the agentic capability (the remote MCP server at
 * /mcp).
 *
 * Prose comes from the single source (`features/landing/data/marketingContent.ts`),
 * exactly as `FeaturesSection.tsx` does it: the six title/description pairs live
 * there, and the lucide icon COMPONENTS are composed here, keyed by the stable
 * `key`. Duplicating the sentences here is what this page used to do, and it is
 * how the landing copy and this page could drift apart.
 *
 * Claims discipline (mirrors McpPage): describe account access, confirmation and
 * practical limits without exposing service topology, internal operation names,
 * implementation details, or unfinished roadmap items. Never promise that an
 * agent can move money, cancel a subscription, or accept a household invite.
 */

import { useEffect } from 'react'
import { Link, useNavigate } from 'react-router'
import { useAuth, useClerk } from '@clerk/react-router'
import { ArrowRight, BarChart3, Bot, Check, Landmark, LayoutDashboard, Users, Zap } from 'lucide-react'
import { useDocumentHead } from '@/shared/hooks/useDocumentHead'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics'
import { MARKETING_FEATURES, type FeatureIconKey } from '@/features/landing/data/marketingContent'

const PAGE_TITLE = 'Features — BoostedBudget'
const PAGE_DESCRIPTION =
  'Connect every account, automate what you can, and hand the busywork to your AI agent. It can read everything — and changes nothing without your confirmation.'
const CANONICAL_URL = 'https://boostedbudget.com/features'

// The ICONS live here, not in the data module: the llms-full.txt generator imports
// marketingContent.ts and must never pull `lucide-react` into a Node-side build.
const FEATURE_ICONS: Record<FeatureIconKey, typeof Landmark> = {
  accounts: Landmark,
  dashboard: LayoutDashboard,
  categorization: BarChart3,
  automation: Zap,
  household: Users,
  agent: Bot,
}

// The five non-agentic feature cards, prose sourced from marketingContent.ts and
// paired with this page's icons. The agentic copy is rendered in its own section.
const CORE_FEATURES = MARKETING_FEATURES.filter((feature) => feature.key !== 'agent').map(
  ({ key, title, description }) => ({
    icon: FEATURE_ICONS[key],
    title,
    description,
  }),
)

const AGENT_HIGHLIGHTS = [
  'Read across your accounts, transactions, budgets, and portfolio',
  'Recategorize, tag, and save repeatable reviews — each change waits for your confirmation',
  'Credentials are injected server-side and never shown to the agent',
]

function Section({ id, title, children }: { id: string; title: string; children: React.ReactNode }) {
  return (
    <section aria-labelledby={id} className="space-y-6">
      <h2 id={id} className="scroll-mt-24 text-2xl font-semibold text-foreground">
        {title}
      </h2>
      {children}
    </section>
  )
}

export function FeaturesPage() {
  const trackEvent = useTrackEvent()
  const { openSignUp } = useClerk()
  const { isSignedIn } = useAuth()
  const navigate = useNavigate()

  useDocumentHead(PAGE_TITLE, PAGE_DESCRIPTION, CANONICAL_URL)

  useEffect(() => {
    trackEvent(ANALYTICS_EVENTS.FEATURES_PAGE_VIEWED)
  }, [trackEvent])

  const handlePrimaryCtaClick = () => {
    trackEvent(ANALYTICS_EVENTS.FEATURES_CTA_CLICKED)
    // This page is reachable signed in, so a signed-in visitor gets the app
    // instead of a sign-up modal for an account they already have.
    if (isSignedIn) {
      navigate('/dashboard')
    } else {
      openSignUp()
    }
  }

  return (
    <div className="mx-auto max-w-6xl px-4 py-16 sm:px-8">
      <header className="mx-auto max-w-3xl text-center">
        <h1 className="text-4xl font-bold tracking-tight text-foreground sm:text-5xl">
          Everything you need to run your finances
        </h1>
        <p className="mx-auto mt-6 max-w-2xl text-lg text-muted-foreground">
          Connect every account, automate what you can, and hand the busywork to your AI agent. It can read
          everything — and changes nothing without your confirmation.
        </p>
        <div className="mt-8 flex flex-col items-center justify-center gap-4 sm:flex-row">
          <button
            type="button"
            onClick={handlePrimaryCtaClick}
            className="inline-flex items-center gap-2 rounded-lg bg-primary px-6 py-3 text-base font-semibold text-primary-foreground transition-opacity hover:opacity-90 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
          >
            Start Free Today
          </button>
          <Link
            to="/mcp"
            className="inline-flex items-center gap-2 text-base font-semibold text-foreground hover:text-primary"
          >
            Explore the MCP server
            <ArrowRight className="h-4 w-4" aria-hidden="true" />
          </Link>
        </div>
        <p className="mt-4 text-sm text-muted-foreground">
          60-day free trial. No credit card required. Cancel anytime.
        </p>
      </header>

      <div className="mt-20 space-y-16">
        <Section id="core-features" title="What BoostedBudget does">
          <div className="grid gap-6 sm:grid-cols-2">
            {CORE_FEATURES.map((feature) => (
              <div key={feature.title} className="rounded-2xl border border-border bg-card p-6">
                <div className="mb-4 inline-flex h-11 w-11 items-center justify-center rounded-xl bg-primary text-primary-foreground">
                  <feature.icon className="h-5 w-5" aria-hidden="true" />
                </div>
                <h3 className="text-lg font-semibold text-foreground">{feature.title}</h3>
                <p className="mt-2 text-muted-foreground">{feature.description}</p>
              </div>
            ))}
          </div>
        </Section>

        <Section id="your-agent" title="Your agent can do the digging">
          <div className="flex items-start gap-4">
            <div className="inline-flex h-11 w-11 shrink-0 items-center justify-center rounded-xl bg-primary text-primary-foreground">
              <Bot className="h-5 w-5" aria-hidden="true" />
            </div>
            <p className="text-foreground">
              BoostedBudget runs a remote MCP (Model Context Protocol) server. Point Claude, Cursor, VS Code, or
              any MCP-compatible client at one URL, sign in once, and your agent can work across your finances in
              plain language — no exporting statements, no copy-pasting numbers into a chat window.
            </p>
          </div>
          <ul className="grid gap-2 sm:grid-cols-1">
            {AGENT_HIGHLIGHTS.map((highlight) => (
              <li key={highlight} className="flex items-start gap-2 text-foreground">
                <Check className="mt-0.5 h-4 w-4 shrink-0 text-primary" aria-hidden="true" />
                {highlight}
              </li>
            ))}
          </ul>
          <p className="text-muted-foreground">
            Every proposed change waits for you to confirm it, confirmations are single-use, and BoostedBudget
            never moves money.
          </p>
          <Link
            to="/mcp"
            className="inline-flex items-center gap-2 font-semibold text-primary hover:text-primary/80"
          >
            See how the MCP server works
            <ArrowRight className="h-4 w-4" aria-hidden="true" />
          </Link>
        </Section>
      </div>
    </div>
  )
}
