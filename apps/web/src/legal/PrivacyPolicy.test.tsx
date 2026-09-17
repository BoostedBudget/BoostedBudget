/**
 * PrivacyPolicy Tests
 *
 * Tests for the Privacy Policy page.
 * Issue #858: Legal pages for Paddle domain verification
 */

import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, it, expect } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { PrivacyPolicy } from './PrivacyPolicy'

const renderWithRouter = (component: React.ReactElement) =>
  render(<MemoryRouter>{component}</MemoryRouter>)

/**
 * The rendered text of one numbered subsection: the `<h3>` matching `heading`, plus every
 * sibling element up to the next `<h3>`.
 *
 * ⚠ This exists because the policy is a ~600-line single document. A bare
 * `screen.getByText(/…/)` on it is satisfied by ANY paragraph — including the one you are
 * trying to prove was changed, in a different section. Anchoring to the subsection is what makes
 * "§ 8.4 says X" a real assertion rather than "the page contains X somewhere".
 */
function subsectionText(heading: RegExp): string {
  const h3 = Array.from(document.querySelectorAll('h3')).find((h) =>
    heading.test(h.textContent ?? '')
  )
  if (!h3) throw new Error(`no <h3> matching ${heading}`)
  let text = ''
  let node: Element | null = h3.nextElementSibling
  while (node && node.tagName !== 'H3') {
    text += ' ' + (node.textContent ?? '')
    node = node.nextElementSibling
  }
  return text.replace(/\s+/g, ' ').trim()
}

/**
 * The path to the reviewed golden the Go side pins the thirteen deletion phrases in.
 *
 * ⚠ This file is a GIT-TRACKED SOURCE, not a build product, and it is not written by anything
 * that runs in this suite. See backend/internal/queue/jobs/retention_notice_golden_test.go.
 */
const RETENTION_NOTICE_GOLDEN = join(
  import.meta.dir,
  '../../../../backend/internal/queue/jobs/testdata/retention_notice.golden'
)

/**
 * The thirteen items `retentionDeletionItems`
 * (backend/internal/queue/jobs/retention_cleanup_check.go) enumerates in the 30-day and 7-day
 * pre-deletion notice emails, READ OUT OF THE GO-SIDE GOLDEN rather than re-declared here.
 *
 * ⚠ WHY IT READS A FILE INSTEAD OF HOLDING A LITERAL (#1581/#1592, round 5). This array used to be
 * a hand-copied duplicate of the Go slice. That copy made the Go golden fail on a Go change and
 * made NOTHING fail on the TS side, so the two drifted the moment the golden was promoted — the
 * exact "two copies, one truth, nothing watching" shape the comment here used to describe as
 * unavoidable ("There is no mechanical link across the Go/TS boundary"). There is one, and this is
 * it: the golden is checked in, is regenerated only by a reviewed `mv`, and already carries every
 * phrase verbatim. Change a phrase in Go, promote the golden, and THIS test fails until
 * PrivacyPolicy.tsx § 8.4 is edited too. That round-trip was executed before this was committed.
 *
 * ⚠ IT IS A LINK, NOT A SECOND RENDERER. Nothing here reproduces the Go notice format: it takes
 * the `phrase: ` lines out of the golden's item→tables mapping and stops. That mapping's exact
 * `tables: …\nphrase: …` shape is itself pinned Go-side by
 * TestReviewedGoldenActuallyPinsWhatItClaims, so the format this parse depends on cannot be
 * dropped from the golden without the Go suite failing first.
 */
function retentionDeletionPhrases(): string[] {
  const golden = readFileSync(RETENTION_NOTICE_GOLDEN, 'utf-8')

  // Only the mapping section. Every phrase ALSO appears as a bullet in the two notice bodies
  // below the `### BEGIN` markers, and parsing those would double the list for no extra pinning.
  const mapping = golden.split('### BEGIN')[0]
  const phrases = Array.from(mapping.matchAll(/^phrase: (.+)$/gm)).map((m) => m[1].trim())

  // ⚠ A parse that silently yields [] would turn the § 8.4 enumeration assertion into a vacuous
  // for-loop over nothing — the failure mode this whole change exists to prevent, reintroduced one
  // level up. The count is asserted, not just non-emptiness: dropping an item from the Go slice
  // must fail HERE rather than quietly shrinking what § 8.4 is checked against.
  expect(phrases.length).toBe(13)
  for (const phrase of phrases) {
    expect(phrase.length).toBeGreaterThan(20)
  }
  return phrases
}

