/**
 * FAQSection Tests
 *
 * Issue #1569 / #1592 (finding I2): the "what happens to my data if I cancel?" answer is one of
 * three surfaces that promised an automatic post-cancellation deletion the app does not perform.
 * It was also wrong about the window (30 days, not 6 months) and conflated cancellation with a
 * user-requested deletion, which are two different flows with two different outcomes.
 */

import { describe, it, expect } from 'bun:test'
import { render, screen, fireEvent, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { FAQSection } from './FAQSection'

/** The component renders internal `/mcp` links, so it needs a router ancestor. */
function renderFaq() {
  return render(
    <MemoryRouter>
      <FAQSection />
    </MemoryRouter>
  )
}

/** Escape a literal for use inside a RegExp (questions contain `?`). */
function literalRe(value: string): RegExp {
  return new RegExp(value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'), 'i')
}

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
    renderFaq()
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
    renderFaq()
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
  // Issue #1592 round 5 (G13). The FAQ said "we keep it that way for up to 6 months" and then
  // "we are not automatically deleting cancelled accounts today" — the second sentence tells the
  // reader nothing is deleted, but the first still caps the window at six months, so a reader
  // finishing the answer cannot tell what happens in month seven. The legal pages resolve this by
  // calling six months a floor; the FAQ, which is the surface most people actually read, omitted
  // the qualification entirely.
  //
  // The negative is pinned as well as the positive: dropping "6 months" altogether would satisfy
  // a bare "says it is kept past the window" assertion while removing the commitment.
  it('states six months as a floor and says the data is kept past that window', () => {
    renderFaq()
    const answer = answerFor(/what happens to my data if i cancel/i)
    expect(answer).not.toMatch(/for up to 6 months/i)
    expect(answer).toMatch(/for at least 6 months/i)
    expect(answer).toMatch(/kept past that 6-month window/i)
  })

  it('keeps the user-requested deletion promise, with its 30-day grace period, distinct from cancelling', () => {
    renderFaq()
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
      renderFaq()
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
      renderFaq()
      const button = screen.getByText(/what happens to my data if i cancel/i).closest('button')!
      expect(button).toHaveAttribute('aria-expanded', 'false')
      fireEvent.click(button)
      expect(button).toHaveAttribute('aria-expanded', 'true')
    })
  })

  // Issue #1666 item 8. The landing FAQ gains the same three agentic Q&As the
  // /faq page carries, so the two surfaces tell one story until Todo 14 unifies
  // the prose into a single module.
  describe('agentic FAQ entries', () => {
    const AGENTIC_QUESTIONS = [
      'Can my AI agent help me manage my budget?',
      'Can an AI agent move my money?',
      'Is it safe to let an AI agent see my finances?',
    ]

    it('renders all three agentic questions', () => {
      renderFaq()
      for (const question of AGENTIC_QUESTIONS) {
        expect(screen.getByRole('heading', { level: 3, name: question })).toBeInTheDocument()
      }
    })

    it('links each agentic answer to /mcp as a real link', () => {
      renderFaq()
      for (const question of AGENTIC_QUESTIONS) {
        const heading = screen.getByRole('heading', { level: 3, name: question })
        const answerId = heading.closest('button')!.getAttribute('aria-controls')!
        const region = document.getElementById(answerId)!
        const link = within(region).getByRole('link', { name: /mcp server/i })
        expect(link).toHaveAttribute('href', '/mcp')
      }
    })

    // Claims discipline: the four promises every agentic answer must keep.
    it('states confirmation, no money movement, server-side credentials, and revocability', () => {
      renderFaq()
      const help = answerFor(/can my ai agent help me manage my budget/i)
      expect(help).toMatch(/waits for your explicit confirmation/i)

      const money = answerFor(/can an ai agent move my money/i)
      expect(money).toMatch(/never moves money/i)

      const safe = answerFor(/is it safe to let an ai agent see my finances/i)
      expect(safe).toMatch(/injected server-side/i)
      expect(safe).toMatch(/never visible to the agent/i)
      expect(safe).toMatch(/revoke the agent/i)
    })

    // No agentic answer may promise an action the product cannot perform.
    it('promises no cancellation, transfer, or invite action', () => {
      renderFaq()
      for (const question of AGENTIC_QUESTIONS) {
        const answer = answerFor(literalRe(question))
        expect(answer).not.toMatch(/will cancel/i)
        expect(answer).not.toMatch(/transfers your money/i)
        expect(answer).not.toMatch(/accepts the invite/i)
      }
    })

    it('includes all three agentic questions in the FAQPage JSON-LD', () => {
      renderFaq()
      const script = document.querySelector('script[type="application/ld+json"]')!
      const schema = JSON.parse(script.textContent ?? '{}') as {
        mainEntity: Array<{ name: string; acceptedAnswer: { text: string } }>
      }
      const names = schema.mainEntity.map((entry) => entry.name)
      for (const question of AGENTIC_QUESTIONS) {
        expect(names).toContain(question)
      }
      expect(schema.mainEntity).toHaveLength(10)
    })
  })
})
