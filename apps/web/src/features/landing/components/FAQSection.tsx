/**
 * FAQSection Component
 *
 * Frequently asked questions with expandable answers and JSON-LD schema.
 * Issue #699: Create Marketing Landing Page for First-Time Visitors
 */

import { useState } from 'react'
import { ChevronDown } from 'lucide-react'

interface FAQItem {
  question: string
  answer: string
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
      'When you cancel, your data is retained for 30 days in case you change your mind. After that, your data is permanently deleted from our servers in compliance with GDPR. You can request immediate deletion at any time.',
  },
  {
    question: 'Is there a free trial?',
    answer:
      'Yes! All new accounts start with a 2-month free trial of our Premium features. No credit card is required to start. You can upgrade, downgrade, or cancel at any time during or after your trial.',
  },
  {
    question: 'Do you sell my financial data?',
    answer:
      'Never. Your financial data is yours and yours alone. We do not sell, share, or monetize your personal financial information. Our business model is simple: you pay for the service, and that is how we make money.',
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
              <div
                id={`faq-answer-${index}`}
                className={`overflow-hidden transition-all duration-200 ${
                  openIndex === index ? 'max-h-96 pb-6' : 'max-h-0'
                }`}
              >
                <p className="px-6 leading-relaxed text-slate-600 dark:text-slate-400">
                  {faq.answer}
                </p>
              </div>
            </div>
          ))}
        </div>
      </div>
    </section>
  )
}
