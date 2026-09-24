/**
 * McpBand Component
 *
 * Full-width landing band introducing the remote MCP server — the product's
 * actual differentiator — and linking to the public /mcp page (built by
 * another #1411 task). Inserted between FeaturesSection and SocialProof.
 *
 * Issue #1411: truth-up landing copy + public MCP marketing page.
 */

import { Link } from 'react-router'
import { ArrowRight } from 'lucide-react'
import { useTrackEvent } from '../../../shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '../../../shared/analytics'

export function McpBand() {
  const trackEvent = useTrackEvent()

  const handleLinkClick = () => {
    trackEvent(ANALYTICS_EVENTS.MCP_BAND_CTA_CLICKED)
  }

  return (
    <section
      className="mcp-band bg-slate-50 py-24 dark:bg-slate-800/50 sm:py-32"
      aria-labelledby="mcp-band-heading"
    >
      <div className="mx-auto max-w-7xl px-4 sm:px-6 lg:px-8">
        <div className="grid gap-12 lg:grid-cols-2 lg:items-center lg:gap-16">
          <div>
            <h2
              id="mcp-band-heading"
              className="text-3xl font-bold tracking-tight text-slate-900 dark:text-white sm:text-4xl lg:text-5xl"
            >
              Connect It to Your Agent
            </h2>
            <p className="mt-6 text-lg leading-relaxed text-slate-600 dark:text-slate-400">
              BoostedBudget runs a remote MCP server. Point Claude, Cursor, VS Code, or any
              MCP-compatible client at one URL, sign in once, and your agent can read across
              your accounts, transactions, budgets, and portfolio — changing things only when
              you confirm each one.
            </p>
            <div className="mt-8">
              <Link
                to="/mcp"
                onClick={handleLinkClick}
                className="inline-flex items-center gap-2 text-lg font-semibold text-blue-600 hover:text-blue-500 dark:text-blue-400 dark:hover:text-blue-300"
              >
                Explore the MCP server
                <ArrowRight className="h-4 w-4" aria-hidden="true" />
              </Link>
            </div>
          </div>

          <figure
            data-illustrative="true"
            className="rounded-2xl bg-white p-6 shadow-sm dark:bg-slate-900 lg:p-8"
          >
            <div className="flex items-center justify-between">
              <span className="text-xs font-semibold uppercase tracking-wide text-slate-500 dark:text-slate-400">
                Agent session
              </span>
              <span className="rounded-full bg-slate-100 px-2.5 py-1 text-xs font-medium text-slate-600 dark:bg-slate-700 dark:text-slate-300">
                Sample data
              </span>
            </div>

            <div
              className="mt-6 overflow-x-auto"
              tabIndex={0}
              role="region"
              aria-label="Illustrative agent transcript"
            >
              <div className="space-y-4 text-sm">
                <div className="flex gap-3">
                  <span className="w-14 shrink-0 font-semibold text-slate-900 dark:text-white">
                    You
                  </span>
                  <span className="min-w-0 break-words font-mono text-slate-700 dark:text-slate-300">
                    Where did my money go last month?
                  </span>
                </div>

                <div className="flex gap-3">
                  <span className="w-14 shrink-0 font-semibold text-blue-600 dark:text-blue-400">
                    Agent
                  </span>
                  <span className="min-w-0 break-words font-mono text-slate-700 dark:text-slate-300">
                    search({'{'} query: "spending by category" {'}'})
                    <br />→ getSpendingByCategory
                  </span>
                </div>

                <div className="flex gap-3">
                  <span className="w-14 shrink-0 font-semibold text-blue-600 dark:text-blue-400">
                    Agent
                  </span>
                  <span className="min-w-0 break-words font-mono text-slate-700 dark:text-slate-300">
                    Groceries $612 · Dining $388 · Transport $214
                    <br />
                    Dining is up 34% month over month.
                  </span>
                </div>
              </div>
            </div>

            <figcaption className="mt-6 text-xs text-slate-500 dark:text-slate-400">
              Illustrative transcript. Real tool calls, synthetic figures.
            </figcaption>
          </figure>
        </div>
      </div>
    </section>
  )
}