describe('PrivacyPolicy', () => {
  it('renders "Privacy Policy" heading', () => {
    renderWithRouter(<PrivacyPolicy />)
    // Use h1 specifically — h2 "12. Changes to This Privacy Policy" also matches /privacy policy/i
    const h1 = document.querySelector('h1')
    expect(h1?.textContent).toMatch(/privacy policy/i)
  })

  it('renders GDPR Article 6 legal basis table', () => {
    renderWithRouter(<PrivacyPolicy />)
    expect(screen.getByText(/contract performance/i)).toBeInTheDocument()
    expect(screen.getByText(/legitimate interests/i)).toBeInTheDocument()
    // "legal obligation" appears in both Section 3 compliance bullet and Section 3.1 GDPR table
    expect(screen.getAllByText(/legal obligation/i).length).toBeGreaterThan(0)
  })

  it('renders 6-month passive retention for cancelled accounts (Section 8.4)', () => {
    renderWithRouter(<PrivacyPolicy />)
    // Anchored to the HEADING, not to body text: § 8.4 now discusses cancelled accounts in its
    // prose too, and a bare getByText(/cancelled accounts/i) would throw on multiple matches.
    expect(screen.getByRole('heading', { name: /cancelled accounts/i })).toBeInTheDocument()
    expect(screen.getAllByText(/6 months/i).length).toBeGreaterThan(0)
  })

  // Issue #1592 (split out of #1569 T9). The clause this replaces named THREE categories —
  // "your automation rules, transaction history, and balance history" — of the thirteen the
  // pre-deletion emails enumerate and the 53 tables DeleteUserCascade actually destroys. § 8.4
  // is the clause the ADR's Decision 3 argument leans on (the retention sweep is the one
  // automatic destroyer the ADR permits, and it is permitted BECAUSE the user was told what it
  // destroys), so an under-stated § 8.4 undercuts the permission itself.
  it('enumerates, item by item, everything the post-retention deletion destroys (Section 8.4)', () => {
    renderWithRouter(<PrivacyPolicy />)
    const text = subsectionText(/8\.4/)
    for (const item of retentionDeletionPhrases()) {
      expect(text).toContain(item)
    }
  })

  // Issue #1592, half 2. The emails' first draft over-claimed erasure and was corrected;
  // § 8.4 must carry the SAME qualification or widening the enumeration just relocates the
  // over-claim. audit_logs.user_id is ON DELETE SET NULL — every non-residue audit row survives
  // deletion, keeping ip_address, user_agent, resource_id and created_at. Under the
  // erasure-PROMISE reading a regulator applies to a deletion notice, over-claiming is the
  // unsafe direction.
  it('qualifies the deletion promise rather than claiming unconditional erasure (Section 8.4)', () => {
    renderWithRouter(<PrivacyPolicy />)
    const text = subsectionText(/8\.4/)
    expect(text).toContain('except where the law requires us to keep a record')
    // The superseded three-category clause, pinned in full. A short substring here would be
    // satisfied by the (still present, still true) words "transaction" or "balance history".
    expect(text).not.toMatch(
      /including your automation rules, transaction history, and balance history/i
    )
  })

  // Issue #1592 finding I2 — the OPPOSITE error, in the same paragraph. § 8.4 promised that
  // "after the retention period, we will automatically and permanently delete your data". That
  // deletion does not happen in ANY environment: #1569 gated the sweep on both pre-deletion
  // notices having been recorded (retentionNoticesDeliveredPredicate, profile_repository.go),
  // and no EmailService implements jobs.RetentionEmailService (cmd/server/main.go), so the
  // notices are never sent, the markers are never stamped, and expired accounts are retained
  // indefinitely. Retaining is the safe direction to fail; promising the deletion anyway is not.
  it('does not promise an automatic deletion the retention sweep cannot perform (Section 8.4)', () => {
    renderWithRouter(<PrivacyPolicy />)
    const text = subsectionText(/8\.4/)
    expect(text).not.toMatch(/we will automatically and permanently delete your data/i)
    // Both notices are a precondition of the sweep, not a courtesy…
    expect(text).toMatch(
      /we do not automatically delete a cancelled account unless both .{0,40}notices have been sent/i
    )
    // …and the policy says plainly that they are not being sent today.
    expect(text).toMatch(/not sending those notices yet/i)
    expect(text).toMatch(/retained past the 6-month window/i)
  })

  // Issue #1592 round 5 (G13). Round 4's own copy fix created a contradiction INSIDE § 8.4: the
  // opening sentence capped the window ("we may retain your account data in read-only mode for up
  // to 6 months"), while the paragraph two below said cancelled accounts are "retained past the
  // 6-month window". "Up to N" and "past N" cannot both be true of the same window, and the two
  // sentences render three inches apart. Six months is a FLOOR — the reactivation window we
  // commit to — not a deadline, because the sweep that would enforce a deadline is gated off.
  //
  // Both halves are asserted: the capping phrase must be gone AND the floor must be stated. The
  // negative alone would pass on any rewording that dropped the number entirely, which would be a
  // different defect, not a fix.
  it('states six months as a floor, not a cap, while accounts are kept past it (Section 8.4)', () => {
    renderWithRouter(<PrivacyPolicy />)
    const text = subsectionText(/8\.4/)
    expect(text).not.toMatch(/read-only mode for up to ?6 months/i)
    expect(text).toMatch(/read-only mode for at least ?6 months/i)
    // The two must be reconciled in the same subsection, not merely both present somewhere.
    expect(text).toMatch(/retained past the 6-month window/i)
    // …and the opening sentence must itself point forward at the "kept longer" fact, so a reader
    // who stops after one paragraph is not left with the capped reading.
    expect(text).toMatch(/longer than that today/i)
  })

  // The distinction is the whole point of I2: the retention sweep's own WHERE clause is
  // `deletion_requested_at IS NULL`, so a deletion the user REQUESTS never goes near the notice
  // gate. § 8.4 must not let a reader conclude that nothing gets deleted ever.
  it('keeps the user-requested deletion path exempt from the notice precondition (Section 8.4)', () => {
    renderWithRouter(<PrivacyPolicy />)
    const text = subsectionText(/8\.4/)
    expect(text).toMatch(/deletion you request yourself/i)
    expect(text).toMatch(/not conditioned on these notices/i)
  })

  // Issue #1592, half 2 applied to § 8.2 — "All personal data is permanently deleted" carried no
  // qualification at all, and "Some data may be retained in anonymized form" does not cover a
  // retained ip_address / user_agent, which is not anonymised.
  it('says what survives in the activity log, and that it is stripped rather than erased (Section 8.2)', () => {
    renderWithRouter(<PrivacyPolicy />)
    const text = subsectionText(/8\.2 Account Deletion/)
    expect(text).toContain('except where the law requires us to keep a record')
    expect(text).toMatch(/stripped of their contents rather than erased/i)
    // The specific fields that survive — the ones the old "anonymized form" sentence hid.
    expect(text).toMatch(/IP address/i)
    expect(text).toMatch(/browser user agent/i)
    // The pre-#1592 unqualified promise, as a plain substring — an anchored /^…$/m regex here
    // would be vacuous, because `subsectionText` collapses the subsection to a single line.
    expect(text).not.toContain('All personal data is permanently deleted')
    // The existing carve-out must stay visible, not be replaced by the new one.
    expect(text).toMatch(/anonymized form/i)
  })

  // Issue #1569: DisconnectInstitution/DeleteAccount now soft-remove a linked bank account —
  // its transaction, balance, and holdings history is RETAINED, not erased. Section 8.2's
  // "Account Deletion" heading is ambiguous now that the product also has a per-bank-account
  // "Delete Account" action (AccountsPage), so the policy must distinguish the two explicitly
  // rather than let a reader assume disconnecting one bank account erases its history the way
  // deleting the whole BoostedBudget account does.
  it('distinguishes removing a linked bank account (history retained) from deleting your BoostedBudget account (Section 8.2a)', () => {
    renderWithRouter(<PrivacyPolicy />)
    expect(screen.getByText(/removing a linked bank account/i)).toBeInTheDocument()
    expect(screen.getByText(/is retained/i)).toBeInTheDocument()
  })

  // ⚠ THE ASSERTION ABOVE IS NOT ENOUGH, and this is why it is not the only one.
  //
  // /is retained/i passed for the whole life of the defect #1569 closed. §8.2a names THREE kinds of
  // history and then makes a viewability claim about all three — "so you can still view it under
  // 'Closed accounts'" — and the holdings third of that sentence was simply false: holding_snapshots
  // was written by every sync and read by nothing, so a closed brokerage's positions were reachable
  // from no endpoint and no screen. The generic matcher could not see it, because the word
  // "retained" was still on the page.
  //
  // Each clause below is now pinned separately, and each names the surface that makes it true:
  //   - transactions   → the transactions list, filtered by account (#1569 T12)
  //   - balance        → AccountSummaryHeader's last-known balance pair (#1591)
  //   - holdings       → ClosedAccountHoldingsHistory in HoldingsTable.tsx, over
  //                      GET /api/v1/investments/holdings/history (#1569, this round)
  //
  // If any of those readers is removed, DELETE THE MATCHING CLAUSE FROM THE POLICY FIRST. A promise
  // in a privacy policy is the one place a stale claim is not merely untidy.
  it('promises exactly the three kinds of history the product can actually show back (Section 8.2a)', () => {
    renderWithRouter(<PrivacyPolicy />)

    const clause = screen.getByText(/transaction, balance, and holdings history/i)
    expect(clause).toBeInTheDocument()

    const section = clause.textContent ?? ''
    expect(section).toMatch(/is retained/i)
    expect(section).toMatch(/not erased/i)
    // The viewability half — the part that was false. "We kept it" and "you can see it" are
    // different promises, and §8.2a makes both.
    expect(section).toMatch(/you can still view it/i)
    expect(section).toMatch(/closed accounts/i)
  })

  // The policy must not promise a restore. Nothing anywhere sets deleted_at = NULL, and the one
  // account un-removal that exists is scoped to a provider-absent cause the user never invokes.
  it('does not tell the user a removed linked account can be restored (Section 8.2a)', () => {
    renderWithRouter(<PrivacyPolicy />)
    const section = screen.getByText(/transaction, balance, and holdings history/i).textContent ?? ''
    expect(section).not.toMatch(/restore|undelete|recover/i)
  })

  // ══════════════════════════════════════════════════════════════════════════════════════════
  // §8.2a's VIEW promise, tied to readers that actually exist (#1569)
  // ══════════════════════════════════════════════════════════════════════════════════════════
  //
  // ⚠ WHY THIS IS NOT A THIRD TEXT ASSERTION. Every guard above this point checks that the policy
  // SAYS something. That is exactly why the sentence shipped false three times: /is retained/i,
  // /you can still view it/i and /closed accounts/i were all satisfied by a sentence whose
  // holdings clause named a screen that did not exist, and later by whose balance clause named a
  // reader that did not exist either. A promise about the product cannot be verified by reading
  // the promise.
  //
  // So this reads the CLAUSE LIST out of the policy source, and requires each kind it names to
  // have a registered reader whose evidence is re-checked against the repository on every run.
  // Two directions, both of which must fail:
  //   - add a kind to the sentence with no reader  → the registry lookup fails
  //   - delete a reader and leave the sentence     → that reader's evidence fails
  //
  // The evidence is deliberately split into an API half (the generated spec index, which is a
  // build product of api/openapi.yaml and therefore of the Go routes) and a UI half (a source file
  // that mounts it). Either half alone is insufficient: T2 shipped an endpoint with no screen, and
  // "a component exists" says nothing about whether it can reach any data.
  //
  // ⚠ WHAT THE UI HALF ACTUALLY CHECKS, AND WHAT IT DOES NOT (T7-FIX, finding D). It was over-sold
  // to the user as proving the reader is reachable. It did not: it asserted only that a nominated
  // FILE contained a STRING somewhere, so the balance evidence was satisfied by an `import` line
  // and the transactions evidence by a key in a filter spec that issues no request at all.
  // Unmounting the component while leaving the import kept it green.
  //
  // It now checks a three-link CHAIN, each link textual but each one a different fact:
  //
  //   1. the reader file calls the hook or endpoint function, OUTSIDE its import statements;
  //   2. a named surface RENDERS that reader (`<Component`), or uses that hook;
  //   3. App.tsx mounts that surface as a route element (`<Surface />`).
  //
  // That is strictly stronger than "the string occurs somewhere" and it is still NOT a proof of
  // reachability. It is source-text matching: it cannot see that the route is behind a feature
  // flag, that the render is inside a branch nobody takes, or that the hook is called with
  // arguments that always return nothing. A real reachability proof would mean mounting the route
  // and asserting the request goes out, which is a browser-level test and lives in the e2e suite,
  // not here. The per-component unit tests (ClosedAccountBalanceHistory.test.tsx,
  // HoldingsTable.test.tsx, AccountDetailPage.test.tsx) are what assert the components mount and
  // read. This guard's job is narrower and stated as such: the policy may not name a kind of
  // history whose reader has been deleted, renamed, or unwired from its surface.

  const REPO_ROOT = join(import.meta.dir, '../../../..')

  /** The generated MCP spec index — one entry per operation in api/openapi.yaml. */
  function specOperations(): Array<{ operationId: string; parameters?: Array<{ name: string }> }> {
    const raw = readFileSync(
      join(REPO_ROOT, 'apps/mcp-server/src/spec/generated/spec-index.json'),
      'utf-8'
    )
    return JSON.parse(raw).entries
  }

  function repoFile(rel: string): string {
    return readFileSync(join(REPO_ROOT, rel), 'utf-8')
  }

  /**
   * One registered reader per kind of history §8.2a promises the user can VIEW.
   *
   * ⚠ Each entry is a claim that must be re-derived, not inherited. `apiParam` is what makes the
   * endpoint able to answer for ONE account: an operation that only answers in aggregate cannot
   * make this promise true, which is precisely how the balance clause was false while
   * getBalanceHistory existed the whole time (it takes account_types and has never taken an
   * account id).
   *
   * `readerFile` must CALL the read (`uiNeedles`, matched with imports stripped), `surfaceFile`
   * must render or use the reader (`surfaceNeedle`), and App.tsx must mount `routeElement` as a
   * route. See the chain note above for what that does and does not prove.
   */
  interface ViewableHistoryReader {
    apiOperationId: string
    apiParam: string
    /** The file that actually issues the read. */
    readerFile: string
    /** Fragments that must appear in readerFile OUTSIDE its import statements. */
    uiNeedles: string[]
    /** The screen that renders or uses the reader. */
    surfaceFile: string
    surfaceNeedle: string
    /** The route element App.tsx must mount for that screen. */
    routeElement: string
  }

  const VIEWABLE_HISTORY_READERS: Record<string, ViewableHistoryReader> = {
    transaction: {
      apiOperationId: 'getTransactions',
      apiParam: 'account_ids',
      // ⚠ NOT useTransactionFilters.ts, which the previous version of this guard named. That file
      // is a filter SPEC — `account_ids` appears in it as a key in a shape declaration and it
      // issues no request at all, so the evidence was satisfied by something that cannot make the
      // promise true. useInfiniteTransactions is the file that imports the endpoint function and
      // puts account_ids on the wire.
      readerFile: 'apps/web/src/features/transactions/hooks/useInfiniteTransactions.ts',
      uiNeedles: ['getTransactions(', 'account_ids:'],
      surfaceFile: 'apps/web/src/features/transactions/TransactionsPage.tsx',
      surfaceNeedle: 'useInfiniteTransactions(',
      routeElement: '<TransactionsPage />',
    },
    balance: {
      // #1569, this round. Before it, the ONLY per-account balance anywhere was the single
      // last-known figure in AccountSummaryHeader (#1591) — one number is not a history.
      apiOperationId: 'getAccountBalanceHistory',
      apiParam: 'id',
      readerFile: 'apps/web/src/features/accounts/components/ClosedAccountBalanceHistory.tsx',
      uiNeedles: ['useGetAccountBalanceHistory('],
      surfaceFile: 'apps/web/src/features/accounts/AccountDetailPage.tsx',
      surfaceNeedle: '<ClosedAccountBalanceHistory',
      routeElement: '<AccountDetailPage />',
    },
    holdings: {
      // #1569, previous round.
      apiOperationId: 'getAccountHoldingsHistory',
      apiParam: 'account_id',
      readerFile: 'apps/web/src/features/investments/components/HoldingsTable.tsx',
      uiNeedles: ['useGetAccountHoldingsHistory('],
      surfaceFile: 'apps/web/src/features/accounts/AccountDetailPage.tsx',
      surfaceNeedle: '<HoldingsTable',
      routeElement: '<AccountDetailPage />',
    },
  }

  /**
   * Drops `import ... from '...'` statements (single- and multi-line) so that an import line cannot
   * stand in for a call. This is why the needles above end in `(`: a name that only ever appears in
   * an import is exactly the shape that kept the balance evidence green while the component could
   * have been unmounted.
   */
  function withoutImports(source: string): string {
    return source.replace(/^import\s[\s\S]*?from\s+['"][^'"]+['"];?\s*$/gm, '')
  }

  /**
   * The kinds §8.2a actually names, read out of the policy source rather than restated here.
   *
   * Anchored on the possessive that introduces the list, so a sentence reworded around it still
   * parses, and a list item added anywhere in it is picked up.
   */
  function kindsNamedInPolicy(): string[] {
    const source = repoFile('apps/web/src/legal/PrivacyPolicy.tsx')
    // JSX wraps the paragraph across lines; collapse to one line before matching.
    const flat = source.replace(/\s+/g, ' ')
    const m = flat.match(/that account's ([^<]+?) history <strong>is\s*retained<\/strong>/)
    expect(m, 'the §8.2a clause list could not be parsed — this guard would be vacuous').not.toBeNull()
    return m![1]
      .split(/,| and /)
      .map((k) => k.trim())
      .filter(Boolean)
  }

  it('names at least the three kinds of history it has always named (Section 8.2a)', () => {
    // A canary for the PARSE, not a freeze on the sentence. A parse that silently yielded [] or
    // one item would make the registry test below vacuous and green forever — which is the failure
    // mode this whole block exists to prevent. Deliberately NOT an exact-equality assertion: a
    // FOURTH kind added alongside a real reader should pass here and be caught (or not) by the
    // registry test on its own merits, rather than failing a list nobody remembered to update.
    const kinds = kindsNamedInPolicy()
    expect(kinds).toEqual(expect.arrayContaining(['transaction', 'balance', 'holdings']))
    expect(kinds.length).toBeGreaterThanOrEqual(3)
  })

  it('promises no kind of viewable history without a reader that exists (Section 8.2a)', () => {
    const ops = specOperations()

    for (const kind of kindsNamedInPolicy()) {
      const reader = VIEWABLE_HISTORY_READERS[kind]
      expect(
        reader,
        `§8.2a promises the user can view "${kind}" history, but no reader is registered for it. ` +
          'Either build the reader and register it here, or DELETE THE CLAUSE FROM THE POLICY. ' +
          'A promise in a privacy policy is the one place a stale claim is not merely untidy.'
      ).toBeDefined()

      const op = ops.find((o) => o.operationId === reader.apiOperationId)
      expect(op, `${kind}: ${reader.apiOperationId} is no longer in the API spec`).toBeDefined()
      expect(
        (op!.parameters ?? []).map((prm) => prm.name),
        `${kind}: ${reader.apiOperationId} no longer takes ${reader.apiParam}, so it can no longer ` +
          'answer for ONE account — which is what §8.2a promises about a single removed account.'
      ).toContain(reader.apiParam)

      // Link 1: the reader file CALLS the read, outside its imports.
      const readerBody = withoutImports(repoFile(reader.readerFile))
      for (const needle of reader.uiNeedles) {
        expect(
          readerBody,
          `${kind}: ${reader.readerFile} no longer calls ${needle} outside its import statements, ` +
            'so the endpoint is reachable by an API consumer and not by the person the policy is ' +
            'addressed to. An import alone is not a read.'
        ).toContain(needle)
      }

      // Link 2: a surface renders or uses that reader.
      expect(
        withoutImports(repoFile(reader.surfaceFile)),
        `${kind}: ${reader.surfaceFile} no longer contains ${reader.surfaceNeedle}, so the reader ` +
          'exists but nothing puts it on a screen. Deleting the render while leaving the import is ' +
          'exactly the edit this link exists to catch.'
      ).toContain(reader.surfaceNeedle)

      // Link 3: App.tsx mounts that surface as a route.
      expect(
        repoFile('apps/web/src/App.tsx'),
        `${kind}: App.tsx no longer mounts ${reader.routeElement}, so the screen exists and the ` +
          'user cannot navigate to it.'
      ).toContain(reader.routeElement)
    }
  })

  // The other half of the promise, and the half that was quietly wrong even after the readers were
  // built: §8.2a used to say all three kinds are viewable "under 'Closed accounts' on your Accounts
  // page". For a closed checking/savings/credit account that was the wrong place — the account
  // detail page renders a transactions link only for an investment account, so its transactions are
  // found in the Transactions list. The sentence now names each location, and this pins that.
  it('names the right place for each kind, not one place for all three (Section 8.2a)', () => {
    renderWithRouter(<PrivacyPolicy />)
    const section = subsectionText(/8\.2a Removing a Linked Bank Account/)

    expect(section).toMatch(/closed accounts/i)
    expect(section).toMatch(/balances/i)
    expect(section).toMatch(/holdings/i)
    // Transactions are NOT under "Closed accounts" — they are in the Transactions list.
    expect(section).toMatch(/transactions (stay|remain) in your Transactions list/i)
    expect(section).toMatch(/filter by that account/i)
  })

  it('renders household member deletion cross-reference (Section 8.5)', () => {
    renderWithRouter(<PrivacyPolicy />)
    expect(screen.getByText(/removed household members/i)).toBeInTheDocument()
    expect(screen.getByText(/30 days before permanent deletion/i)).toBeInTheDocument()
  })

  it('renders Sentry data disclosure with specifics', () => {
    renderWithRouter(<PrivacyPolicy />)
    expect(screen.getByText(/device and browser information/i)).toBeInTheDocument()
    expect(screen.getByText(/application state at the time of an error/i)).toBeInTheDocument()
  })

  it('renders CPRA sensitive personal information right', () => {
    renderWithRouter(<PrivacyPolicy />)
    expect(screen.getByText(/limit the use of your sensitive personal information/i)).toBeInTheDocument()
    expect(screen.getByText(/financial account numbers, balances, and/i)).toBeInTheDocument()
  })

  it('renders Do Not Track disclosure (Section 7.2)', () => {
    renderWithRouter(<PrivacyPolicy />)
    // "Do Not Track" appears in both the h3 heading and the paragraph body
    expect(screen.getAllByText(/do not track/i).length).toBeGreaterThan(0)
    expect(screen.getByText(/browser analytics remain off/i)).toBeInTheDocument()
    expect(screen.queryByText(/we do not currently respond/i)).not.toBeInTheDocument()
  })

  it('renders data breach notification commitment', () => {
    renderWithRouter(<PrivacyPolicy />)
    expect(screen.getByText(/data breach notification/i)).toBeInTheDocument()
    expect(screen.getByText(/without unreasonable delay/i)).toBeInTheDocument()
  })

  it('replaces pricing table with link to /pricing page', () => {
    renderWithRouter(<PrivacyPolicy />)
    const pricingLink = screen.getByRole('link', { name: /pricing page/i })
    expect(pricingLink.getAttribute('href')).toBe('/pricing')
    // Confirm pricing amounts are gone
    expect(screen.queryByText(/\$8\.99/)).not.toBeInTheDocument()
    expect(screen.queryByText(/\$12\.99/)).not.toBeInTheDocument()
  })

  it('renders global "do not sell" statement outside CCPA section', () => {
    renderWithRouter(<PrivacyPolicy />)
    const doNotSellInstances = screen.getAllByText(/we do not sell your personal information|do not sell or share personal information/i)
    expect(doNotSellInstances.length).toBeGreaterThan(0)
  })

  it('renders CCPA rights section', () => {
    renderWithRouter(<PrivacyPolicy />)
    expect(screen.getByText(/california privacy rights/i)).toBeInTheDocument()
    expect(screen.getByText(/california resident/i)).toBeInTheDocument()
  })

  it('renders SCCs for international transfers', () => {
    renderWithRouter(<PrivacyPolicy />)
    expect(screen.getByText(/standard contractual clauses/i)).toBeInTheDocument()
  })

  it('renders link to Terms of Service', () => {
    renderWithRouter(<PrivacyPolicy />)
    const tosLinks = screen.getAllByRole('link', { name: /terms of service/i })
    expect(tosLinks.length).toBeGreaterThan(0)
    expect(tosLinks.some((link) => link.getAttribute('href') === '/legal/terms')).toBe(true)
  })

  it('renders link to Cookie Policy', () => {
    renderWithRouter(<PrivacyPolicy />)
    const cookieLinks = screen.getAllByRole('link', { name: /cookie policy/i })
    expect(cookieLinks.length).toBeGreaterThan(0)
    expect(cookieLinks.some((link) => link.getAttribute('href') === '/legal/cookie-policy')).toBe(true)
  })

  it('documents the fail-closed browser analytics privacy boundary', () => {
    renderWithRouter(<PrivacyPolicy />)
    expect(screen.getByText(/pseudonymous product analytics/i)).toBeInTheDocument()
    expect(screen.getByText(/no posthog analytics persistence or analytics event capture occurs before/i)).toBeInTheDocument()
    expect(screen.getByText(/authenticated support chat when you choose to contact us/i)).toBeInTheDocument()
    expect(screen.getByText(/support message content.*sanitized application route/i)).toBeInTheDocument()
    expect(screen.getByText(/temporary support session identifier.*only after you select support/i)).toBeInTheDocument()
    expect(screen.getByText(/not included in authenticated requests.*removed on logout.*account switch/i)).toBeInTheDocument()
    expect(screen.getByText(/production session replay is disabled/i)).toBeInTheDocument()
    expect(screen.getByText(/do not track.*browser only/i)).toBeInTheDocument()
    expect(screen.getByText(/tab-scoped sessionStorage/i)).toBeInTheDocument()
  })

  it('renders application-level encryption details (Section 4.2)', () => {
    renderWithRouter(<PrivacyPolicy />)
    // Algorithm name
    expect(screen.getByText(/AES-256-GCM/i)).toBeInTheDocument()
    // Section 2.1 forward-reference
    expect(screen.getByText(/sensitive personal and financial fields.*encrypted at the application level/i)).toBeInTheDocument()
    // Section 4.2 headings and content
    expect(screen.getAllByText(/application.level encryption/i).length).toBeGreaterThan(0)
    expect(screen.getAllByText(/display masking/i).length).toBeGreaterThan(0)
    // Masking examples
    expect(screen.getByText(/u\*\*\*@example\.com/)).toBeInTheDocument()
    expect(screen.getByText(/\*\*\*\*9012/)).toBeInTheDocument()
    // Blind indexes disclosure
    expect(screen.getAllByText(/blind index/i).length).toBeGreaterThan(0)
    // Key management
    expect(screen.getAllByText(/cryptographically derived/i).length).toBeGreaterThan(0)
  })

  // Bumped by #1650 because the PostHog Support and Surveys processing disclosures changed.
  it('renders correct Last Updated date (September 13, 2026)', () => {
    renderWithRouter(<PrivacyPolicy />)
    expect(screen.getByText(/september 13, 2026/i)).toBeInTheDocument()
  })

  it('renders household data sharing as a consent-based lawful basis (Section 3.1)', () => {
    renderWithRouter(<PrivacyPolicy />)
    const consentBasisItem = screen.getByText(/analytics cookies \(posthog\), marketing emails/i)
    expect(consentBasisItem.textContent).toMatch(/household data sharing/i)
  })

  it('renders Household Members data-sharing subsection with accurate visible/never-shared lists (Section 9)', () => {
    renderWithRouter(<PrivacyPolicy />)
    expect(screen.getByText(/9\.1 household data sharing/i)).toBeInTheDocument()
    expect(screen.getAllByText(/household members/i).length).toBeGreaterThan(0)
    // Visible to household members
    expect(screen.getByText(/account balances/i)).toBeInTheDocument()
    expect(screen.getByText(/net worth contribution/i)).toBeInTheDocument()
    expect(screen.getByText(/spending activity, including merchant names/i)).toBeInTheDocument()
    expect(screen.getByText(/income sources and savings-rate figures/i)).toBeInTheDocument()
    // Never shared with household members
    expect(screen.getByText(/individual transaction line items/i)).toBeInTheDocument()
    // Covers both unmasked and masked account numbers — the household aggregate response
    // never serializes even a masked/last-4 account number (only id/name/balance).
    expect(screen.getByText(/full account numbers or masks/i)).toBeInTheDocument()
    expect(screen.getByText(/login credentials/i)).toBeInTheDocument()
  })

  it('describes both household consent moments and per-account revocation (Section 9)', () => {
    renderWithRouter(<PrivacyPolicy />)
    // Consent at invite-join
    expect(screen.getByText(/before joining a household/i)).toBeInTheDocument()
    // Consent at newly-linked-account sharing
    expect(screen.getByText(/accounts you link.*after joining a household/i)).toBeInTheDocument()
    // Revocation via hiding
    expect(screen.getByText(/hide.*account.*household/i)).toBeInTheDocument()
  })

  it('scopes the recorded-consent language to invite-join only, not post-join account sharing (Section 9.1)', () => {
    renderWithRouter(<PrivacyPolicy />)
    // The "recorded... version identifier and timestamp" claim describes the versioned
    // consent snapshot written by AcceptInvite (backend household_service.go), which only
    // happens at invite-join time. Post-join newly-linked-account sharing only writes a
    // household_account_shares visibility row (SetShareVisibility), not a versioned
    // consent entry, so that paragraph must not carry this claim.
    const recordedText = screen.getByText(/version identifier and timestamp/i)
    expect(recordedText.textContent).toMatch(/before joining a household/i)
    expect(recordedText.textContent).not.toMatch(/after joining a household/i)
  })
})
