/**
 * McpPage — the public /mcp marketing + docs page (Issue #1411).
 *
 * Read-dominant with a Persuade opening: hero + one dramatized transcript +
 * a one-line trust strip above a hard visual rule, then a docs-style layout
 * (sticky TOC rail + content) below. See .superpowers/sdd/task-P8-brief.md
 * for the full spec — every verbatim string below is copied from its
 * "Hero copy" / "§4 access matrix" / "Transcripts" sections.
 *
 * capabilitySummary.json is GENERATED (apps/mcp-server/scripts/generate-mcp-
 * capability-summary.ts reads the real capability manifest + spec index and
 * emits only aggregates). apps/web never imports the manifest directly — see
 * that script's doc comment for why. The drift test at
 * apps/mcp-server/src/manifest/capability-summary.drift.test.ts fails the
 * build if this committed file falls out of sync with the manifest.
 *
 * Claims discipline: this file must never say the agent "writes and runs
 * code" (it calls a typed client from a declared operation list), that
 * tokens are "bound to your OAuth grant" (that binding is inert at runtime —
 * #1418; say "your account and the exact request"), that recipes are
 * "read-only" (saving one is a confirmation-gated write), that disconnection
 * is available from account settings or is instant (no such UI exists yet —
 * #1419; it's Clerk-dashboard-only and fails open during a Clerk outage),
 * that recipes can run automatically (no scheduler — #1220), that a paid
 * plan is required (api_access is unenforced), or any unfalsifiable security
 * adjective ("bank-grade", "military-grade", "zero-trust").
 */

import { useEffect, useLayoutEffect } from 'react'
import { Check } from 'lucide-react'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics'
import { McpHero } from './components/McpHero'
import { McpTranscript } from './components/McpTranscript'
import { McpTocRail } from './components/McpTocRail'
import { McpSetupTabs } from './components/McpSetupTabs'
import capabilitySummary from './data/capabilitySummary.json'

const PAGE_TITLE = 'MCP Server — BoostedBudget'
const PAGE_DESCRIPTION =
  'Connect Claude, Cursor, or VS Code to your BoostedBudget account through a remote MCP server. Read your accounts, transactions, budgets, and portfolio — every change waits for your confirmation.'
const CANONICAL_URL = 'https://boostedbudget.com/mcp'

const TOC_SECTIONS = [
  { id: 'overview', label: 'Overview' },
  { id: 'capabilities', label: 'Capabilities' },
  { id: 'how-it-works', label: 'How it works' },
  { id: 'connect', label: 'Connect' },
  { id: 'write-safety', label: 'Write safety' },
  { id: 'example-prompts', label: 'Example prompts' },
  { id: 'recipes', label: 'Recipes' },
  { id: 'data-and-access', label: 'Data and access' },
  { id: 'limits', label: 'Limits' },
  { id: 'troubleshooting', label: 'Troubleshooting' },
]

const TRANSCRIPT_SUBSCRIPTION_AUDIT = [
  {
    speaker: 'You' as const,
    lines: [
      "Which of my subscriptions are costing me the most, and can you cancel the ones I don't use?",
    ],
  },
  {
    speaker: 'Agent' as const,
    lines: [
      'execute({ calls: [',
      '  { operationId: "getRecurringSubscriptions" },',
      '  { operationId: "getLargestTransactionsForPeriod", params: { limit: 5 } }',
      ']})',
      '→ 5 recurring charges found, $71.96/mo total. Latest Peacock charge: $6.99 on Checking ···4021, no matching activity in 60 days.',
    ],
  },
  {
    speaker: 'Agent' as const,
    lines: [
      "I can't cancel a subscription myself — that only happens with the merchant. I can tag that Peacock charge \"Review to cancel\" so it's easy to find when you do.",
    ],
  },
  { speaker: 'You' as const, lines: ['Yes, tag it.'] },
  {
    speaker: 'Agent' as const,
    lines: [
      'execute({ calls: [{ operationId: "updateTransactionTags", pathParams: { id: "txn_peacock_04a1" }, body: { tags: ["Review to cancel"] } }] })',
      '⟳ Confirm: set tags on this transaction to ["Review to cancel"]?',
    ],
  },
  { speaker: 'You' as const, lines: ['Confirmed.'] },
  {
    speaker: 'Agent' as const,
    lines: [
      'execute({ ..., confirmationToken: "3f7a9c..." })',
      "→ Tagged. Cancelling Peacock itself still happens on peacocktv.com or with your bank — BoostedBudget can't do that step for you.",
    ],
  },
]

