/**
 * FAQSection Component
 *
 * Frequently asked questions with expandable answers and JSON-LD schema.
 * Issue #699: Create Marketing Landing Page for First-Time Visitors
 */

import { useState } from 'react'
import { Link } from 'react-router'
import { ChevronDown } from 'lucide-react'

interface FAQItem {
  question: string
  answer: string
  /** Optional inline link appended to the answer. The JSON-LD text stays the plain answer. */
  link?: { label: string; to: string }
}

const faqs: FAQItem[] = [
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

// Generate JSON-LD schema for FAQPage
function generateFAQSchema(items: FAQItem[]) {
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

export function FAQSection() {
  const [openIndex, setOpenIndex] = useState<number | null>(null)

  const toggleFAQ = (index: number) => {
    setOpenIndex(openIndex === index ? null : index)
  }

  return (
    <section
      className="faq-section bg-white py-24 dark:bg-slate-900 sm:py-32"
      aria-labelledby="faq-heading"
    >
      {/* JSON-LD Schema */}
      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{
          __html: JSON.stringify(generateFAQSchema(faqs)),
        }}
      />

      <div className="mx-auto max-w-3xl px-4 sm:px-6 lg:px-8">
        {/* Section Header */}
        <div className="text-center">
          <h2
            id="faq-heading"
            className="text-3xl font-bold tracking-tight text-slate-900 dark:text-white sm:text-4xl lg:text-5xl"
          >
            Frequently Asked Questions
          </h2>
          <p className="mx-auto mt-6 max-w-2xl text-lg leading-relaxed text-slate-600 dark:text-slate-400">
            Got questions? We have answers.
          </p>
        </div>

        {/* FAQ Items */}
        <div className="mt-16 space-y-4">
          {faqs.map((faq, index) => (
            <div
              key={index}
              className="rounded-2xl bg-slate-50 transition-colors dark:bg-slate-800/50"
            >
              <button
                onClick={() => toggleFAQ(index)}
                className="flex w-full items-start justify-between px-6 py-5 text-left"
                aria-expanded={openIndex === index}
                aria-controls={`faq-answer-${index}`}
              >
                <h3 className="pr-4 text-lg font-medium text-slate-900 dark:text-white">
                  {faq.question}
                </h3>
                <ChevronDown
                  className={`mt-0.5 h-5 w-5 flex-shrink-0 text-slate-500 transition-transform duration-200 ${
                    openIndex === index ? 'rotate-180' : ''
                  }`}
                  aria-hidden="true"
                />
              </button>
              {/*
                A `grid-rows-[0fr] -> [1fr]` collapse rather than `max-h-0 -> max-h-96`: the
                max-height version clips silently at 24rem, and the cancellation answer is long
                enough to exceed that on a narrow viewport — with `overflow-hidden` the reader
                simply loses the end of the sentence, with no scrollbar and no indication. The
                grid row animates to the CONTENT's height, so it cannot truncate at any answer
                length or viewport width. `min-h-0` on the inner element is what lets the grid
                track shrink to zero.
              */}
              <div
                id={`faq-answer-${index}`}
                className={`grid overflow-hidden transition-all duration-200 ${
                  openIndex === index ? 'grid-rows-[1fr] pb-6' : 'grid-rows-[0fr]'
                }`}
              >
                <p className="min-h-0 px-6 leading-relaxed text-slate-600 dark:text-slate-400">
                  {faq.answer}
                  {faq.link && (
                    <>
                      {' '}
                      <Link
                        to={faq.link.to}
                        className="font-medium text-primary underline underline-offset-2 hover:text-primary/80"
                      >
                        {faq.link.label}
                      </Link>
                    </>
                  )}
                </p>
              </div>
            </div>
          ))}
        </div>
      </div>
    </section>
  )
}
