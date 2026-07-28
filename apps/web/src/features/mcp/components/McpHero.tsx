/**
 * McpHero — Persuade-opening hero for the public /mcp page (Issue #1411).
 *
 * Copy is verbatim per .superpowers/sdd/task-P8-brief.md § "Hero copy — verbatim".
 * The capability count is passed in (generated from the manifest via
 * apps/mcp-server/scripts/generate-mcp-capability-summary.ts) rather than
 * hand-typed, so this line never drifts from the real capability manifest.
 */

import { ArrowRight, Shield } from 'lucide-react'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics'

export interface McpHeroProps {
  /** Generated count of public MCP capabilities — never hand-typed. */
  capabilityCount: number
}

export function McpHero({ capabilityCount }: McpHeroProps) {
  const trackEvent = useTrackEvent()

  const handlePrimaryCtaClick = () => {
    trackEvent(ANALYTICS_EVENTS.MCP_PAGE_CTA_CLICKED)
  }

  return (
    <div className="text-center">
      <h1 className="text-4xl font-bold tracking-tight text-foreground sm:text-5xl">
        Your finances, in the agent you already use.
      </h1>

      <p className="mx-auto mt-6 max-w-2xl text-lg text-muted-foreground">
        BoostedBudget runs a remote MCP server. Point Claude or VS Code at one URL, sign in once, and your agent can
        read across your accounts, transactions, budgets, and portfolio — and change things only after you confirm each
        change.
      </p>

      <div className="mt-8 flex flex-col items-center justify-center gap-4 sm:flex-row">
        <a
          href="#connect"
          onClick={handlePrimaryCtaClick}
          className="inline-flex items-center gap-2 rounded-lg bg-primary px-6 py-3 text-base font-semibold text-primary-foreground transition-opacity hover:opacity-90"
        >
          Connect your client
          <ArrowRight className="h-4 w-4" aria-hidden="true" />
        </a>
        <a
          href="#capabilities"
          className="inline-flex items-center gap-2 text-base font-semibold text-foreground hover:text-primary"
        >
          See exactly what it can do
        </a>
      </div>

      <p className="mt-4 text-sm text-muted-foreground">You paste a URL. There is no API key and no token to manage.</p>

      <ul className="mt-10 flex flex-wrap items-center justify-center gap-3">
        <li className="rounded-full border border-border bg-muted px-4 py-1.5 text-sm font-medium text-foreground">
          {capabilityCount} capabilities
        </li>
        <li className="rounded-full border border-border bg-muted px-4 py-1.5 text-sm font-medium text-foreground">
          Every write confirmed
        </li>
        <li className="rounded-full border border-border bg-muted px-4 py-1.5 text-sm font-medium text-foreground">
          No API key
        </li>
      </ul>

      <p className="mx-auto mt-10 flex max-w-2xl items-center justify-center gap-2 text-sm text-muted-foreground">
        <Shield className="h-4 w-4 shrink-0" aria-hidden="true" />
        Your account permissions apply to every request. Credentials are never shown to the agent. Calls are recorded in
        an audit log on a best-effort basis.
      </p>
    </div>
  )
}