const TRANSCRIPT_HOUSEHOLD_REFUSAL = [
  { speaker: 'You' as const, lines: ['My partner sent me a household invite. Accept it.'] },
  {
    speaker: 'Agent' as const,
    lines: [
      'search({ query: "household invite", tag: "Household" })',
      '→ getMyHousehold, listHouseholdInvites, getHouseholdInvitePreview',
    ],
  },
  {
    speaker: 'Agent' as const,
    lines: [
      "I can see the invite — it's from the account ending 8815 and it's still pending. I can't accept it.",
      "Accepting a household invite is consent to share financial data, so it isn't available to agents at all. Open BoostedBudget and accept it there.",
    ],
  },
]

/** Restores document.title / meta description / canonical link on unmount. Best-effort SEO: a
 * crawler that doesn't execute JavaScript still sees index.html's static "/" canonical — a real
 * fix needs prerendering (#1423). */
function useDocumentHead(title: string, description: string, canonicalHref: string) {
  useLayoutEffect(() => {
    const previousTitle = document.title
    document.title = title

    let descriptionTag = document.querySelector<HTMLMetaElement>('meta[name="description"]')
    const descriptionExisted = descriptionTag !== null
    const previousDescription = descriptionTag?.getAttribute('content') ?? null
    if (!descriptionTag) {
      descriptionTag = document.createElement('meta')
      descriptionTag.setAttribute('name', 'description')
      document.head.appendChild(descriptionTag)
    }
    descriptionTag.setAttribute('content', description)

    let canonicalTag = document.querySelector<HTMLLinkElement>('link[rel="canonical"]')
    const canonicalExisted = canonicalTag !== null
    const previousCanonical = canonicalTag?.getAttribute('href') ?? null
    if (!canonicalTag) {
      canonicalTag = document.createElement('link')
      canonicalTag.setAttribute('rel', 'canonical')
      document.head.appendChild(canonicalTag)
    }
    canonicalTag.setAttribute('href', canonicalHref)

    return () => {
      document.title = previousTitle

      if (descriptionExisted) {
        if (previousDescription !== null) descriptionTag?.setAttribute('content', previousDescription)
      } else {
        descriptionTag?.remove()
      }

      if (canonicalExisted) {
        if (previousCanonical !== null) canonicalTag?.setAttribute('href', previousCanonical)
      } else {
        canonicalTag?.remove()
      }
    }
  }, [title, description, canonicalHref])
}

function Section({
  id,
  title,
  children,
}: {
  id: string
  title: string
  children: React.ReactNode
}) {
  return (
    <section aria-labelledby={id} className="space-y-4">
      <h2 id={id} className="scroll-mt-24 text-2xl font-semibold text-foreground">
        {title}
      </h2>
      {children}
    </section>
  )
}

function topTags(readCountByTag: Record<string, number>, count: number): [string, number][] {
  return Object.entries(readCountByTag)
    .sort((a, b) => b[1] - a[1])
    .slice(0, count)
}

