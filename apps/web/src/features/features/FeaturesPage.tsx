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
 * Claims discipline (mirrors McpPage): describe account access, confirmation and
 * practical limits without exposing service topology, internal operation names,
 * implementation details, or unfinished roadmap items. Never promise that an
 * agent can move money, cancel a subscription, or accept a household invite.
 */

import { useEffect } from 'react'
import { Link } from 'react-router'
import { useClerk } from '@clerk/react-router'
import { ArrowRight, BarChart3, Bot, Check, Landmark, LayoutDashboard, Users, Zap } from 'lucide-react'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics'

const CORE_FEATURES = [
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
    title: 'Categorization That Follows Your Rules',
    description:
      "Every transaction arrives with a category from Plaid. Write a rule for merchant, amount, or description, and it takes over for every match — unless you've already categorized or reviewed that transaction yourself.",
  },
  {
    icon: Zap,
    title: 'Automation You Can Audit',
    description:
      'Build rules that flag budget risks and suggest transfers. Every rule is one you wrote, so you always know why it fired.',
  },
  {
    icon: Users,
    title: 'Shared Household Budgeting',
    description:
      'One plan across 2 to 5+ members. Each person chooses which accounts to share, and can hide any account from the household at any time.',
  },
]

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

  useEffect(() => {
    trackEvent(ANALYTICS_EVENTS.FEATURES_PAGE_VIEWED)
  }, [trackEvent])

  const handlePrimaryCtaClick = () => {
    trackEvent(ANALYTICS_EVENTS.FEATURES_CTA_CLICKED)
    openSignUp()
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