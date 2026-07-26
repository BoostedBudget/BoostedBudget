/**
 * McpTranscript — illustrative agent-session transcript (Issue #1411).
 *
 * Used twice on the /mcp page: the hero's subscription-audit transcript and
 * §2's household-invite refusal transcript. Per task-P8-brief.md § "Transcripts":
 * - Always inside `<figure data-illustrative="true">` with a "Sample data" chip.
 * - Fixed figcaption; an extra sentence is appended only when the transcript
 *   shows write-confirmation status output (real tool output uses ✅/⚠️/❌ —
 *   this component never renders those; it uses →, ⟳, ✕ instead).
 * - Speaker labels are real text nodes (never CSS ::before, which would be
 *   invisible to assistive tech and to any text-based verification).
 */

import { cn } from '@/lib/utils'

export interface McpTranscriptTurn {
  speaker: 'You' | 'Agent'
  lines: string[]
}

export interface McpTranscriptProps {
  turns: McpTranscriptTurn[]
  /** True when this transcript shows write-confirmation status output (⟳ / → / ✕). */
  showsStatusMarkers?: boolean
  className?: string
}

const BASE_FIGCAPTION =
  'Illustrative transcript. The tool names, operation IDs, and confirmation flow are exactly what the server does. The account names and dollar figures are synthetic sample data, not customer data.'

export function McpTranscript({ turns, showsStatusMarkers = false, className }: McpTranscriptProps) {
  const figcaption = showsStatusMarkers
    ? `${BASE_FIGCAPTION} Status markers are simplified for readability.`
    : BASE_FIGCAPTION

  return (
    <figure
      data-illustrative="true"
      className={cn('rounded-2xl border border-border bg-card p-6', className)}
    >
      <div className="flex items-center justify-between">
        <span className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
          Agent session
        </span>
        <span className="rounded-full bg-muted px-2.5 py-1 text-xs font-medium text-foreground">
          Sample data
        </span>
      </div>

      <div
        className="mt-6 space-y-4 overflow-x-auto text-sm"
        tabIndex={0}
        role="region"
        aria-label="Illustrative agent transcript"
      >
        {turns.map((turn, i) => (
          <div key={i} className="flex gap-3">
            <span
              className={cn(
                'w-14 shrink-0 font-semibold',
                turn.speaker === 'You' ? 'text-foreground' : 'text-primary'
              )}
            >
              {turn.speaker}
            </span>
            <div className="min-w-0 flex-1 space-y-1 font-mono text-foreground">
              {turn.lines.map((line, j) => (
                <p key={j} className="m-0 whitespace-pre-wrap break-words">
                  {line}
                </p>
              ))}
            </div>
          </div>
        ))}
      </div>

      <figcaption className="mt-6 text-xs text-muted-foreground">{figcaption}</figcaption>
    </figure>
  )
}
