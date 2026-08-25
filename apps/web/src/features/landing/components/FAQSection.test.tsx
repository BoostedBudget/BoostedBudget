/**
 * FAQSection Tests
 *
 * Issue #1569 / #1592 (finding I2): the "what happens to my data if I cancel?" answer is one of
 * three surfaces that promised an automatic post-cancellation deletion the app does not perform.
 * It was also wrong about the window (30 days, not 6 months) and conflated cancellation with a
 * user-requested deletion, which are two different flows with two different outcomes.
 */

import { describe, it, expect } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { FAQSection } from './FAQSection'

/**
 * The rendered answer text for one question. Anchored to the question's own disclosure block
 * (`#faq-answer-N`) rather than the whole document, so an unrelated answer — or the JSON-LD
 * script, which repeats every answer verbatim — cannot satisfy an assertion about this one.
 */
function answerFor(question: RegExp): string {
  const heading = screen
    .getAllByRole('heading', { level: 3 })
    .find((h) => question.test(h.textContent ?? ''))
  if (!heading) throw new Error(`no FAQ question matching ${question}`)
  const button = heading.closest('button')
  const answerId = button?.getAttribute('aria-controls')
  if (!answerId) throw new Error(`FAQ question ${question} has no aria-controls`)
  const answer = document.getElementById(answerId)
  if (!answer) throw new Error(`no answer region #${answerId}`)
  return (answer.textContent ?? '').replace(/\s+/g, ' ').trim()
}

describe('FAQSection', () => {
  it('renders the heading and every question', () => {
    render(<FAQSection />)
    expect(screen.getByRole('heading', { name: /frequently asked questions/i })).toBeInTheDocument()
    expect(screen.getByText(/what happens to my data if i cancel/i)).toBeInTheDocument()
  })

  // RED for #1592 finding I2. The old answer said: "your data is retained for 30 days ... After
  // that, your data is permanently deleted from our servers in compliance with GDPR."
  //
  // Both halves are false. The cancellation window is 6 months, not 30 days (30 days is the
  // grace period on a deletion the user REQUESTS), and no automatic deletion runs at all: the
  // retention sweep requires both the 30-day and 7-day pre-deletion notices to have been
  // recorded (retentionNoticesDeliveredPredicate, profile_repository.go), no EmailService
  // implements jobs.RetentionEmailService in any environment, so expired accounts are retained
  // indefinitely instead. See PrivacyPolicy § 8.4, which this answer must agree with.
  it('does not promise an automatic post-cancellation deletion the app does not perform', () => {
    render(<FAQSection />)
    const answer = answerFor(/what happens to my data if i cancel/i)

    // The specific false sentence, pinned in full rather than by a short substring — a
    // `toMatch(/30 days/)` here would be satisfied by the (true) deletion-request grace period.
    expect(answer).not.toMatch(
      /retained for 30 days in case you change your mind\. After that, your data is permanently deleted/i
    )
    // Cancellation alone leaves the account read-only for the 6-month window.
    expect(answer).toMatch(/read-only/i)
    expect(answer).toMatch(/6 months/i)
    // …and the honest statement of what is not happening today.
    expect(answer).toMatch(/we are not automatically deleting cancelled accounts/i)
  })

  // The user-requested path is unaffected by the notice gate — deletion_requested_at IS NULL is
  // in the retention sweep's own WHERE clause, so a requested deletion never goes near it. The
  // answer must keep saying so, and must keep the 30-day grace attached to THAT flow.
  it('keeps the user-requested deletion promise, with its 30-day grace period, distinct from cancelling', () => {
    render(<FAQSection />)
    const answer = answerFor(/what happens to my data if i cancel/i)
    expect(answer).toMatch(/request deletion at any time/i)
    expect(answer).toMatch(/30-day grace period/i)
  })

  // The answers grew with the fix above, and the disclosure used to collapse with
  // `max-h-0 -> max-h-96` under `overflow-hidden`. 24rem is a fixed cap: an answer taller than
  // that is CLIPPED with no scrollbar and no indication, and the cancellation answer exceeds it
  // on a narrow viewport. jsdom computes no layout, so this cannot assert a rendered height —
  // what it can pin is that the mechanism is not a fixed cap.
  describe('the answer disclosure cannot truncate a long answer', () => {
    function answerRegion(question: RegExp): HTMLElement {
      const heading = screen
        .getAllByRole('heading', { level: 3 })
        .find((h) => question.test(h.textContent ?? ''))!
      const id = heading.closest('button')!.getAttribute('aria-controls')!
      return document.getElementById(id)!
    }

    it('collapses by grid row rather than a fixed max-height cap', () => {
      render(<FAQSection />)
      const region = answerRegion(/what happens to my data if i cancel/i)
      expect(region.className).toContain('grid-rows-[0fr]')
      expect(region.className).not.toMatch(/max-h-\d/)

      fireEvent.click(screen.getByText(/what happens to my data if i cancel/i))
      expect(answerRegion(/what happens to my data if i cancel/i).className).toContain(
        'grid-rows-[1fr]'
      )
      expect(answerRegion(/what happens to my data if i cancel/i).className).not.toMatch(/max-h-\d/)
    })

    it('reports its expanded state to assistive technology', () => {
      render(<FAQSection />)
      const button = screen.getByText(/what happens to my data if i cancel/i).closest('button')!
      expect(button).toHaveAttribute('aria-expanded', 'false')
      fireEvent.click(button)
      expect(button).toHaveAttribute('aria-expanded', 'true')
    })
  })
})