export function McpPage() {
  const trackEvent = useTrackEvent()

  useDocumentHead(PAGE_TITLE, PAGE_DESCRIPTION, CANONICAL_URL)

  useEffect(() => {
    trackEvent(ANALYTICS_EVENTS.MCP_PAGE_VIEWED)
  }, [trackEvent])

  // Codex review finding #6: McpSetupTabs reads a #connect-<slug> hash to pick the active
  // client tab, but nothing scrolled the page there — a support link landed a user at the
  // top of the page instead of the Connect section they were sent to. Scroll on mount only
  // (not on every hash change) so switching tabs later doesn't re-jump the page.
  useEffect(() => {
    if (window.location.hash.startsWith('#connect-')) {
      document.getElementById('connect')?.scrollIntoView()
    }
  }, [])

  const {
    totalOperations,
    allowedOperations,
    readOperations,
    writeOperations,
    excludedOperations,
    destructiveWriteOperations,
    readCountByTag,
    exclusionGroups,
  } = capabilitySummary

  const featuredTags = topTags(readCountByTag, 5)

  return (
    <div>
      {/* Above the rule: centred, larger type, Persuade opening. */}
      <div className="mx-auto max-w-3xl px-4 py-16 sm:px-8">
        <McpHero allowedOperationsCount={allowedOperations} />
        <div className="mt-12">
          <McpTranscript turns={TRANSCRIPT_SUBSCRIPTION_AUDIT} showsStatusMarkers />
        </div>
      </div>

      <hr className="border-border" />

      {/* Below the rule: docs-style, max-w-6xl, left-aligned, one type step smaller. */}
      <div className="mx-auto max-w-6xl px-4 py-12 text-[0.9375rem] sm:px-8">
        <div className="lg:flex lg:gap-12">
          <McpTocRail sections={TOC_SECTIONS} />

          <div className="flex-1 space-y-16 text-left">
            <Section id="overview" title="What the MCP server is">
              <p className="text-foreground">
                BoostedBudget runs a remote MCP (Model Context Protocol) server at a single
                URL. Connect Claude Desktop, Claude Code, Cursor, or VS Code, sign in with your
                BoostedBudget account, and your agent can read your accounts, transactions,
                budgets, and portfolio directly — no exporting statements, no copy-pasting
                numbers into a chat window.
              </p>
              <p className="text-foreground">
                Every write — recategorizing a transaction, tagging it, saving a recipe — waits
                for you to confirm it first. Nothing in your account changes until you say yes.
              </p>
            </Section>

            <Section id="capabilities" title="What it can and can't do">
              <p className="text-foreground">
                The API behind BoostedBudget has {totalOperations} operations. {allowedOperations}{' '}
                of them are reachable through MCP ({readOperations} read, {writeOperations}{' '}
                write — every write confirmation-gated, {destructiveWriteOperations} of them
                deletions). The rest — {excludedOperations} operations — are permanently
                excluded and never reachable at all, no matter what an agent asks for.
              </p>
              <p className="text-foreground">
                Reads span every area of the product, including{' '}
                {featuredTags.map(([tag, count], i) => (
                  <span key={tag}>
                    {i > 0 && ', '}
                    {tag} ({count})
                  </span>
                ))}
                .
              </p>
              <ul className="grid gap-2 sm:grid-cols-2">
                <li className="flex items-start gap-2 text-foreground">
                  <Check className="mt-0.5 h-4 w-4 shrink-0 text-primary" aria-hidden="true" />
                  Analyze spending, budgets, net worth, and portfolio performance
                </li>
                <li className="flex items-start gap-2 text-foreground">
                  <Check className="mt-0.5 h-4 w-4 shrink-0 text-primary" aria-hidden="true" />
                  Recategorize, tag, and review transactions — with your confirmation
                </li>
                <li className="flex items-start gap-2 text-foreground">
                  <Check className="mt-0.5 h-4 w-4 shrink-0 text-primary" aria-hidden="true" />
                  Create and adjust budgets, categories, tags, and savings goals
                </li>
                <li className="flex items-start gap-2 text-foreground">
                  <Check className="mt-0.5 h-4 w-4 shrink-0 text-primary" aria-hidden="true" />
                  Save and run named recipes for repeatable analysis
                </li>
              </ul>

              <p className="text-foreground">
                Here&apos;s what that boundary looks like when an agent hits it:
              </p>
              <McpTranscript turns={TRANSCRIPT_HOUSEHOLD_REFUSAL} />

              <div>
                <h3 className="mb-2 text-base font-semibold text-foreground">
                  Permanently excluded — cannot be enabled
                </h3>
                <ul className="space-y-1.5">
                  {exclusionGroups.map((group) => (
                    <li key={group.reason} className="flex justify-between gap-4 text-muted-foreground">
                      <span>{group.reason}</span>
                      <span className="shrink-0 font-medium text-foreground">{group.count}</span>
                    </li>
                  ))}
                </ul>
              </div>
            </Section>

            <Section id="how-it-works" title={`Two tools instead of ${allowedOperations}`}>
              <p className="text-foreground">
                Rather than exposing all {allowedOperations} reachable operations as separate
                tools, the server gives an agent two primitives instead of {allowedOperations}{' '}
                tools: <code>search</code> to discover the right operation by keyword, tag, or
                read/write class, and <code>execute</code> to call it. <code>execute</code> calls
                a typed client against a declared list of operations — it does not write or run
                arbitrary code against your finances.
              </p>
              <p className="text-foreground">
                The capability manifest behind both tools is deny-by-default: an operation is
                reachable only if it&apos;s explicitly classified as read or write. Anything not
                listed — or explicitly excluded — is denied, with no exceptions carved out at
                request time.
              </p>
              <p className="text-foreground">
                A third set of tools, <code>recipe.*</code>, builds saved, repeatable
                compositions on top of <code>search</code> and <code>execute</code> — see{' '}
                <a href="#recipes" className="text-primary underline underline-offset-2">
                  Saved recipes
                </a>{' '}
                below.
              </p>
            </Section>

            <Section id="connect" title="Connect your client">
              <p className="text-foreground">
                You need a BoostedBudget account. During your 60-day trial and while your
                subscription is active, your agent can read your data and make changes you
                confirm. If your subscription lapses, reading keeps working and changes are
                blocked — unless you&apos;re a covered member of an active household plan, in
                which case your agent can keep making confirmed changes too.
              </p>
              <McpSetupTabs />
            </Section>

            <Section id="write-safety" title="Every write waits for you">
              <p className="text-foreground">
                Every write goes through the same confirmation flow, whether it&apos;s a single
                tag update or a saved recipe: the agent calls <code>execute</code>, the server
                describes what would happen and returns a one-time confirmation token, the
                agent presents that to you, and only after you say yes does the agent re-invoke{' '}
                <code>execute</code> with the token to actually make the change.
              </p>
              <p className="text-foreground">
                Each token is single-use and expires after 5 minutes. It&apos;s tied to your
                account and the exact request being confirmed — the specific operation and
                arguments — so it can&apos;t be replayed for a different call.
              </p>
              <p className="text-foreground">
                Some categories of change are excluded from MCP entirely and can&apos;t be
                confirmed into existence: Plaid account linking and unlinking, account deletion,
                admin operations, and money movement. There is no money-movement operation at
                all — Boosts&apos; <code>acceptTransferSuggestion</code> records your intent to
                transfer; the transfer itself happens at your bank and is matched afterward.
              </p>
            </Section>

            <Section id="example-prompts" title="Example prompts">
              <ul className="space-y-4">
                <li className="rounded-lg border border-border bg-card p-4">
                  <p className="font-mono text-sm text-foreground">
                    &ldquo;What&apos;s my dining spend this month, and is it trending up?&rdquo;
                  </p>
                  <p className="mt-1 text-foreground">
                    The agent finds the right analytics operations and answers in one message —
                    no dashboard required.
                  </p>
                </li>
                <li className="rounded-lg border border-border bg-card p-4">
                  <p className="font-mono text-sm text-foreground">
                    &ldquo;Review my unreviewed transactions.&rdquo;
                  </p>
                  <p className="mt-1 text-foreground">
                    The agent pulls your review inbox, flags anything that looks miscategorized,
                    and asks before bulk-approving the rest.
                  </p>
                </li>
                <li className="rounded-lg border border-border bg-card p-4">
                  <p className="font-mono text-sm text-foreground">
                    &ldquo;What should I do with my extra cash this month?&rdquo;
                  </p>
                  <p className="mt-1 text-foreground">
                    The agent checks your budgets, savings goals, and any pending Boosts
                    transfer suggestions, then explains the options — it won&apos;t move
                    anything without your confirmation.
                  </p>
                </li>
                <li className="rounded-lg border border-border bg-card p-4">
                  <p className="font-mono text-sm text-foreground">
                    &ldquo;Save a recipe that summarizes my cash flow every week.&rdquo;
                  </p>
                  <p className="mt-1 text-foreground">
                    The agent saves a named, reusable set of read calls you can re-run anytime.
                  </p>
                </li>
              </ul>
            </Section>

            <Section id="recipes" title="Saved recipes">
              <p className="text-foreground">
                A recipe is a named, saved sequence of calls — for example, &ldquo;Monthly
                Financial Review&rdquo; or &ldquo;Portfolio Review.&rdquo; <code>recipe.save</code>{' '}
                stores it, <code>recipe.list</code> shows what you&apos;ve saved, and{' '}
                <code>recipe.run</code> resolves a saved recipe to its calls, which the agent
                then passes to <code>execute</code>.
              </p>
              <p className="text-foreground">
                Recipes can only be built from read calls in v1 — but{' '}
                <code>recipe.save</code> is still a confirmation-gated write, since it changes
                what&apos;s stored on your account. You&apos;ll see the same confirm-before-it-
                happens flow described above.
              </p>
              <p className="text-foreground">
                There&apos;s no scheduler yet — recipes run when you or your agent ask for them,
                not on a timer (tracked in{' '}
                <a
                  href="https://github.com/BoostedBudget/BoostedBudget/issues/1220"
                  className="text-primary underline underline-offset-2"
                >
                  #1220
                </a>
                ). A handful of starter templates — a monthly financial review, a spending
                deep-dive, a subscription audit, a portfolio review — are built in; ask your
                agent to list them.
              </p>
            </Section>

            <Section id="data-and-access" title="Data handling and access control">
              <p className="text-foreground">
                The Worker that handles MCP requests never connects to the database. Every call
                is proxied to the same Go backend that powers the BoostedBudget web app, which
                enforces account ownership and validates every request the same way it would
                from a browser.
              </p>
              <p className="text-foreground">
                Your BoostedBudget credentials are injected server-side after you sign in —
                they&apos;re never visible in a tool&apos;s input or output, and the agent never
                stores them. Tool calls, successful or not, are recorded to an audit log on a
                best-effort basis.
              </p>
              <p className="text-foreground">
                Access is revoked from the Clerk dashboard today — an in-app disconnect page is
                planned (
                <a
                  href="https://github.com/BoostedBudget/BoostedBudget/issues/1419"
                  className="text-primary underline underline-offset-2"
                >
                  #1419
                </a>
                ) but doesn&apos;t exist yet. A revoked grant is detected within about 30–60
                seconds via a background check on every request. That&apos;s a best-effort SLA,
                not a hard security boundary: the check fails open if Clerk&apos;s API is
                unreachable, so a Clerk outage does not block MCP access.
              </p>
            </Section>

            <Section id="limits" title="Limits and known gaps">
              <ul className="space-y-2 text-foreground">
                <li>
                  60 requests/minute and 5 concurrent calls, enforced per session, not per
                  account — a second agent session against the same account gets its own limit.
                </li>
                <li>
                  <code>execute</code> accepts at most 25 calls per invocation.
                </li>
                <li>
                  Each call&apos;s result is truncated to about 500 characters — ask for a small{' '}
                  <code>limit</code> and page through results rather than requesting everything
                  at once.
                </li>
                <li>
                  <code>/mcp</code> (Streamable HTTP, used by every tab above) is the
                  recommended transport and is checked on every request. <code>/sse</code> is
                  also accepted, but a revoked grant is only checked when an{' '}
                  <code>/sse</code> connection opens, not on every message afterward — a
                  long-lived stream can outlive a revocation until it reconnects.
                </li>
                <li>
                  <code>execute</code> calls a typed API client from a declared list of
                  operations. A broader code-execution sandbox is scaffolded in the codebase but
                  not enabled in production today.
                </li>
                <li>
                  Whether Cursor&apos;s remote MCP support performs Dynamic Client Registration
                  automatically is unverified — see the Cursor tab above (
                  <a
                    href="https://github.com/BoostedBudget/BoostedBudget/issues/1421"
                    className="text-primary underline underline-offset-2"
                  >
                    #1421
                  </a>
                  ).
                </li>
              </ul>
            </Section>

            <Section id="troubleshooting" title="If something doesn't connect">
              <ul className="space-y-3 text-foreground">
                <li>
                  <span className="font-medium">Boostedbudget doesn&apos;t appear after adding it: </span>
                  double-check you pasted the full URL, including the <code>https://</code>{' '}
                  scheme, and left Advanced settings empty so Dynamic Client Registration can
                  run.
                </li>
                <li>
                  <span className="font-medium">Sign-in opens but nothing happens after: </span>
                  close the tab and retry — the first sign-in can take a few seconds.
                </li>
                <li>
                  <span className="font-medium">
                    <code>claude mcp list</code> shows <code>! Needs authentication</code>:{' '}
                  </span>
                  that&apos;s expected before you run <code>/mcp</code> → Authenticate — it is
                  not an error.
                </li>
                <li>
                  <span className="font-medium">I revoked access but the agent can still call tools: </span>
                  revocation takes up to about 30–60 seconds to take effect (see{' '}
                  <a href="#data-and-access" className="text-primary underline underline-offset-2">
                    Data and access
                  </a>
                  ). If it&apos;s been longer, contact support.
                </li>
                <li>
                  <span className="font-medium">Cursor asks for a Client ID/Secret: </span>
                  Cursor&apos;s Dynamic Client Registration support is unverified — see the
                  Cursor tab above.
                </li>
              </ul>
              <p className="text-muted-foreground">
                Still stuck?{' '}
                <a
                  href="mailto:support@boostedbudget.com"
                  className="text-foreground underline underline-offset-4 hover:text-primary"
                >
                  Contact support
                </a>
                .
              </p>
            </Section>
          </div>
        </div>
      </div>
    </div>
  )
}
