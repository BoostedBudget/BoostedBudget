/**
 * Parses `dead_letter_jobs.errors` — an existing JSON column served to the client as `unknown`.
 *
 * ⚠ BACKWARD COMPATIBILITY IS THE POINT OF THIS MODULE. The column holds production rows written
 * in the old `{attempt, message, timestamp}` shape, alongside rows written after #1631 that also
 * carry `chain` and `pg_error`. Every field here is optional and absent-tolerant: an old row
 * parses into an entry whose `chain` is `[]` and whose `pg` is `undefined`, and a malformed or
 * absent column yields `[]` rather than an exception. Nothing in the render path may assume a
 * field exists.
 *
 * ⚠ PII. `pg.message`, `pg.detail` and `pg.hint`, and every `chain` entry, can quote ROW VALUES:
 * a 23505 Detail reads `Key (email)=(someone@example.com) already exists.` for a unique-email
 * constraint. Anything rendering those MUST carry `data-ph-mask`, and they must never reach an
 * `aria-label`/`title`/`alt`/`placeholder` — session replay records attributes verbatim and
 * cannot mask them. `code`, `constraint_name`, `table_name`, `schema_name`, `column_name`,
 * `severity` and `routine` are schema identifiers, not row data, and stay legible.
 */

export interface PgErrorContext {
  code?: string
  severity?: string
  message?: string
  detail?: string
  hint?: string
  constraint_name?: string
  table_name?: string
  schema_name?: string
  column_name?: string
  routine?: string
}

export interface ParsedJobError {
  /** Always present, possibly '' — old rows always have it, but nothing guarantees it. */
  message: string
  attempt?: number
  timestamp?: string
  /** `[]` for every row written before #1631. */
  chain: string[]
  /** `undefined` unless a *pgconn.PgError was found in the chain. */
  pg?: PgErrorContext
}

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === 'object' && value !== null && !Array.isArray(value)

const optionalString = (value: unknown): string | undefined =>
  typeof value === 'string' && value.length > 0 ? value : undefined

const PG_STRING_KEYS = [
  'code',
  'severity',
  'message',
  'detail',
  'hint',
  'constraint_name',
  'table_name',
  'schema_name',
  'column_name',
  'routine',
] as const satisfies readonly (keyof PgErrorContext)[]

const parsePgContext = (value: unknown): PgErrorContext | undefined => {
  if (!isRecord(value)) return undefined
  const out: PgErrorContext = {}
  let any = false
  for (const key of PG_STRING_KEYS) {
    const parsed = optionalString(value[key])
    if (parsed !== undefined) {
      out[key] = parsed
      any = true
    }
  }
  return any ? out : undefined
}

/** Non-string members are dropped rather than stringified into "null"/"5". */
const parseChain = (value: unknown): string[] =>
  Array.isArray(value) ? value.filter((entry): entry is string => typeof entry === 'string') : []

export function parseJobErrors(raw: unknown): ParsedJobError[] {
  let value = raw
  if (typeof value === 'string') {
    try {
      value = JSON.parse(value)
    } catch {
      return []
    }
  }
  if (!Array.isArray(value)) return []

  return value.filter(isRecord).map((entry) => ({
    message: typeof entry.message === 'string' ? entry.message : '',
    attempt: typeof entry.attempt === 'number' ? entry.attempt : undefined,
    timestamp: optionalString(entry.timestamp),
    chain: parseChain(entry.chain),
    pg: parsePgContext(entry.pg_error),
  }))
}

export interface PgErrorSummary {
  code?: string
  constraintName?: string
}

/**
 * The at-a-glance triage summary for a whole job: SQLSTATE and constraint name from the LAST
 * attempt that carried Postgres context.
 *
 * ⚠ It deliberately returns ONLY those two fields. Both are schema identifiers, so this summary
 * is safe to render unmasked where an operator needs to scan a queue — which is the whole reason
 * `pg.message` is excluded despite being tempting: for e.g. a 22P02 the message quotes the
 * offending value (`invalid input syntax for type uuid: "..."`).
 */
export function pgErrorSummary(errors: ParsedJobError[]): PgErrorSummary | undefined {
  for (let i = errors.length - 1; i >= 0; i -= 1) {
    const pg = errors[i].pg
    if (pg && (pg.code !== undefined || pg.constraint_name !== undefined)) {
      return { code: pg.code, constraintName: pg.constraint_name }
    }
  }
  return undefined
}
