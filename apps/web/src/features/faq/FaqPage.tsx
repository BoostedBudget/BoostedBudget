/**
 * FaqPage — the public /faq page (Issue #1666).
 *
 * A real routed page, not a landing anchor: App.tsx computes
 * `isLandingPage = pathname === '/' && !isAuthenticated`, so a signed-in user is
 * redirected from `/` to /dashboard and the landing's `#faq` hash dies before an
 * answer is ever opened. This page is the destination the footer links to
 * instead — reachable signed out and signed in.
 *
 * Content is the landing page's seven Q&As (copied verbatim from
 * `features/landing/components/FAQSection.tsx`) plus three agentic Q&As about
 * the remote MCP server at /mcp.
 *
 * ⚠ UNIFICATION (Todo 14): this same prose is currently duplicated between this
 * file, FAQSection.tsx and FeaturesSection.tsx. Todo 14 extracts a single
 * source of truth to `features/landing/data/marketingContent.ts`, at which point
 * `FAQ_ITEMS` below becomes an import and the components consume it. To keep
 * that a swap rather than a rewrite, the content here is a plain
 * `{ question, answer, link? }` data array with no JSX — the disclosure markup
 * is rendered from it, not embedded in it.
 *
 * Claims discipline (mirrors McpPage/FeaturesPage): every agentic answer states
 * that changes wait for confirmation, that BoostedBudget never moves money, that
 * credentials are injected server-side and never visible to the agent, and that
 * access can be revoked. No answer promises that an agent can move money, cancel
 * a subscription, or accept a household invite.
 */

import { useEffect, useState } from 'react'
import { Link } from 'react-router'
import { useClerk } from '@clerk/react-router'
import { ChevronDown } from 'lucide-react'
import { useDocumentHead } from '@/shared/hooks/useDocumentHead'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics'

const PAGE_TITLE = 'FAQ — BoostedBudget'
const PAGE_DESCRIPTION =
  'Answers about security, billing, household sharing, and handing the busywork to your AI agent. BoostedBudget never moves money.'
const CANONICAL_URL = 'https://boostedbudget.com/faq'

interface FaqItem {
  question: string
  answer: string
  /** Optional inline link appended to the answer. The JSON-LD text stays the plain answer. */
  link?: { label: string; to: string }
}

// Todo 14 swaps this literal for an import from features/landing/data/marketingContent.ts.
const FAQ_ITEMS: FaqItem[] = [
  {
    question: 'Is my bank data secure?',
    answer:
      'Yes, absolutely. We use Plaid, the same trusted service used by Venmo, Robinhood, and thousands of other apps, to securely connect to your bank. We never see or store your bank login credentials. All data is encrypted with 256-bit encryption, the same standard used by banks.',
  },
  {
    question: 'How does BoostedBudget connect to my bank?',
    answer:
      'We use Plaid to establish secure, read-only connections to over 12,000 financial institutions. When you link an account, you authenticate directly with your bank through Plaid - we never see your username or password.',
  },
  {
    question: 'Can I cancel anytime?',
    answer:
      'Yes, you can cancel your subscription at any time with no penalty. Your data remains accessible until the end of your billing period, and you can export all your data before canceling.',
  },
  {
    question: 'What happens to my data if I cancel?',
    answer:
      'Cancelling and deleting are two different things. When you cancel, your account switches to read-only: everything you have already recorded stays there, you can still view and export it, and we keep it that way for at least 6 months so you can pick up where you left off by resubscribing. We are not automatically deleting cancelled accounts today, so in practice your data is kept past that 6-month window rather than deleted at the end of it - see Section 8.4 of our Privacy Policy for exactly what an automatic deletion would destroy, and when we will start doing it. If you want your data gone, you can request deletion at any time: we hold it through a 30-day grace period in case you change your mind, then permanently delete it.',
  },
  {
    question: 'Is there a free trial?',
    answer:
      'Yes! All new accounts start with a 60-day free trial of our Premium features. No credit card is required to start. You can upgrade, downgrade, or cancel at any time during or after your trial.',
  },
  {
    question: 'Do you sell my financial data?',
    answer:
      'Never. Your financial data is yours and yours alone. We do not sell, share, or monetize your personal financial information. Our business model is simple: you pay for the service, and that is how we make money.',
  },
  {
    question: 'Can my family share one subscription?',
    answer:
      'Yes. Household plans cover 2 to 5+ members on one subscription. Each member chooses which accounts to share, and can hide any account at any time. Accepting a household invite is always a deliberate action you take in the app.',
  },
  {
    question: 'Can my AI agent help me manage my budget?',
    answer:
      'Yes. BoostedBudget runs a remote MCP server that lets an AI agent read across your accounts, transactions, budgets, and portfolio in plain language. It can recategorize, tag, and save repeatable reviews — but every proposed change waits for your explicit confirmation before it is applied, and each confirmation is single-use.',
    link: { label: 'See how the MCP server works', to: '/mcp' },
  },
  {
    question: 'Can an AI agent move my money?',
    answer:
      'No. BoostedBudget never moves money, with or without an agent. A connected agent can read your data and propose changes for you to review and confirm, but it has no ability to transfer funds, make payments, or act on your behalf without that confirmation step you control.',
    link: { label: 'See how the MCP server works', to: '/mcp' },
  },
  {
    question: 'Is it safe to let an AI agent see my finances?',
    answer:
      'Yes — and you stay in control. Your BoostedBudget credentials are injected server-side after you sign in, so they are never visible to the agent, and calls are recorded in an audit log on a best-effort basis. You can revoke the agent’s access at any time from your sign-in provider’s security dashboard; it can take about 30–60 seconds to take effect.',
    link: { label: 'See how the MCP server works', to: '/mcp' },
  },
]

