/**
 * FAQSection Component
 *
 * Frequently asked questions with expandable answers and JSON-LD schema.
 * Issue #699: Create Marketing Landing Page for First-Time Visitors
 */

import { useState } from 'react'
import { Link } from 'react-router'
import { ChevronDown } from 'lucide-react'
import { MARKETING_FAQ, type FaqItem } from '../data/marketingContent'

// Generate JSON-LD schema for FAQPage
function generateFAQSchema(items: FaqItem[]) {
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
          __html: JSON.stringify(generateFAQSchema(MARKETING_FAQ)),
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
          {MARKETING_FAQ.map((faq, index) => (
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
