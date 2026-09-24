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
 * Claims discipline: keep this page user-facing and precise. Describe account
 * access, confirmation, and practical limits without exposing service topology,
 * internal operation names, implementation details, or unfinished roadmap items.
 */

import { useEffect } from 'react'
import { Link } from 'react-router'
import { Check } from 'lucide-react'
import { useDocumentHead } from '@/shared/hooks/useDocumentHead'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics'
import { McpHero } from './components/McpHero'
import { McpTranscript } from './components/McpTranscript'
import { McpTocRail } from './components/McpTocRail'
import { McpSetupTabs } from './components/McpSetupTabs'
import capabilitySummary from './data/capabilitySummary.json'

const PAGE_TITLE = 'MCP Server — BoostedBudget'
const PAGE_DESCRIPTION =
  'Connect Claude or VS Code to your BoostedBudget account through a remote MCP server. Read your accounts, transactions, budgets, and portfolio — every change waits for your confirmation.'
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
  { id: 'agent-discovery', label: 'Agent discovery' }
]

const CAPABILITY_HIGHLIGHTS = [
  'Analyze spending, budgets, net worth, and portfolio performance',
  'Recategorize, tag, and review transactions — with your confirmation',
  'Create and adjust budgets, categories, tags, and savings goals',
  'Save named reviews and run them again when you need them'
]

const TRANSCRIPT_SUBSCRIPTION_AUDIT = [
  {
    speaker: 'You' as const,
    lines: ["Which of my subscriptions are costing me the most, and can you cancel the ones I don't use?"]
  },
  {
    speaker: 'Agent' as const,
    lines: [
      'I found 5 recurring charges totaling $71.96/mo. The latest Peacock charge is $6.99 on Checking ···4021, with no matching activity in 60 days.',
      'I can\'t cancel a subscription myself — that happens with the merchant. I can tag the Peacock charge "Review to cancel" so it\'s easy to find.'
    ]
  },
  { speaker: 'You' as const, lines: ['Yes, tag it.'] },
  {
    speaker: 'Agent' as const,
    lines: ['⟳ Confirm: tag the Peacock transaction as "Review to cancel"?']
  },
  { speaker: 'You' as const, lines: ['Confirmed.'] },
  {
    speaker: 'Agent' as const,
    lines: [
      "→ Tagged. Cancelling Peacock itself still happens on peacocktv.com or with your bank — BoostedBudget can't do that step for you."
    ]
  }
]

const TRANSCRIPT_HOUSEHOLD_REFUSAL = [
  {
    speaker: 'You' as const,
    lines: ['My partner sent me a household invite. Accept it.']
  },
  {
    speaker: 'Agent' as const,
    lines: [
      "I found the invite — it's from the account ending 8815 and it's still pending. I can't accept it.",
      "Accepting a household invite is consent to share financial data, so it isn't available to agents at all. Open BoostedBudget and accept it there."
    ]
  }
]

