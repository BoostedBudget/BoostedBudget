import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'
import { parseJobErrors, type ParsedJobError, type PgErrorContext } from '../lib/parseJobErrors'

/**
 * Renders `dead_letter_jobs.errors` as structured, progressively-disclosed error context (#1631).
 *
 * The motivating case: a holdings sync dead-lettered five times and the admin saw ONE flattened
 * line, while Postgres had already sent the Detail naming the exact colliding row. Inline, an
 * operator now sees the SQLSTATE and the constraint — enough to tell one failing job from
 * another while scanning a queue. Behind the expander sits the wrapped error chain (so the
 * boundary that failed is legible instead of concatenated) and everything else Postgres sent.
 *
 * ⚠ TWO invariants, both enforced by JobErrorList.test.tsx:
 *  1. BACKWARD COMPATIBILITY. Rows written before #1631 carry only {attempt, message, timestamp}.
 *     They render as a plain masked message with no expander and no invented SQLSTATE. Absent
 *     fields are the normal case, not an error case.
 *  2. PII. `pg.message`, `pg.detail`, `pg.hint` and every chain layer can quote ROW VALUES — a
 *     unique-email violation prints `Key (email)=(someone@example.com) already exists.` Each is
 *     wrapped in `data-ph-mask`, and NONE of them may be interpolated into an `aria-label`,
 *     `title`, `alt` or `placeholder`: session replay records attributes verbatim and there is no
 *     attribute-masking mechanism to reach for. The SQLSTATE, constraint, table, schema, column,
 *     severity and routine are schema identifiers rather than row data, so they stay legible —
 *     masking them would delete the signal this view exists to provide.
 */

const preClass =
  'bg-muted border border-border rounded-md px-3 py-2 text-[0.8125rem] font-mono whitespace-pre-wrap break-words max-h-[200px] overflow-y-auto'
const labelClass = 'text-[0.6875rem] font-semibold uppercase tracking-wide text-muted-foreground'
const metaClass = 'text-[0.8125rem] text-muted-foreground'

interface JobErrorListProps {
  /** The raw `errors` column, served as `unknown`. Absent/malformed values are tolerated. */
  errors: unknown
  className?: string
}

/** A schema identifier — safe to render unmasked, unlike anything carrying row values. */
function Identifier({ children, testId }: { children: string; testId?: string }) {
  return (
    <code className="font-mono text-[0.75rem] text-foreground bg-muted rounded px-1.5 py-0.5" data-testid={testId}>
      {children}
    </code>
  )
}

/** A SQL free-text field. Always masked, never placed in an attribute. */
function MaskedField({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex flex-col gap-1">
      <span className={labelClass}>{label}</span>
      <pre className={preClass} data-ph-mask>
        {value}
      </pre>
    </div>
  )
}

function qualifiedTable(pg: PgErrorContext): string | undefined {
  if (!pg.table_name) return undefined
  return pg.schema_name ? `${pg.schema_name}.${pg.table_name}` : pg.table_name
}

function ErrorEntry({ entry, index }: { entry: ParsedJobError; index: number }) {
  const { pg, chain } = entry
  const hasStructure = !!pg || chain.length > 0
  const attemptLabel = entry.attempt === undefined ? `Error ${index + 1}` : `Attempt ${entry.attempt}`
  const timestamp = entry.timestamp ? new Date(entry.timestamp) : undefined
  const timestampLabel =
    timestamp && !Number.isNaN(timestamp.getTime()) ? timestamp.toLocaleString() : undefined

  const header = (
    <>
      <span className="text-[0.8125rem] font-semibold text-foreground">{attemptLabel}</span>
      {timestampLabel && <span className={metaClass}>{timestampLabel}</span>}
      {pg?.code && (
        <Badge variant="destructive" data-testid="job-error-sqlstate">
          {pg.code}
        </Badge>
      )}
      {pg?.constraint_name && <Identifier testId="job-error-constraint">{pg.constraint_name}</Identifier>}
    </>
  )

  // An old-shape row: nothing structured to disclose, so no expander is offered and the message
  // stays exactly as legible as it was before this component existed.
  if (!hasStructure) {
    return (
      <li className="flex flex-col gap-2 border border-border rounded-md p-3">
        <div className="flex flex-wrap items-center gap-2">{header}</div>
        <pre className={cn(preClass, 'text-destructive')} data-ph-mask>
          {entry.message}
        </pre>
      </li>
    )
  }

  const tableLabel = pg ? qualifiedTable(pg) : undefined

  return (
    <li className="border border-border rounded-md p-3">
      <details data-testid="job-error-expander">
        <summary className="flex flex-wrap items-center gap-2 cursor-pointer list-none [&::-webkit-details-marker]:hidden">
          {header}
          {tableLabel && <Identifier>{tableLabel}</Identifier>}
          <span className="ml-auto text-xs text-primary hover:underline font-medium">Error detail</span>
        </summary>

        <div className="flex flex-col gap-3 mt-3">
          {chain.length > 0 && (
            <div className="flex flex-col gap-1">
              <span className={labelClass}>Error chain (outermost first)</span>
              <ol className="flex flex-col gap-1 m-0 pl-0 list-none">
                {chain.map((layer, layerIndex) => (
                  <li
                    key={`${layerIndex}-${layer}`}
                    className="text-[0.8125rem] font-mono break-words text-foreground"
                    style={{ paddingLeft: `${layerIndex * 0.75}rem` }}
                    data-testid="job-error-chain-layer"
                    data-ph-mask
                  >
                    {layer}
                  </li>
                ))}
              </ol>
            </div>
          )}

          {pg && (
            <>
              {(pg.severity || pg.column_name || pg.routine) && (
                <div className="flex flex-wrap items-center gap-2">
                  {pg.severity && <Identifier>{pg.severity}</Identifier>}
                  {pg.column_name && <Identifier>{pg.column_name}</Identifier>}
                  {pg.routine && <Identifier>{pg.routine}</Identifier>}
                </div>
              )}
              {pg.message && <MaskedField label="Postgres message" value={pg.message} />}
              {pg.detail && <MaskedField label="Postgres detail" value={pg.detail} />}
              {pg.hint && <MaskedField label="Postgres hint" value={pg.hint} />}
            </>
          )}

          {entry.message && <MaskedField label="Raw error" value={entry.message} />}
        </div>
      </details>
    </li>
  )
}

export function JobErrorList({ errors, className }: JobErrorListProps) {
  const parsed = parseJobErrors(errors)

  if (parsed.length === 0) {
    return <p className={cn('text-sm text-muted-foreground', className)}>(none)</p>
  }

  return (
    <ol className={cn('flex flex-col gap-2 m-0 p-0 list-none', className)}>
      {parsed.map((entry, index) => (
        <ErrorEntry key={`${entry.attempt ?? index}-${entry.timestamp ?? ''}`} entry={entry} index={index} />
      ))}
    </ol>
  )
}
