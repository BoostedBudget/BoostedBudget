import type { ExecuteCall } from '../mcp/tools/execute'
import type { SpecEntry } from '../spec/search-runtime'

export type ToolSurface =
  | 'search'
  | 'read_execute'
  | 'write_execute'
  | 'recipe_list'
  | 'recipe_save'
  | 'recipe_run'
  | 'recipe_templates'

export type ToolFamily =
  | 'accounts'
  | 'analytics'
  | 'budgets'
  | 'boosts'
  | 'savings'
  | 'transactions'
  | 'discovery'
  | 'workflows'
  | 'cross_domain'
  | 'other'

export type DurationBucket = 'under_250ms' | '250ms_to_1s' | '1s_to_5s' | 'over_5s'

export interface MCPToolCompletion {
  tool_surface: ToolSurface
  tool_family: ToolFamily
  outcome: 'succeeded' | 'failed'
  duration_bucket: DurationBucket
}

export interface MCPToolTelemetry {
  record(completion: MCPToolCompletion): Promise<void>
}

type Scheduler = (promise: Promise<void>) => void
type OutcomeReporter = (outcome: MCPToolCompletion['outcome']) => void

interface InstrumentToolOptions<Input> {
  surface: ToolSurface
  family: (input: Input) => ToolFamily
  telemetry: MCPToolTelemetry
  schedule: Scheduler
  now?: () => number
}

export function durationBucket(milliseconds: number): DurationBucket {
  if (milliseconds < 250) return 'under_250ms'
  if (milliseconds < 1_000) return '250ms_to_1s'
  if (milliseconds < 5_000) return '1s_to_5s'
  return 'over_5s'
}

const TAG_FAMILIES: Readonly<Record<string, ToolFamily>> = {
  accounts: 'accounts',
  'user accounts': 'accounts',
  household: 'accounts',
  analytics: 'analytics',
  dashboard: 'analytics',
  investments: 'analytics',
  budget: 'budgets',
  budgets: 'budgets',
  boosts: 'boosts',
  contributions: 'savings',
  savings: 'savings',
  'savings goals': 'savings',
  savingsimpact: 'savings',
  transfersuggestions: 'savings',
  categories: 'transactions',
  'auto-categorization': 'transactions',
  merchants: 'transactions',
  tags: 'transactions',
  transactions: 'transactions',
  'mcp recipes': 'workflows',
}

function familyForEntry(entry: SpecEntry | undefined): ToolFamily {
  if (!entry) return 'other'
  for (const tag of entry.tags) {
    const family = TAG_FAMILIES[tag.toLowerCase()]
    if (family) return family
  }
  return 'other'
}

export function classifyExecuteFamily(
  calls: Pick<ExecuteCall, 'operationId'>[],
  entries: SpecEntry[],
): ToolFamily {
  const entriesByOperation = new Map(entries.map(entry => [entry.operationId, entry]))
  const families = new Set(calls.map(call => familyForEntry(entriesByOperation.get(call.operationId))))
  if (families.size === 0) return 'other'
  if (families.size > 1) return 'cross_domain'
  return families.values().next().value ?? 'other'
}

export function instrumentTool<Input, Output>(
  options: InstrumentToolOptions<Input>,
  handler: (input: Input, reportOutcome: OutcomeReporter) => Promise<Output>,
): (input: Input) => Promise<Output> {
  const now = options.now ?? (() => performance.now())
  return async function instrumentedTool(input: Input): Promise<Output> {
    let startedAt: number | undefined
    try {
      startedAt = now()
    } catch {
      // A runtime clock fault must not affect tool semantics.
    }
    let outcome: MCPToolCompletion['outcome'] = 'succeeded'
    try {
      const result = await handler(input, reported => { outcome = reported })
      if (result && typeof result === 'object' && 'isError' in result && result.isError === true) {
        outcome = 'failed'
      }
      return result
    } catch (error) {
      outcome = 'failed'
      throw error
    } finally {
      let elapsed = 0
      try {
        const endedAt = now()
        if (startedAt !== undefined && Number.isFinite(startedAt) && Number.isFinite(endedAt)) {
          elapsed = Math.max(0, endedAt - startedAt)
        }
      } catch {
        // Fall back to the shortest bounded bucket if the runtime clock is unavailable.
      }
      let toolFamily: ToolFamily = 'other'
      try {
        toolFamily = options.family(input)
      } catch {
        // A manifest-classification fault is operational; never leak its details or fail the tool.
      }
      const completion: MCPToolCompletion = {
        tool_surface: options.surface,
        tool_family: toolFamily,
        outcome,
        duration_bucket: durationBucket(elapsed),
      }
      try {
        options.schedule(options.telemetry.record(completion).catch(() => undefined))
      } catch {
        // Observability must never change the tool result, including a synchronous runtime
        // failure before waitUntil accepts the best-effort delivery promise.
      }
    }
  }
}
