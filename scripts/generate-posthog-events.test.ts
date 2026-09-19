import { describe, expect, test } from 'bun:test'
import { mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

import { parseDocument } from 'yaml'

import { PII_PROPERTY_DENYLIST } from '../apps/web/src/shared/lib/piiScrubbing'

import {
  findRawCaptureEventLiterals,
  parseEventCatalogYaml,
  parseProductMetricsYaml,
  renderGeneratedArtifacts,
  validateEventCatalog,
  validateProductMetrics,
  verifyGeneratedArtifacts,
} from './generate-posthog-events'

const CAPTURE_SOURCE_ROOTS = ['apps/web/src', 'apps/mcp-server/src', 'backend/internal', 'backend/cmd']
const CAPTURE_SOURCE_EXTENSIONS = ['.go', '.ts', '.tsx']

function productionCaptureSources(root: string): string[] {
  const visit = (relativeDirectory: string): string[] => readdirSync(join(root, relativeDirectory), {
    withFileTypes: true,
  }).flatMap((entry) => {
    const relativePath = join(relativeDirectory, entry.name)
    if (entry.isDirectory()) return visit(relativePath)
    if (!entry.isFile()) return []
    if (!CAPTURE_SOURCE_EXTENSIONS.some((extension) => entry.name.endsWith(extension))) return []
    if (/\.test\.|_test\.go$|\.generated\.ts$|events_generated\.go$/.test(entry.name)) return []
    return [relativePath]
  })

  return CAPTURE_SOURCE_ROOTS.flatMap(visit).sort()
}

const activeEvent = () => ({
  contract_id: 'frontend_account_link_completed',
  name: 'account_link_completed',
  kind: 'custom',
  symbols: { typescript: ['ACCOUNT_LINK_COMPLETED'], go: [] },
  producers: ['frontend'],
  subject: 'actor_user',
  consent: 'analytics_required',
  privacy: 'non_sensitive',
  status: 'active',
  introduced: '#1650',
  properties: {
    method: {
      type: 'string',
      required: true,
      cardinality: 'bounded',
      values: ['plaid'],
    },
  },
})

const validEventCatalog = () => ({
  schema_version: 2,
  events: [
    activeEvent(),
    {
      contract_id: 'backend_sync_completed',
      kind: 'custom',
      name: 'sync_completed',
      symbols: { typescript: [], go: ['EventSyncCompleted'] },
      producers: ['backend'],
      subject: 'actor_user',
      consent: 'analytics_required',
      privacy: 'non_sensitive',
      status: 'legacy',
      introduced: '#1',
      properties: {},
      legacy_exceptions: ['property_schema_unreviewed'],
    },
  ],
})

const validMetrics = () => ({
  schema_version: 1,
  metrics: [
    {
      id: 'observed_activation',
      question: 'Do linked users observe value?',
      formula: 'Activated eligible users / eligible linked users',
      unit: 'proportion',
      cohort_start: 'First account_link_completed per user.',
      denominator: 'Consenting users with account_link_completed.',
      exclusions: ['Internal and test users.'],
      window: 'P7D',
      timezone: 'UTC',
      consent_caveat: 'Measures only users who consent to product analytics.',
      owner: 'Product',
      review_cadence: 'monthly',
      decision_supported: 'Prioritize activation improvements.',
      availability: 'planned',
      event_dependencies: ['account_link_completed'],
      future_event_dependencies: [],
    },
  ],
})

describe('event catalog validation', () => {
  test('accepts a complete schema version 2 catalog', () => {
    expect(validateEventCatalog(validEventCatalog()).events).toHaveLength(2)
  })

  test.each([
    ['invalid snake_case event names', (catalog: any) => (catalog.events[0].name = 'AccountLinked')],
    [
      'duplicate active event names',
      (catalog: any) => catalog.events.push({ ...activeEvent(), contract_id: 'backend_duplicate' }),
    ],
    [
      'duplicate TypeScript symbols',
      (catalog: any) => (catalog.events[1].symbols.typescript = ['ACCOUNT_LINK_COMPLETED']),
    ],
    ['unknown producers', (catalog: any) => (catalog.events[0].producers = ['browser_extension'])],
    ['unknown status values', (catalog: any) => (catalog.events[0].status = 'experimental')],
    ['unknown privacy values', (catalog: any) => (catalog.events[0].privacy = 'probably_safe')],
    ['unknown consent values', (catalog: any) => (catalog.events[0].consent = 'implied')],
    [
      'unknown cardinality values',
      (catalog: any) => (catalog.events[0].properties.method.cardinality = 'unlimited'),
    ],
    [
      'numeric bounds on string properties',
      (catalog: any) => (catalog.events[0].properties.method.minimum = 1),
    ],
    [
      'numeric bounds on boolean properties',
      (catalog: any) => {
        catalog.events[0].properties.method = {
          type: 'boolean', required: true, cardinality: 'bounded', minimum: 0,
        }
      },
    ],
    [
      'sensitive property keys in legacy definitions',
      (catalog: any) => (catalog.events[1].properties = { account_number: { type: 'string' } }),
    ],
    ...[
      ...PII_PROPERTY_DENYLIST,
      'institutionName',
      'linkSessionId',
    ].map((key) => [
      `PII denylist key ${key}`,
      (catalog: any) => {
        catalog.events[0].properties = {
          [key]: { type: 'boolean', required: true, cardinality: 'bounded' },
        }
      },
    ] as const),
    ...[
      'accountNumberLast4',
      'categoryName',
      'contributionIdHash',
      'displayName',
      'goalIdHash',
      'jobIdHash',
      'tokenCount',
      'transactionIdHash',
      'totalValue',
    ].map((key) => [
      `catalog-sensitive camelCase key ${key}`,
      (catalog: any) => {
        catalog.events[0].properties = {
          [key]: { type: 'boolean', required: true, cardinality: 'bounded' },
        }
      },
    ] as const),
    [
      'unbounded active string properties',
      (catalog: any) => delete catalog.events[0].properties.method.values,
    ],
    ['unknown event fields', (catalog: any) => (catalog.events[0].surprise = true)],
    ['future events without cutover metadata', (catalog: any) => (catalog.events[0].status = 'future')],
    [
      'deprecated events without deprecation metadata',
      (catalog: any) => (catalog.events[0].status = 'deprecated'),
    ],
  ])('rejects %s', (_name, mutate) => {
    const catalog = validEventCatalog()
    mutate(catalog)
    expect(() => validateEventCatalog(catalog)).toThrow()
  })

  test('represents a documented cross-layer collision in one globally unique legacy definition', () => {
    const catalog = validEventCatalog()
    const shared = {
      ...activeEvent(),
      contract_id: 'legacy_shared_name',
      name: 'shared_name',
      symbols: { typescript: ['SHARED_NAME'], go: ['EventSharedName'] },
      producers: ['frontend', 'backend'],
      subject: 'mixed_legacy_subjects',
      status: 'legacy',
      properties: {},
      legacy_exceptions: ['cross_layer_name_collision', 'property_schema_unreviewed'],
      ownership: [
        { producer: 'frontend', subject: 'actor_user' },
        { producer: 'backend', subject: 'target_user' },
      ],
      metric_owner: 'none_until_cutover',
    }
    catalog.events.push(shared)

    expect(validateEventCatalog(catalog).events).toHaveLength(3)
    catalog.events.push({ ...activeEvent(), contract_id: 'duplicate_shared_name', name: 'shared_name' })
    expect(() => validateEventCatalog(catalog)).toThrow(/duplicate event name/i)
  })

  test('rejects multiple producers unless collision ownership and a canonical metric owner are explicit', () => {
    const catalog = validEventCatalog()
    catalog.events[0].producers = ['frontend', 'backend']

    expect(() => validateEventCatalog(catalog)).toThrow(/multiple producers|collision/i)
  })

  test('rejects a TypeScript symbol without a TypeScript producer', () => {
    const catalog = validEventCatalog()
    catalog.events[0].producers = ['backend']

    expect(() => validateEventCatalog(catalog)).toThrow(/TypeScript.*producer/i)
  })

  test('rejects a Go symbol without the backend producer', () => {
    const catalog = validEventCatalog()
    catalog.events[0].symbols.go = ['EventAccountLinkCompleted']

    expect(() => validateEventCatalog(catalog)).toThrow(/Go.*producer/i)
  })

  test('rejects ambiguous frontend and MCP TypeScript symbol ownership', () => {
    const catalog = validEventCatalog()
    catalog.events[0].producers = ['frontend', 'mcp_worker']

    expect(() => validateEventCatalog(catalog)).toThrow(/ambiguous TypeScript.*producer/i)
  })

  test('rejects a canonical metric owner that is not one of the event producers', () => {
    const catalog = validEventCatalog()
    const event = catalog.events[1] as any
    event.producers = ['frontend', 'backend']
    event.subject = 'mixed_legacy_subjects'
    event.legacy_exceptions.push('cross_layer_name_collision')
    event.ownership = [
      { producer: 'frontend', subject: 'actor_user' },
      { producer: 'backend', subject: 'actor_user' },
    ]
    event.metric_owner = 'mcp_worker'

    expect(() => validateEventCatalog(catalog)).toThrow(/metric.owner|canonical/i)
  })

  test('accepts PostHog-reserved names only for explicit vendor-reserved definitions', () => {
    const catalog = validEventCatalog()
    catalog.events.push({
      ...activeEvent(),
      contract_id: 'vendor_posthog_pageview',
      name: '$pageview',
      kind: 'vendor_reserved',
      symbols: { typescript: ['POSTHOG_PAGEVIEW'], go: [] },
      status: 'legacy',
      properties: {},
      legacy_exceptions: ['property_schema_unreviewed'],
    } as any)

    expect(validateEventCatalog(catalog).events).toHaveLength(3)
    ;(catalog.events[2] as any).kind = 'custom'
    expect(() => validateEventCatalog(catalog)).toThrow(/reserved|custom/i)
  })

  test('accepts vendor names with exactly one or two dollar prefixes, never more', () => {
    const catalog = validEventCatalog()
    catalog.events.push({
      ...activeEvent(),
      contract_id: 'vendor_posthog_heatmap',
      name: '$$heatmap',
      kind: 'vendor_reserved',
      symbols: { typescript: [], go: [] },
      properties: {},
    } as any)
    expect(validateEventCatalog(catalog).events).toHaveLength(3)

    const triple = validEventCatalog()
    triple.events.push({
      ...activeEvent(),
      contract_id: 'vendor_posthog_bad',
      name: '$$$bad',
      kind: 'vendor_reserved',
      symbols: { typescript: [], go: [] },
      properties: {},
    } as any)
    expect(() => validateEventCatalog(triple)).toThrow(/vendor-reserved/i)
  })

  test('requires every string property to be bounded unless legacy explicitly documents the exception', () => {
    const catalog = validEventCatalog()
    const legacy = catalog.events[1] as any
    legacy.properties = {
      account_type: { type: 'string', required: true, cardinality: 'high' },
    }

    expect(() => validateEventCatalog(catalog)).toThrow(/unbounded.*legacy|unbounded_string/i)
    legacy.legacy_exceptions.push('unbounded_string')
    expect(validateEventCatalog(catalog).events).toHaveLength(2)
  })

  test('rejects an unnecessary legacy unbounded_string exception', () => {
    const catalog = validEventCatalog()
    catalog.events[1].legacy_exceptions.push('unbounded_string')

    expect(() => validateEventCatalog(catalog)).toThrow(/unnecessary.*unbounded_string/i)
  })

  test.each([
    ['duplicate', ['known', 'known']],
    ['empty', ['']],
    ['non-string', [1]],
  ])('rejects %s bounded string values even with a legacy exception', (_name, values) => {
    const catalog = validEventCatalog()
    const legacy = catalog.events[1] as any
    legacy.properties = {
      account_type: { type: 'string', required: true, cardinality: 'bounded', values },
    }
    legacy.legacy_exceptions.push('unbounded_string')

    expect(() => validateEventCatalog(catalog)).toThrow(/values|duplicate|non-empty/i)
  })
})

describe('strict YAML parsing', () => {
  test('rejects duplicate mapping keys before object conversion', () => {
    expect(() =>
      parseEventCatalogYaml('schema_version: 2\nschema_version: 2\nevents: []\n'),
    ).toThrow(/duplicate|unique/i)
  })

  test('rejects unsupported YAML tags and aliases', () => {
    expect(() =>
      parseEventCatalogYaml('schema_version: 2\nevents: !!set { one: null }\n'),
    ).toThrow()
    expect(() =>
      parseEventCatalogYaml('schema_version: 2\nevents: &events []\ncopy: *events\n'),
    ).toThrow(/alias|anchor|unexpected/i)
  })

  test.each(['!!int 2', '!!seq []', '!!str legacy'])('rejects explicit standard YAML tag %s', (tagged) => {
    expect(() =>
      parseEventCatalogYaml(`schema_version: ${tagged}\nevents: []\n`),
    ).toThrow(/tag/i)
  })

  test('accepts alias-like text in quoted values and comments', () => {
    const catalog = validEventCatalog()
    ;(catalog.events[0] as any).description = 'Research &anchor and *alias are prose.'
    const source = `${JSON.stringify(catalog)}\n# Literal &comment_anchor and *comment_alias are prose.\n`

    expect(parseEventCatalogYaml(source).events).toHaveLength(2)
  })

  test('rejects anchors even when no alias references them', () => {
    expect(() =>
      parseEventCatalogYaml('schema_version: 2\nevents: &events []\n'),
    ).toThrow(/anchor/i)
  })
})

describe('product metric validation', () => {
  test('accepts a decision-complete metric with resolvable dependencies', () => {
    expect(
      validateProductMetrics(validMetrics(), validateEventCatalog(validEventCatalog())).metrics,
    ).toHaveLength(1)
  })

  test.each([
    ['required question', (metric: any) => delete metric.question],
    ['required formula', (metric: any) => delete metric.formula],
    ['required cohort start', (metric: any) => delete metric.cohort_start],
    ['required denominator', (metric: any) => delete metric.denominator],
    ['required exclusions', (metric: any) => delete metric.exclusions],
    ['valid ISO duration window', (metric: any) => (metric.window = 'seven days')],
    ['a complete day-duration window', (metric: any) => (metric.window = 'P1DT')],
    ['UTC timezone', (metric: any) => (metric.timezone = 'America/New_York')],
    ['known event dependencies', (metric: any) => (metric.event_dependencies = ['missing_event'])],
    ['explicit availability', (metric: any) => delete metric.availability],
  ])('rejects a metric without %s', (_name, mutate) => {
    const catalog = validMetrics()
    mutate(catalog.metrics[0])
    expect(() =>
      validateProductMetrics(catalog, validateEventCatalog(validEventCatalog())),
    ).toThrow()
  })

  test('rejects YAML with duplicate metric keys', () => {
    expect(() =>
      parseProductMetricsYaml(
        'schema_version: 1\nmetrics:\n  - id: one\n    id: two\n',
      ),
    ).toThrow(/duplicate|unique/i)
  })

  test('requires future dependencies to reference future catalog events', () => {
    const catalog = validMetrics()
    catalog.metrics[0].future_event_dependencies = ['sync_completed']

    expect(() =>
      validateProductMetrics(catalog, validateEventCatalog(validEventCatalog())),
    ).toThrow(/future dependency/i)
  })

  test.each([
    ['an empty dependency union', (metric: any, _events: any) => {
      metric.event_dependencies = []
      metric.future_event_dependencies = []
    }, /at least one.*dependenc/i],
    ['duplicate current dependencies', (metric: any, _events: any) => {
      metric.event_dependencies = ['account_link_completed', 'account_link_completed']
    }, /duplicate current.*dependenc/i],
    ['duplicate future dependencies', (metric: any, events: any) => {
      events.events[0].status = 'future'
      events.events[0].cutover = 'after_instrumentation'
      metric.event_dependencies = []
      metric.future_event_dependencies = ['account_link_completed', 'account_link_completed']
    }, /duplicate future.*dependenc/i],
    ['overlapping current and future dependencies', (metric: any, _events: any) => {
      metric.future_event_dependencies = ['account_link_completed']
    }, /disjoint|overlap/i],
  ])('rejects metrics with %s', (_name, mutate, error) => {
    const catalog = validMetrics()
    const events = validEventCatalog()
    mutate(catalog.metrics[0], events)

    expect(() =>
      validateProductMetrics(catalog, validateEventCatalog(events)),
    ).toThrow(error)
  })
})

describe('deterministic generation', () => {
  test('renders identical bytes regardless of catalog input order', () => {
    const events = validateEventCatalog(validEventCatalog())
    const metrics = validateProductMetrics(validMetrics(), events)
    const reversedEvents = { ...events, events: [...events.events].reverse() }

    expect(renderGeneratedArtifacts(events, metrics)).toEqual(
      renderGeneratedArtifacts(reversedEvents, metrics),
    )
  })

  test('preserves public TypeScript and Go symbol names', () => {
    const events = validateEventCatalog(validEventCatalog())
    const metrics = validateProductMetrics(validMetrics(), events)
    const generated = renderGeneratedArtifacts(events, metrics)

    expect(generated.typescript).toContain("ACCOUNT_LINK_COMPLETED: 'account_link_completed'")
    expect(generated.go).toContain('EventSyncCompleted = "sync_completed"')
    expect(generated.go.endsWith('\n')).toBe(true)
  })

  test('renders frontend payload types and runtime property contracts from the catalog', () => {
    const catalog = validEventCatalog()
    catalog.events[0].properties = {
      method: {
        type: 'string',
        required: true,
        cardinality: 'bounded',
        values: ['plaid'],
      },
      retries: {
        type: 'integer',
        required: false,
        cardinality: 'bounded',
        minimum: 0,
        maximum: 3,
      },
    }
    const events = validateEventCatalog(catalog)
    const generated = renderGeneratedArtifacts(events, validateProductMetrics(validMetrics(), events))

    expect(generated.typescript).toContain('export interface AnalyticsEventProperties')
    expect(generated.typescript).toContain("'account_link_completed': {")
    expect(generated.typescript).toContain("method: 'plaid'")
    expect(generated.typescript).toContain('retries?: number')
    expect(generated.typescript).toContain('export type AnalyticsEventArguments')
    expect(generated.typescript).toContain('export const ANALYTICS_EVENT_CONTRACTS')
    expect(generated.typescript).toContain('export function isValidAnalyticsEventPayload')
    expect(generated.typescript).toContain("type: 'integer', required: false")
    expect(generated.typescript).toContain('minimum: 0, maximum: 3')
  })

  test('types legacy unreviewed properties broadly but generates a stripping boundary', () => {
    const catalog = validEventCatalog()
    catalog.events[0].status = 'legacy'
    catalog.events[0].introduced = 'pre_1650'
    catalog.events[0].properties = {}
    catalog.events[0].legacy_exceptions = ['property_schema_unreviewed']
    const events = validateEventCatalog(catalog)
    const generated = renderGeneratedArtifacts(events, validateProductMetrics(validMetrics(), events))

    expect(generated.typescript).toContain(
      "'account_link_completed': Record<string, unknown>",
    )
    expect(generated.typescript).toContain(
      "export const ANALYTICS_EVENTS_WITH_UNREVIEWED_PROPERTIES",
    )
    expect(generated.typescript).toContain("'account_link_completed',")
    expect(generated.typescript).toContain('export function normalizeAnalyticsEventPayload')
  })

  test('renders MCP Worker constants separately from frontend constants', () => {
    const catalog = validEventCatalog()
    catalog.events.push({
      ...activeEvent(),
      contract_id: 'mcp_tool_called',
      name: 'mcp_tool_called',
      symbols: { typescript: ['TOOL_CALLED'], go: [] },
      producers: ['mcp_worker'],
      subject: 'mcp_user',
    })
    const events = validateEventCatalog(catalog)
    const metrics = validateProductMetrics(validMetrics(), events)
    const generated = renderGeneratedArtifacts(events, metrics)

    expect(generated.mcpTypescript).toContain("TOOL_CALLED: 'mcp_tool_called'")
    expect(generated.typescript).not.toContain("TOOL_CALLED: 'mcp_tool_called'")
  })

  test('emits Go that is already gofmt-clean', () => {
    const catalog = validEventCatalog()
    catalog.events[1].symbols.go.push('EventMuchLongerSyncCompleted')
    const events = validateEventCatalog(catalog)
    const metrics = validateProductMetrics(validMetrics(), events)
    const generated = renderGeneratedArtifacts(events, metrics).go
    const directory = mkdtempSync(join(tmpdir(), 'posthog-contract-test-'))
    const path = join(directory, 'events.go')
    writeFileSync(path, generated)
    const formatted = Bun.spawnSync(['gofmt', path])
    rmSync(directory, { recursive: true })

    expect(formatted.exitCode).toBe(0)
    expect(formatted.stdout.toString()).toBe(generated)
  })

  test('namespaces bounded Go enum types shared by multiple events', () => {
	const catalog = validEventCatalog()
	for (const [suffix, symbol] of [['one', 'EventOneCompleted'], ['two', 'EventTwoCompleted']]) {
	  catalog.events.push({
		...activeEvent(),
		contract_id: `backend_${suffix}_completed`,
		name: `${suffix}_completed`,
		symbols: { typescript: [], go: [symbol] },
		producers: ['backend'],
		properties: { outcome: { type: 'string', required: true, cardinality: 'bounded', values: ['success', 'failure'] } },
	  } as any)
	}
	const events = validateEventCatalog(catalog)
	const generated = renderGeneratedArtifacts(events, validateProductMetrics(validMetrics(), events)).go
	expect(generated).toContain('type OneCompletedOutcome string')
	expect(generated).toContain('type TwoCompletedOutcome string')
	expect(generated.match(/type Outcome string/g)).toBeNull()
  })

  test('keeps bounded Go enum names stable before a property is shared', () => {
	const catalog = validEventCatalog()
	catalog.events.push({
	  ...activeEvent(),
	  contract_id: 'backend_one_completed',
	  name: 'one_completed',
	  symbols: { typescript: [], go: ['EventOneCompleted'] },
	  producers: ['backend'],
	  properties: { outcome: { type: 'string', required: true, cardinality: 'bounded', values: ['success'] } },
	} as any)
	const events = validateEventCatalog(catalog)
	const generated = renderGeneratedArtifacts(events, validateProductMetrics(validMetrics(), events)).go
	expect(generated).toContain('type OneCompletedOutcome string')
  })

  test('renders optional Go properties as pointers and omits absent values', () => {
    const catalog = validEventCatalog()
    const backend = catalog.events[1] as any
    backend.properties = {
      required_string: { type: 'string', required: true, cardinality: 'bounded', values: ['ready'] },
      optional_enum: { type: 'string', required: false, cardinality: 'bounded', values: ['known'] },
      optional_string: { type: 'string', required: false, cardinality: 'high' },
      optional_integer: { type: 'integer', required: false, cardinality: 'bounded' },
      optional_number: { type: 'number', required: false, cardinality: 'bounded' },
      optional_boolean: { type: 'boolean', required: false, cardinality: 'bounded' },
    }
    backend.legacy_exceptions = ['unbounded_string']
    const events = validateEventCatalog(catalog)
    const generated = renderGeneratedArtifacts(events, validateProductMetrics(validMetrics(), events)).go

    expect(generated).toMatch(/RequiredString\s+SyncCompletedRequiredString `json:"required_string"`/)
    expect(generated).toMatch(/OptionalEnum\s+\*SyncCompletedOptionalEnum\s+`json:"optional_enum,omitempty"`/)
    expect(generated).toMatch(/OptionalString\s+\*string\s+`json:"optional_string,omitempty"`/)
    expect(generated).toMatch(/OptionalInteger\s+\*int64\s+`json:"optional_integer,omitempty"`/)
    expect(generated).toMatch(/OptionalNumber\s+\*float64\s+`json:"optional_number,omitempty"`/)
    expect(generated).toMatch(/OptionalBoolean\s+\*bool\s+`json:"optional_boolean,omitempty"`/)
    expect(generated).toContain('if p.OptionalBoolean != nil {')
    expect(generated).toContain('properties["optional_boolean"] = *p.OptionalBoolean')
    expect(generated).not.toContain('"optional_boolean": p.OptionalBoolean')
  })

  test('renders catalog guidance as Go documentation on generated symbols', () => {
    const catalog = validEventCatalog()
    ;(catalog.events[0] as any).description = 'Reports completed account links.\n// Prose stays prose.'
    ;(catalog.events[1] as any).description = 'Reports completed syncs.\n/* Prose stays prose. */'
    const events = validateEventCatalog(catalog)
    const metrics = validateProductMetrics(validMetrics(), events)
    const generated = renderGeneratedArtifacts(events, metrics)

    expect(generated.go).toContain(
      '// EventSyncCompleted Reports completed syncs.\n\t// /* Prose stays prose. */',
    )
    expect(generated.typescript).toContain(
      '// Reports completed account links.\n  // // Prose stays prose.',
    )
  })

  test.each(['stale', 'missing'])('fails --check semantics for a %s generated artifact', (mode) => {
    const root = mkdtempSync(join(tmpdir(), 'posthog-artifact-check-'))
    const events = validateEventCatalog(validEventCatalog())
    const metrics = validateProductMetrics(validMetrics(), events)
    const generated = renderGeneratedArtifacts(events, metrics)
    const paths = {
      typescript: 'apps/web/src/shared/analytics/events.generated.ts',
      mcpTypescript: 'apps/mcp-server/src/analytics/events.generated.ts',
      go: 'backend/internal/integration/posthog/events_generated.go',
    } as const
    for (const [kind, relativePath] of Object.entries(paths) as [keyof typeof paths, string][]) {
      const path = join(root, relativePath)
      mkdirSync(join(path, '..'), { recursive: true })
      writeFileSync(path, generated[kind])
    }

    const target = join(root, paths.mcpTypescript)
    if (mode === 'stale') writeFileSync(target, '// stale\n')
    else rmSync(target)

    expect(() => verifyGeneratedArtifacts(root, generated)).toThrow(/stale.*generate:posthog/i)
    rmSync(root, { recursive: true })
  })
})

describe('raw capture guard', () => {
  test('detects literal event names across supported capture call shapes', () => {
    expect(findRawCaptureEventLiterals(`
      trackEvent('frontend_raw_event')
      track(\`frontend_alias_raw_event\`)
      posthog.capture('$pageview')
      posthog.capture('frontend_properties_raw_event', properties)
      posthog.capture(userId, 'mcp_raw_event', {})
      analytics.capture('generic_alias_raw_event', properties)
      aliasedAnalytics.capture(
        'multiline_alias_raw_event',
        properties,
      )
      s.capture(userID, \`backend_wrapper_raw_event\`, nil)
      analytics.Capture(userID, "backend_raw_event", nil)
      analytics.Capture(userID, \`backend_raw_string_event\`, nil)
      analytics.CaptureIfConsented(ctx, userID, "backend_private_raw_event", nil)
      // track('comment_only_event')
      /* posthog.capture('block_comment_only_event') */
    `)).toEqual([
      '$pageview',
      'backend_private_raw_event',
      'backend_raw_event',
      'backend_raw_string_event',
      'backend_wrapper_raw_event',
      'frontend_alias_raw_event',
      'frontend_properties_raw_event',
      'frontend_raw_event',
      'generic_alias_raw_event',
      'mcp_raw_event',
      'multiline_alias_raw_event',
    ])
  })

  test('ignores dynamic, commented, and prose-only capture expressions', () => {
    expect(findRawCaptureEventLiterals(`
      analytics.capture(eventName, properties)
      posthog.capture(userId, eventName, properties)
      const prose = "analytics.capture('prose_only_event', props)"
      // analytics.capture('line_comment_event', props)
      /* aliased.capture(
        'block_comment_event',
        props,
      ) */
    `)).toEqual([])
  })

  test('finds no raw event-name literals in production capture call sites', () => {
    const root = join(import.meta.dir, '..')
    const findings = productionCaptureSources(root).flatMap((path) =>
      findRawCaptureEventLiterals(readFileSync(join(root, path), 'utf8')).map((event) => `${path}: ${event}`),
    )

    expect(findings).toEqual([])
  })
})

describe('PostHog contract CI', () => {
  test('keeps the direct YAML dependency compatible with npm overrides', () => {
    const packageJson = JSON.parse(
      readFileSync(join(import.meta.dir, '../package.json'), 'utf8'),
    ) as { devDependencies: Record<string, string>; overrides: Record<string, string> }

    expect(packageJson.overrides.yaml).toBe(packageJson.devDependencies.yaml)
  })

  test('installs the pinned Go toolchain before validating generated artifacts', () => {
    const workflow = parseDocument(
      readFileSync(join(import.meta.dir, '../.github/workflows/test.yml'), 'utf8'),
    ).toJS() as any
    const job = workflow.jobs['posthog-contract-validate']
    const filterStep = workflow.jobs['detect-changes'].steps.find((step: any) => step.id === 'filter')
    const filters = parseDocument(filterStep.with.filters).toJS() as Record<string, string[]>
    const setupGo = job.steps.findIndex((step: any) =>
      step.uses === 'actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e')
    const validate = job.steps.findIndex((step: any) => step.run === 'bun run validate:posthog')

    expect(setupGo).toBeGreaterThan(-1)
    expect(job.steps[setupGo].with['go-version']).toBe('${{ env.GO_VERSION }}')
    expect(setupGo).toBeLessThan(validate)
    const productionFilters = filters.posthog_contract.filter((path) =>
      CAPTURE_SOURCE_ROOTS.some((root) => path.startsWith(root)),
    )
    expect(productionFilters).toEqual(CAPTURE_SOURCE_ROOTS.map((root) => `${root}/**`))
  })
})

describe('legacy migration inventory', () => {
  test('pins every pre-schema symbol and event so legacy is not an escape hatch', () => {
    const fixture = JSON.parse(
      readFileSync(join(import.meta.dir, 'fixtures/posthog-events-v1.json'), 'utf8'),
    ) as {
      legacy_events: string[]
      typescript: Record<string, string>
      go: Record<string, string>
      mcp: Record<string, string>
      legacy_contract_metadata: Record<string, unknown>
      descriptions: Record<string, string>
    }
    const catalog = parseEventCatalogYaml(
      readFileSync(join(import.meta.dir, '../config/posthog_events.yaml'), 'utf8'),
    )
    const legacy = catalog.events.filter((event) => event.status === 'legacy')
    const ts = Object.fromEntries(
      legacy
        .filter((event) => event.producers.includes('frontend'))
        .flatMap((event) => event.symbols.typescript.map((symbol) => [symbol, event.name])),
    )
    const go = Object.fromEntries(
      legacy.flatMap((event) => event.symbols.go.map((symbol) => [symbol, event.name])),
    )
    const mcp = Object.fromEntries(
      legacy
        .filter((event) => event.producers.includes('mcp_worker'))
        .flatMap((event) => event.symbols.typescript.map((symbol) => [symbol, event.name])),
    )

    const retiredFrontendEvents = new Set(['first_account_linked', 'plaid_link_succeeded'])
    const retiredMCPEvents = new Set(Object.values(fixture.mcp))
    const retiredEvents = new Set([...retiredFrontendEvents, ...retiredMCPEvents])
    // #1652 removed these from the catalog outright: their emitters died with the deleted
    // manual-tier-override and past-due services, and the concepts they described are gone.
    // Filtered out of the v1-fixture comparison rather than deleted from the fixture, so the
    // historical inventory stays intact and the removal stays explicit and auditable.
    const removedEvents = new Set([
      'admin_subscription_override_expired',
      'past_due_auto_cancelled',
      'past_due_plaid_disconnected',
    ])
    // #1652 promoted these from legacy to active with a real bounded contract, so they are no
    // longer part of the legacy inventory the fixture pins.
    const promotedEvents = new Set([
      'admin_subscription_override_applied_server',
      'admin_subscription_override_removed_server',
      // #1652 todo 10: promoted so the authoritative daily backstop can reuse the same event
      // through the Gateway (a legacy zero-property contract has no generated payload to pass
      // to CaptureEvent). The subscription-webhook fast-path emitter keeps its raw Capture call.
      'household_over_capacity_entered',
    ])
    expect(legacy.map((event) => event.name).sort()).toEqual(
      fixture.legacy_events.filter((event) => !retiredEvents.has(event) && !removedEvents.has(event) && !promotedEvents.has(event)),
    )
    expect(ts).toEqual(Object.fromEntries(
      Object.entries(fixture.typescript).filter(([, event]) => !retiredFrontendEvents.has(event)),
    ))
    expect(go).toEqual(Object.fromEntries(
      Object.entries(fixture.go).filter(([, event]) => !removedEvents.has(event) && !promotedEvents.has(event)),
    ))
    expect(mcp).toEqual({})
    expect(fixture.mcp).toEqual({
      OAUTH_AUTHORIZED: 'mcp_oauth_authorized',
      TOOL_CALLED: 'mcp_tool_called',
      CODE_EXECUTED: 'mcp_code_executed',
      EXECUTE_FAILED: 'mcp_execute_failed',
      SESSION_REVOKED: 'mcp_session_revoked',
    })
    for (const eventName of retiredMCPEvents) {
      const retired = catalog.events.find((event) => event.name === eventName)
      expect(retired?.status).toBe('deprecated')
      expect(retired?.deprecated).toContain('#1650')
      expect(retired?.symbols.typescript).toEqual([])
      expect(retired?.symbols.go).toEqual([])
    }
    for (const eventName of retiredFrontendEvents) {
      const retired = catalog.events.find((event) => event.name === eventName)
      expect(retired?.status).toBe('deprecated')
      expect(retired?.deprecated).toContain('#1650')
    }
    const metadata = Object.fromEntries(
      catalog.events
        .filter((event) => ['frontend_account_holdings_history_viewed', 'frontend_account_balance_history_viewed'].includes(event.contract_id))
        .map((event) => [event.contract_id, {
          properties: event.properties,
          legacy_exceptions: event.legacy_exceptions,
        }]),
    )
    expect(metadata).toEqual(fixture.legacy_contract_metadata)
  })

  test('preserves load-bearing product and privacy guidance in the catalog', () => {
    const fixture = JSON.parse(
      readFileSync(join(import.meta.dir, 'fixtures/posthog-events-v1.json'), 'utf8'),
    ) as { descriptions: Record<string, string> }
    const catalog = parseEventCatalogYaml(
      readFileSync(join(import.meta.dir, '../config/posthog_events.yaml'), 'utf8'),
    )
    const removedContractIds = new Set([
      'backend_admin_subscription_override_expired',
      'backend_past_due_auto_cancelled',
      'backend_past_due_plaid_disconnected',
    ])
    // #1652 todo 10 promoted this contract to active and rewrote its description to name the
    // daily-backstop emitter, so the fixture's historical description no longer matches.
    const promotedContractIds = new Set(['backend_household_over_capacity_entered'])
    const descriptions = Object.fromEntries(
      catalog.events
        .filter((event) => Object.hasOwn(fixture.descriptions, event.contract_id) && !promotedContractIds.has(event.contract_id))
        .map((event) => [event.contract_id, event.description]),
    )

    expect(descriptions).toEqual(Object.fromEntries(
      Object.entries(fixture.descriptions).filter(([contractId]) => !removedContractIds.has(contractId) && !promotedContractIds.has(contractId)),
    ))
  })
})
