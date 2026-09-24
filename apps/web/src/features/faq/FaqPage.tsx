/**
 * FaqPage — the public /faq page (Issue #1666).
 *
 * A real routed page, not a landing anchor: App.tsx computes
 * `isLandingPage = pathname === '/' && !isAuthenticated`, so a signed-in user is
 * redirected from `/` to /dashboard and the landing's `#faq` hash dies before an
 * answer is ever opened. This page is the destination the footer links to
 * instead — reachable signed out and signed in.
 *
 * Content is the ten public Q&As — the landing page's seven plus three agentic
 * Q&As about the remote MCP server at /mcp — sourced from the single shared
 * `features/landing/data/marketingContent.ts` (Todo 14), which the landing FAQ,
 * this page, and the generated `llms-full.txt` all consume. The data is a plain
 * `{ question, answer, link? }` array with no JSX; the disclosure markup is
 * rendered from it, not embedded in it.
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
import { MARKETING_FAQ, type FaqItem } from '@/features/landing/data/marketingContent'

const PAGE_TITLE = 'FAQ — BoostedBudget'
const PAGE_DESCRIPTION =
  'Answers about security, billing, household sharing, and handing the busywork to your AI agent. BoostedBudget never moves money.'
const CANONICAL_URL = 'https://boostedbudget.com/faq'

// Generate JSON-LD schema for FAQPage. Kept in lockstep with the rendered
// questions by sourcing both from MARKETING_FAQ.
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
          __html: JSON.stringify(generateFaqSchema(MARKETING_FAQ)),
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
        {MARKETING_FAQ.map((item, index) => {
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