// Generate JSON-LD schema for FAQPage. Kept in lockstep with the rendered
// questions by sourcing both from FAQ_ITEMS.
function generateFaqSchema(items: FaqItem[]) {
  return {
    '@context': 'https://schema.org',
    '@type': 'FAQPage',
    mainEntity: items.map((item) => ({
      '@type': 'Question',
      name: item.question,
      acceptedAnswer: {
        '@type': 'Answer',
        text: item.answer,
      },
    })),
  }
}

export function FaqPage() {
  const trackEvent = useTrackEvent()
  const { openSignUp } = useClerk()
  const [openIndex, setOpenIndex] = useState<number | null>(null)

  useDocumentHead(PAGE_TITLE, PAGE_DESCRIPTION, CANONICAL_URL)

  useEffect(() => {
    trackEvent(ANALYTICS_EVENTS.FAQ_PAGE_VIEWED)
  }, [trackEvent])

  const toggleFaq = (index: number) => {
    setOpenIndex((current) => (current === index ? null : index))
  }

  const handleCtaClick = () => {
    trackEvent(ANALYTICS_EVENTS.FAQ_CTA_CLICKED)
    openSignUp()
  }

  return (
    <div className="mx-auto max-w-3xl px-4 py-16 sm:px-8">
      {/* JSON-LD Schema */}
      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{
          __html: JSON.stringify(generateFaqSchema(FAQ_ITEMS)),
        }}
      />

      <header className="text-center">
        <h1 className="text-4xl font-bold tracking-tight text-foreground sm:text-5xl">
          Frequently Asked Questions
        </h1>
        <p className="mx-auto mt-6 max-w-2xl text-lg text-muted-foreground">
          Got questions? We have answers — about security, billing, household sharing, and handing the busywork to
          your AI agent.
        </p>
      </header>

      <div className="mt-14 space-y-4">
        {FAQ_ITEMS.map((item, index) => {
          const isOpen = openIndex === index
          const answerId = `faq-answer-${index}`
          return (
            <div key={item.question} className="rounded-2xl border border-border bg-card">
              <button
                type="button"
                onClick={() => toggleFaq(index)}
                className="flex w-full items-start justify-between px-6 py-5 text-left"
                aria-expanded={isOpen}
                aria-controls={answerId}
              >
                <h3 className="pr-4 text-lg font-medium text-foreground">{item.question}</h3>
                <ChevronDown
                  className={`mt-0.5 h-5 w-5 flex-shrink-0 text-muted-foreground transition-transform duration-200 ${
                    isOpen ? 'rotate-180' : ''
                  }`}
                  aria-hidden="true"
                />
              </button>
              {/*
                A `grid-rows-[0fr] -> [1fr]` collapse rather than `max-h-0 -> max-h-96`: the
                max-height version clips silently at a fixed cap, and the cancellation answer is
                long enough to exceed it on a narrow viewport — with `overflow-hidden` the reader
                simply loses the end of the sentence, with no scrollbar and no indication. The
                grid row animates to the CONTENT's height, so it cannot truncate at any answer
                length or viewport width. `min-h-0` on the inner element lets the grid track
                shrink to zero.
              */}
              <div
                id={answerId}
                className={`grid overflow-hidden transition-all duration-200 ${
                  isOpen ? 'grid-rows-[1fr] pb-6' : 'grid-rows-[0fr]'
                }`}
              >
                <p className="min-h-0 px-6 leading-relaxed text-muted-foreground">
                  {item.answer}
                  {item.link && (
                    <>
                      {' '}
                      <Link
                        to={item.link.to}
                        className="font-medium text-primary underline underline-offset-2 hover:text-primary/80"
                      >
                        {item.link.label}
                      </Link>
                    </>
                  )}
                </p>
              </div>
            </div>
          )
        })}
      </div>

      {/* Closing CTA — an FAQ page without one is a dead end. */}
      <section
        aria-labelledby="faq-cta-heading"
        className="mt-16 rounded-2xl border border-border bg-card p-8 text-center"
      >
        <h2 id="faq-cta-heading" className="text-2xl font-semibold text-foreground">
          Still have questions?
        </h2>
        <p className="mx-auto mt-3 max-w-xl text-muted-foreground">
          Start with a 60-day free trial. Connect your accounts, automate your budget, and bring your agent along when
          you’re ready. No credit card required.
        </p>
        <button
          type="button"
          onClick={handleCtaClick}
          className="mt-6 inline-flex items-center gap-2 rounded-lg bg-primary px-6 py-3 text-base font-semibold text-primary-foreground transition-opacity hover:opacity-90 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
        >
          Start Free Today
        </button>
        <p className="mt-4 text-sm text-muted-foreground">
          Prefer the details?{' '}
          <Link to="/mcp" className="font-medium text-primary underline underline-offset-2 hover:text-primary/80">
            Explore the MCP server
          </Link>
          .
        </p>
      </section>
    </div>
  )
}