function Section({ id, title, children }: { id: string; title: string; children: React.ReactNode }) {
  return (
    <section aria-labelledby={id} className="space-y-4">
      <h2 id={id} className="scroll-mt-24 text-2xl font-semibold text-foreground">
        {title}
      </h2>
      {children}
    </section>
  )
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

  const { allowedOperations, readOperations, writeOperations, destructiveWriteOperations, publicBoundaries } =
    capabilitySummary

  return (
    <div>
      {/* Above the rule: centred, larger type, Persuade opening. */}
      <div className="mx-auto max-w-3xl px-4 py-16 sm:px-8">
        <McpHero capabilityCount={allowedOperations} />
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
                BoostedBudget runs a remote MCP (Model Context Protocol) server at a single URL. Connect Claude Desktop,
                Claude Code, Cursor, VS Code, or any other MCP-compatible client, sign in with your BoostedBudget
                account, and your agent can read your accounts, transactions, budgets, and portfolio directly — no
                exporting statements, no copy-pasting numbers into a chat window.
              </p>
              <p className="text-foreground">
                Every write — recategorizing a transaction, tagging it, saving a recipe — waits for you to confirm it
                first. Nothing in your account changes until you say yes.
              </p>
            </Section>

            <Section id="capabilities" title="What it can and can't do">
              <p className="text-foreground">
                MCP exposes {allowedOperations} capabilities: {readOperations} for reading your finances and{' '}
                {writeOperations} for changes that always wait for your confirmation.
              </p>
              {/*
                #1569 task 20 (A5). capabilitySummary.json has carried destructiveWriteOperations
                since task 16 and nothing read it, so a page whose subject is "what can an agent do
                to my money" could not say that some of what it advertises deletes permanently.

                The number is rendered, never written out in words, and no list of WHICH ones is
                given: both would drift the moment the manifest gains or loses a destructive entry,
                and the generator is the only thing that knows. `writeOperations` still means
                write-OR-destructive, so {allowedOperations} = {readOperations} + {writeOperations}
                is untouched and the sentence above stays true — the count below is a SUBSET of it,
                not a fourth bucket. Pinned by McpPage.test.tsx.
              */}
              <p className="text-foreground">
                {destructiveWriteOperations} of those changes permanently delete something rather than
                retaining it, and cannot be undone — your confirmation is the only thing standing in
                front of them.
              </p>
              <p className="text-foreground">
                Ask about spending, budgets, transactions, categories, savings goals, net worth, investments, and
                household data. You can also organize your finances and save repeatable reviews with your confirmation.
              </p>
              <ul className="grid gap-2 sm:grid-cols-2">
                {CAPABILITY_HIGHLIGHTS.map((highlight) => (
                  <li key={highlight} className="flex items-start gap-2 text-foreground">
                    <Check className="mt-0.5 h-4 w-4 shrink-0 text-primary" aria-hidden="true" />
                    {highlight}
                  </li>
                ))}
              </ul>

              <p className="text-foreground">Here&apos;s what that boundary looks like when an agent hits it:</p>
              <McpTranscript turns={TRANSCRIPT_HOUSEHOLD_REFUSAL} />

              <div>
                <h3 className="mb-2 text-base font-semibold text-foreground">Some actions stay in BoostedBudget</h3>
                <ul aria-label="Actions that stay in BoostedBudget" className="grid gap-3 sm:grid-cols-2">
                  {publicBoundaries.map((boundary) => (
                    <li key={boundary.id} className="rounded-lg border border-border bg-card p-4">
                      <h4 className="font-medium text-foreground">{boundary.title}</h4>
                      <p className="mt-1 text-muted-foreground">{boundary.description}</p>
                    </li>
                  ))}
                </ul>
              </div>
            </Section>

            <Section id="how-it-works" title="How it works">
              <p className="text-foreground">
                Your agent uses a few tools: <code>search</code> finds the right capability, <code>readExecute</code>{' '}
                carries out read-only requests, and <code>writeExecute</code> carries out anything that changes your
                data. You can ask in plain language, then review the answer or confirm a proposed change.
              </p>
              <p className="text-foreground">
                Saved recipes let you keep a repeatable financial review and run it again when you ask for it — see{' '}
                <a href="#recipes" className="text-primary underline underline-offset-2">
                  Saved recipes
                </a>{' '}
                below.
              </p>
            </Section>

            <Section id="connect" title="Connect your client">
              <p className="text-foreground">
                You need a BoostedBudget account. During your 60-day trial and while your subscription is active, your
                agent can read your data and make changes you confirm. If your subscription lapses, reading keeps
                working and changes are blocked — unless you&apos;re a covered member of an active household plan, in
                which case your agent can keep making confirmed changes too.
              </p>
              <McpSetupTabs />
            </Section>

            <Section id="write-safety" title="Every write waits for you">
              <p className="text-foreground">
                Every change goes through the same confirmation flow. The agent describes what would happen and asks you
                to approve it before anything in your account changes.
              </p>
              <p className="text-foreground">
                Confirmations are single-use and expire after 5 minutes. Each one is tied to your account and the exact
                request you reviewed, so it cannot be reused for something different.
              </p>
              <p className="text-foreground">
                Bank connections, household and sharing decisions, subscription changes, and account deletion stay in
                BoostedBudget. MCP does not move money; it can only record your transfer intent for you to complete with
                your bank.
              </p>
            </Section>

            <Section id="example-prompts" title="Example prompts">
              <ul className="space-y-4">
                <li className="rounded-lg border border-border bg-card p-4">
                  <p className="font-mono text-sm text-foreground">
                    &ldquo;What&apos;s my dining spend this month, and is it trending up?&rdquo;
                  </p>
                  <p className="mt-1 text-foreground">
                    The agent finds the right information and answers in one message — no dashboard required.
                  </p>
                </li>
                <li className="rounded-lg border border-border bg-card p-4">
                  <p className="font-mono text-sm text-foreground">&ldquo;Review my unreviewed transactions.&rdquo;</p>
                  <p className="mt-1 text-foreground">
                    The agent pulls your review inbox, flags anything it thinks looks miscategorized, and asks before
                    bulk-approving the rest.
                  </p>
                </li>
                <li className="rounded-lg border border-border bg-card p-4">
                  <p className="font-mono text-sm text-foreground">
                    &ldquo;What should I do with my extra cash this month?&rdquo;
                  </p>
                  <p className="mt-1 text-foreground">
                    The agent checks your budgets, savings goals, and any pending Boosts transfer suggestions, then
                    explains the options — it won&apos;t move anything without your confirmation.
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
                A recipe is a named, saved financial review — for example, &ldquo;Monthly Financial Review&rdquo; or
                &ldquo;Portfolio Review.&rdquo; Your agent can save it, show you what you&apos;ve saved, and run it
                again whenever you ask.
              </p>
              <p className="text-foreground">
                A recipe contains read-only analysis requests, but saving one still needs your confirmation because it
                changes what&apos;s stored on your account. You&apos;ll see the same confirm-before-it-happens flow
                described above.
              </p>
              <p className="text-foreground">
                Recipes run when you or your agent ask for them, not on a timer. A handful of starter reviews — monthly
                finances, spending, subscriptions, and portfolios — are built in.
              </p>
            </Section>

            <Section id="data-and-access" title="Data handling and access control">
              <p className="text-foreground">
                Every request is scoped to your BoostedBudget account and checked with the same permissions used in the
                app.
              </p>
              <p className="text-foreground">
                Your BoostedBudget credentials are injected server-side after you sign in — they&apos;re never visible
                to the agent. Calls, successful or not, are recorded in an audit log on a best-effort basis.
              </p>
              <p className="text-foreground">
                You can revoke access from your sign-in provider&apos;s security dashboard. It can take about 30–60
                seconds to take effect, and that timing is best effort.
              </p>
            </Section>

            <Section id="limits" title="Limits">
              <ul className="space-y-2 text-foreground">
                <li>
                  60 requests/minute and 5 concurrent calls, enforced per session, not per account — a second agent
                  session against the same account gets its own limit.
                </li>
                <li>
                  <code>readExecute</code>/<code>writeExecute</code> each accept at most 25 calls per invocation.
                </li>
                <li>
                  Each call&apos;s result is capped at about 100,000 characters, and a whole batch at about 300,000 —
                  ask for a small <code>limit</code> and page through results rather than requesting everything at
                  once. A truncated result says so, and tells you how much was left out.
                </li>
              </ul>
            </Section>

            <Section id="troubleshooting" title="If something doesn't connect">
              <ul className="space-y-3 text-foreground">
                <li>
                  <span className="font-medium">Boostedbudget doesn&apos;t appear after adding it: </span>
                  double-check you pasted the full URL, including the <code>https://</code> scheme, and left Advanced
                  settings empty so Dynamic Client Registration can run.
                </li>
                <li>
                  <span className="font-medium">Sign-in opens but nothing happens after: </span>
                  close the tab and retry — the first sign-in can take a few seconds.
                </li>
                <li>
                  <span className="font-medium">
                    <code>claude mcp list</code> shows <code>! Needs authentication</code>:{' '}
                  </span>
                  that&apos;s expected before you run <code>/mcp</code> → Authenticate — it is not an error.
                </li>
                <li>
                  <span className="font-medium">I revoked access but the agent can still call tools: </span>
                  revocation takes up to about 30–60 seconds to take effect (see{' '}
                  <a href="#data-and-access" className="text-primary underline underline-offset-2">
                    Data and access
                  </a>
                  ). If it&apos;s been longer, contact support.
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

            <Section id="agent-discovery" title="Discovery files for agents">
              <p className="text-foreground">
                The site also publishes <code>llms.txt</code> and <code>llms-full.txt</code> — plain-text,
                agent-readable documentation that summarizes what BoostedBudget does and where its public pages are.
                They are there so an agent can understand the product from a compact file instead of scraping rendered
                pages.
              </p>
              <p className="text-foreground">
                To be explicit: these are agent-readable documentation, not an AI-search-ranking tactic. Publishing
                them does not change how search engines rank or cite BoostedBudget.
              </p>
              <p className="text-foreground">
                For the human-readable version, see the{' '}
                <Link to="/features" className="text-primary underline underline-offset-2">
                  Features
                </Link>{' '}
                page or read the{' '}
                <Link to="/faq" className="text-primary underline underline-offset-2">
                  FAQ
                </Link>
                .
              </p>
            </Section>
          </div>
        </div>
      </div>
    </div>
  )
}
