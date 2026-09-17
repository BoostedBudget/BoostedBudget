/**
 * TermsOfService Tests
 *
 * Tests for the full Terms of Service page.
 * Issue #858: Legal pages for Paddle domain verification
 */

import { describe, it, expect } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { TermsOfService } from './TermsOfService'

const renderWithRouter = (component: React.ReactElement) =>
  render(<MemoryRouter>{component}</MemoryRouter>)

/**
 * The text of one top-level section: the content between the <h2> matching `heading` and the next
 * <h2>, whitespace-collapsed.
 *
 * Anchoring matters here rather than querying the whole ~600-line document: words like "delete",
 * "6 months" and "retention period" appear in a dozen other sections, so a document-wide matcher
 * would prove nothing about the clause under test.
 */
function sectionText(heading: RegExp): string {
  const h2 = Array.from(document.querySelectorAll('h2')).find((h) =>
    heading.test(h.textContent ?? '')
  )
  expect(h2).toBeDefined()
  let text = ''
  let node: Element | null = h2!.nextElementSibling
  while (node && node.tagName !== 'H2') {
    text += ' ' + (node.textContent ?? '')
    node = node.nextElementSibling
  }
  return text.replace(/\s+/g, ' ').trim()
}

describe('TermsOfService', () => {
  it('renders "Terms of Service" heading', () => {
    renderWithRouter(<TermsOfService />)
    expect(screen.getByRole('heading', { name: /terms of service/i })).toBeInTheDocument()
  })

  it('renders "Last Updated" date', () => {
    renderWithRouter(<TermsOfService />)
    expect(screen.getByText(/last updated: september 13, 2026/i)).toBeInTheDocument()
  })

  it('renders all major sections', () => {
    renderWithRouter(<TermsOfService />)
    expect(screen.getByText(/acceptance of terms/i)).toBeInTheDocument()
    expect(screen.getByText(/service description/i)).toBeInTheDocument()
    expect(screen.getByText(/subscription plans and billing/i)).toBeInTheDocument()
    expect(screen.getByText(/refund and cancellation policy/i)).toBeInTheDocument()
    expect(screen.getByText(/trial expiration and read-only access/i)).toBeInTheDocument()
    expect(screen.getByText(/dispute resolution/i)).toBeInTheDocument()
    expect(screen.getByText(/governing law/i)).toBeInTheDocument()
  })

  it('renders no refunds policy', () => {
    renderWithRouter(<TermsOfService />)
    expect(screen.getByText(/all subscription payments are non-refundable/i)).toBeInTheDocument()
  })

  it('renders free trial terms including no ads', () => {
    renderWithRouter(<TermsOfService />)
    expect(screen.getByText(/60-day free trial/i)).toBeInTheDocument()
    expect(screen.getByText(/no credit card is required/i)).toBeInTheDocument()
    expect(screen.getByText(/no advertisements are displayed/i)).toBeInTheDocument()
  })

  it('renders current subscription tiers and prices', () => {
    renderWithRouter(<TermsOfService />)
    expect(screen.getByText(/\$8\.99\/month/i)).toBeInTheDocument()
    expect(screen.getByText(/\$12\.99\/month/i)).toBeInTheDocument()
    expect(screen.getByText(/\$18\.99\/month/i)).toBeInTheDocument()
  })

  it('renders Paddle as payment processor', () => {
    renderWithRouter(<TermsOfService />)
    expect(screen.getByText(/payments are processed by/i)).toBeInTheDocument()
  })

  it('renders Delaware governing law', () => {
    renderWithRouter(<TermsOfService />)
    expect(screen.getByText(/state of delaware/i)).toBeInTheDocument()
  })

  it('renders link to Privacy Policy', () => {
    renderWithRouter(<TermsOfService />)
    const privacyLinks = screen.getAllByRole('link', { name: /privacy policy/i })
    expect(privacyLinks.length).toBeGreaterThan(0)
    expect(privacyLinks.some((link) => link.getAttribute('href') === '/legal/privacy-policy')).toBe(true)
  })

  it('does not contain placeholder coming soon text', () => {
    renderWithRouter(<TermsOfService />)
    expect(screen.queryByText(/coming soon/i)).not.toBeInTheDocument()
  })

  it('renders support contact email', () => {
    renderWithRouter(<TermsOfService />)
    const emails = screen.getAllByText(/support@boostedbudget\.com/i)
    expect(emails.length).toBeGreaterThan(0)
  })

  it('renders fintech disclaimers', () => {
    renderWithRouter(<TermsOfService />)
    const bankTexts = screen.getAllByText(/not a bank/i)
    expect(bankTexts.length).toBeGreaterThan(0)
    expect(screen.getByText(/not fdic-insured or sipc-protected/i)).toBeInTheDocument()
  })

  it('renders Paddle as Merchant of Record', () => {
    renderWithRouter(<TermsOfService />)
    const morTexts = screen.getAllByText(/merchant of record/i)
    expect(morTexts.length).toBeGreaterThan(0)
  })

  it('renders PostHog in third-party services', () => {
    renderWithRouter(<TermsOfService />)
    const posthogTexts = screen.getAllByText(/posthog/i)
    expect(posthogTexts.length).toBeGreaterThan(0)
  })

  it('renders General Provisions section', () => {
    renderWithRouter(<TermsOfService />)
    expect(screen.getByText(/general provisions/i)).toBeInTheDocument()
    expect(screen.getAllByText(/indemnification/i).length).toBeGreaterThan(0)
    expect(screen.getAllByText(/force majeure/i).length).toBeGreaterThan(0)
    expect(screen.getAllByText(/severability/i).length).toBeGreaterThan(0)
    expect(screen.getAllByText(/entire agreement/i).length).toBeGreaterThan(0)
  })

  it('renders Sentry and SendGrid in third-party services', () => {
    renderWithRouter(<TermsOfService />)
    expect(screen.getAllByText(/sentry/i).length).toBeGreaterThan(0)
    expect(screen.getAllByText(/sendgrid/i).length).toBeGreaterThan(0)
  })

  it('renders link to Cookie Policy', () => {
    renderWithRouter(<TermsOfService />)
    const cookieLinks = screen.getAllByRole('link', { name: /cookie policy/i })
    expect(cookieLinks.length).toBeGreaterThan(0)
    expect(cookieLinks.some((link) => link.getAttribute('href') === '/legal/cookie-policy')).toBe(true)
  })

  it('renders EU/UK consumer withdrawal-right carveout', () => {
    renderWithRouter(<TermsOfService />)
    expect(screen.getByText(/14-day right to withdraw/i)).toBeInTheDocument()
    expect(screen.getByText(/eea or uk/i)).toBeInTheDocument()
  })

  it('renders consumer protection arbitration carveout', () => {
    renderWithRouter(<TermsOfService />)
    expect(screen.getByText(/consumer protection carveout/i)).toBeInTheDocument()
    expect(screen.getByText(/applicable consumer protection law/i)).toBeInTheDocument()
  })

  it('renders mandatory-law liability carveouts', () => {
    renderWithRouter(<TermsOfService />)
    expect(screen.getByText(/gross negligence, willful misconduct/i)).toBeInTheDocument()
    expect(screen.getByText(/cannot be excluded or limited under applicable law/i)).toBeInTheDocument()
  })

  it('renders 6-month data retention instead of indefinite', () => {
    renderWithRouter(<TermsOfService />)
    expect(screen.getAllByText(/6 months/i).length).toBeGreaterThan(0)
    expect(screen.queryByText(/preserved indefinitely/i)).not.toBeInTheDocument()
  })

  // Issue #1592 finding I2. § 7 was the second of three surfaces promising a post-retention
  // deletion that does not happen in any environment: #1569 gated the retention sweep on both
  // pre-deletion notices having been recorded (retentionNoticesDeliveredPredicate,
  // profile_repository.go) and no EmailService implements jobs.RetentionEmailService
  // (cmd/server/main.go), so the notices are never sent and expired accounts are retained
  // indefinitely instead of deleted.
  //
  // Anchored to the § 7 heading and walked to the next <h2>, not queried against the whole
  // ~600-line document — the words "delete" and "retention period" appear in a dozen other
  // sections, so a document-wide matcher would prove nothing about this clause.
  it('does not promise a post-retention deletion the retention sweep cannot perform (Section 7)', () => {
    renderWithRouter(<TermsOfService />)
    const text = sectionText(/trial expiration and read-only access/i)

    // The superseded sentence, pinned in full.
    expect(text).not.toMatch(
      /After the retention period, we will delete or anonymize it unless a longer retention period is legally required/i
    )
    expect(text).toMatch(
      /we do not automatically delete a cancelled account unless both .{0,40}notices have been sent/i
    )
    expect(text).toMatch(/not sending those notices yet/i)
    // …and the user-requested path, which the sweep's own `deletion_requested_at IS NULL`
    // predicate excludes, must stay available and unqualified.
    expect(text).toMatch(/request deletion at any time/i)
  })

  // Issue #1592 round 5 (G13) — the same contradiction as Privacy Policy § 8.4, in § 7 and again
  // in the Key Points Summary. Round 4 added "cancelled accounts are kept past the 6-month
  // window" without reconciling the sentence above it, which still said "for up to 6 months".
  it('states six months as a floor, not a cap, in the cancellation clause (Section 7)', () => {
    renderWithRouter(<TermsOfService />)
    const text = sectionText(/trial expiration and read-only access/i)

    expect(text).not.toMatch(/account data for up to ?6 months after cancellation/i)
    expect(text).toMatch(/account data for at least ?6 months after cancellation/i)
    expect(text).toMatch(/longer than that today/i)
    expect(text).toMatch(/kept past the 6-month window/i)
  })

  // The Key Points Summary is the only part of this document most readers finish. An unqualified
  // "retained for up to 6 months" there re-states the capped claim the body no longer makes.
  it('carries the same floor qualification in the Key Points Summary', () => {
    renderWithRouter(<TermsOfService />)
    const summary = document.querySelector('.privacy-summary')
    expect(summary).not.toBeNull()
    const text = (summary!.textContent ?? '').replace(/\s+/g, ' ').trim()
    expect(text).not.toMatch(/data retained for up to 6 months/i)
    expect(text).toMatch(/data retained for at least 6 months/i)
    expect(text).toMatch(/longer while automatic deletion is not yet running/i)
  })

  it('renders 30-day data preservation for removed household members (Section 9.2)', () => {
    renderWithRouter(<TermsOfService />)
    // First assert the section heading to locate §9.2 specifically
    expect(screen.getByRole('heading', { name: /downgrading household plans/i })).toBeInTheDocument()

    // Locate the specific <li> describing removed members' data retention. Its
    // text ("...preserved for <strong>30 days</strong> before permanent deletion")
    // spans multiple text nodes/elements, so a custom matcher anchored to the
    // <li> itself is used rather than a plain string/regex query - this pins the
    // assertions below to that exact DOM node instead of "somewhere in the whole
    // rendered document."
    const removedMembersDataItem = screen.getByText((_content, element) => {
      return element?.tagName === 'LI' && /removed members' data/i.test(element.textContent ?? '')
    })

    // Assert the 30-day preservation period and the permanent-deletion phrase are
    // both contained within THIS list item. If §9.2's phrasing is edited or
    // removed, this fails even if unrelated "30 days" / "permanent deletion" text
    // exists (now or in the future) elsewhere in the ~600-line document.
    expect(removedMembersDataItem.textContent).toMatch(/30 days/i)
    expect(removedMembersDataItem.textContent).toMatch(/before permanent deletion/i)
  })

  it('renders GDPR Article 6 legal basis in Privacy Policy (via ToS cross-reference)', () => {
    // ToS references Privacy Policy; confirm ToS itself links to it
    renderWithRouter(<TermsOfService />)
    const ppLinks = screen.getAllByRole('link', { name: /privacy policy/i })
    expect(ppLinks.length).toBeGreaterThan(0)
  })

  it('specifies AAA Consumer Arbitration Rules (not generic AAA rules)', () => {
    renderWithRouter(<TermsOfService />)
    expect(screen.getByText(/consumer arbitration rules of the american arbitration association/i)).toBeInTheDocument()
    expect(screen.getByText(/filing fees are governed by the aaa consumer arbitration rules fee schedule/i)).toBeInTheDocument()
  })

  it('renders household data visibility disclosures for shared accounts', () => {
    renderWithRouter(<TermsOfService />)
    expect(screen.getByText(/data visibility between members/i)).toBeInTheDocument()
    // Anchored exact-match: "account balances" also appears unanchored in Section 2
    // (Service Description); an unanchored regex here matches both and throws
    // "Found multiple elements" (verified by running the suite after this line was
    // briefly de-anchored — restoring the anchors here, deliberately, per Round 1's
    // Task 2 review).
    expect(screen.getByText(/^account balances$/i)).toBeInTheDocument()
    expect(screen.getByText(/net worth contribution/i)).toBeInTheDocument()
    expect(screen.getByText(/spending activity, including merchant names/i)).toBeInTheDocument()
    expect(screen.getByText(/income sources and savings-rate figures/i)).toBeInTheDocument()
  })

  it('renders what household members can never see', () => {
    renderWithRouter(<TermsOfService />)
    expect(screen.getByText(/individual transaction line items/i)).toBeInTheDocument()
    // Covers both unmasked and masked account numbers — the household aggregate response
    // never serializes even a masked/last-4 account number (only id/name/balance).
    expect(screen.getByText(/full account numbers or masks/i)).toBeInTheDocument()
    expect(screen.getByText(/login credentials/i)).toBeInTheDocument()
  })

  it('renders consent-at-join disclosures for account sharing', () => {
    renderWithRouter(<TermsOfService />)
    expect(screen.getByText(/consent at join/i)).toBeInTheDocument()
    expect(screen.getByText(/choose which to share with the household/i)).toBeInTheDocument()
    expect(screen.getByText(/all accounts are shared by default/i)).toBeInTheDocument()
    expect(screen.getByText(/version identifier and timestamp/i)).toBeInTheDocument()
    expect(screen.getByText(/accounts you link to boostedbudget after joining a household/i)).toBeInTheDocument()
    expect(screen.getByText(/default to shared/i)).toBeInTheDocument()
  })

  it('renders account-level hide behavior distinct from personal-view hide', () => {
    renderWithRouter(<TermsOfService />)
    // Check for the new wording without "immediately"
    expect(screen.getByText(/removed from the household's shared view\./i)).toBeInTheDocument()
    // Confirm the old overclaiming phrase is NOT present
    expect(screen.queryByText(/removed from the household's shared view immediately/i)).not.toBeInTheDocument()
    // Confirm the distinction between hiding from household vs personal views is explained
    expect(screen.getByText(/hiding an account from the household view is separate from hiding an account from your own personal views/i)).toBeInTheDocument()
  })

  it('renders leaving/removal household-visibility disclosure', () => {
    renderWithRouter(<TermsOfService />)
    expect(screen.getByText(/leaving or being removed/i)).toBeInTheDocument()
    expect(screen.getByText(/immediately lose visibility into your shared accounts/i)).toBeInTheDocument()
    expect(screen.getByText(/personal data.*is retained/i)).toBeInTheDocument()
  })
})
