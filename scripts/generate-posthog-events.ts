import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'

import { parseDocument, visit } from 'yaml'

import { isPiiKey, tokenize } from '../apps/web/src/shared/lib/piiScrubbing'

const PRODUCERS = new Set(['frontend', 'backend', 'mcp_worker'])
const STATUSES = new Set(['active', 'legacy', 'future', 'deprecated'])
const PRIVACY_CLASSES = new Set(['non_sensitive', 'pseudonymous'])
const CONSENT_CLASSES = new Set(['analytics_required', 'operational'])
const SUBJECTS = new Set(['actor_user', 'target_user', 'system', 'mcp_user', 'mixed_legacy_subjects'])
const CARDINALITIES = new Set(['bounded', 'low', 'high'])
const PROPERTY_TYPES = new Set(['boolean', 'integer', 'number', 'string'])
const LEGACY_EXCEPTIONS = new Set([
  'cross_layer_name_collision',
  'property_schema_unreviewed',
  'sensitive_property_key',
  'unbounded_string',
])
const EVENT_NAME = /^[a-z][a-z0-9]*(?:_[a-z0-9]+)*$/
const PROPERTY_NAME = /^[a-z][A-Za-z0-9]*(?:_[a-z0-9]+)*$/
const VENDOR_EVENT_NAME = /^\$[a-z][a-z0-9_]*$/
const CATALOG_SENSITIVE_KEY =
  /(^|_)(account_number|category|contribution_id|goal_id|job_id|name|token|transaction_id|value)($|_)/
const TS_SYMBOL = /^[A-Z][A-Z0-9]*(?:_[A-Z0-9]+)*$/
const GO_SYMBOL = /^[A-Z][A-Za-z0-9]*$/
const METRIC_ID = EVENT_NAME
const DAY_DURATION = /^P[1-9]\d*D$/
const PROPERTY_FIELDS = new Set(['type', 'required', 'cardinality', 'values', 'minimum', 'maximum'])
const METRIC_REQUIRED_STRING_FIELDS = [
  'question',
  'formula',
  'unit',
  'cohort_start',
  'denominator',
  'consent_caveat',
  'owner',
  'review_cadence',
  'decision_supported',
] as const

type UnknownRecord = Record<string, unknown>
type SourceToken = { kind: 'identifier' | 'string' | 'punctuation'; value: string }

function captureArgumentIndexes(callName: string): number[] {
  switch (callName) {
    case 'CaptureIfConsented':
      return [2]
    case 'Capture':
      return [1]
    case 'capture':
      return [0, 1]
    case 'track':
    case 'trackEvent':
      return [0]
    default:
      return []
  }
}

export function findRawCaptureEventLiterals(source: string): string[] {
  const tokens = tokenizeSource(source)
  const findings = new Set<string>()
  for (let index = 0; index < tokens.length - 1; index += 1) {
    const call = tokens[index]
    if (call.kind !== 'identifier' || tokens[index + 1].value !== '(') continue
    const argumentIndexes = captureArgumentIndexes(call.value)
    if (argumentIndexes.length === 0) continue
    const args = callArguments(tokens, index + 1)
    for (const argumentIndex of argumentIndexes) {
      const literal = args[argumentIndex]?.[0]
      if (literal?.kind === 'string' && /^\$?[a-z][a-z0-9_]*$/.test(literal.value)) {
        findings.add(literal.value)
      }
    }
  }
  return [...findings].sort(compareCodePoints)
}

function tokenizeSource(source: string): SourceToken[] {
  const tokens: SourceToken[] = []
  for (let index = 0; index < source.length; index += 1) {
    const current = source[index]
    const next = source[index + 1]
    if (current === '/' && next === '/') {
      while (index < source.length && source[index] !== '\n') index += 1
    } else if (current === '/' && next === '*') {
      index += 2
      while (index < source.length && !(source[index] === '*' && source[index + 1] === '/')) index += 1
      index += 1
    } else if (current === "'" || current === '"' || current === '`') {
      let value = ''
      const quote = current
      while (++index < source.length && source[index] !== quote) {
        if (source[index] === '\\' && quote !== '`') {
          value += source[index + 1] ?? ''
          index += 1
        } else {
          value += source[index]
        }
      }
      tokens.push({ kind: 'string', value })
    } else if (/[A-Za-z_$]/.test(current)) {
      let value = current
      while (/[A-Za-z0-9_$]/.test(source[index + 1] ?? '')) value += source[++index]
      tokens.push({ kind: 'identifier', value })
    } else if ('(),'.includes(current)) {
      tokens.push({ kind: 'punctuation', value: current })
    }
  }
  return tokens
}

function callArguments(tokens: SourceToken[], openParenthesis: number): SourceToken[][] {
  const args: SourceToken[][] = [[]]
  let depth = 1
  for (let index = openParenthesis + 1; index < tokens.length && depth > 0; index += 1) {
    const token = tokens[index]
    if (token.value === '(') depth += 1
    if (token.value === ')') depth -= 1
    if (depth === 0) break
    if (token.value === ',' && depth === 1) args.push([])
    else args.at(-1)?.push(token)
  }
  return args
}

export interface EventDefinition {
  contract_id: string
  name: string
  kind: 'custom' | 'vendor_reserved'
  symbols: { typescript: string[]; go: string[] }
  producers: string[]
  subject: string
  consent: string
  privacy: string
  status: string
  introduced: string
  description?: string
  deprecated?: string
  cutover?: string
  properties: Record<string, UnknownRecord>
  legacy_exceptions?: string[]
  ownership?: Array<{ producer: string; subject: string }>
  metric_owner?: string
}

export interface EventCatalog {
  schema_version: 2
  events: EventDefinition[]
}

export interface ProductMetric {
  id: string
  question: string
  formula: string
  unit: string
  cohort_start: string
  denominator: string
  exclusions: string[]
  window: string
  timezone: 'UTC'
  consent_caveat: string
  owner: string
  review_cadence: string
  decision_supported: string
  availability: 'live' | 'planned'
  event_dependencies: string[]
  future_event_dependencies: string[]
}

export interface ProductMetricCatalog {
  schema_version: 1
  metrics: ProductMetric[]
}

const EVENT_FIELDS = new Set([
  'contract_id',
  'name',
  'kind',
  'symbols',
  'producers',
  'subject',
  'consent',
  'privacy',
  'status',
  'introduced',
  'description',
  'deprecated',
  'cutover',
  'properties',
  'legacy_exceptions',
  'ownership',
  'metric_owner',
])
const METRIC_FIELDS = new Set([
  'id',
  'question',
  'formula',
  'unit',
  'cohort_start',
  'denominator',
  'exclusions',
  'window',
  'timezone',
  'consent_caveat',
  'owner',
  'review_cadence',
  'decision_supported',
  'availability',
  'event_dependencies',
  'future_event_dependencies',
])

function fail(message: string): never {
  throw new Error(message)
}

function record(value: unknown, label: string): UnknownRecord {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) {
    fail(`${label} must be a mapping`)
  }
  return value as UnknownRecord
}

function nonEmptyString(value: unknown, label: string): string {
  if (typeof value !== 'string' || value.trim() === '') fail(`${label} must be a non-empty string`)
  return value
}

function stringArray(value: unknown, label: string): string[] {
  if (!Array.isArray(value) || value.some((item) => typeof item !== 'string' || item === '')) {
    fail(`${label} must be an array of non-empty strings`)
  }
  return value as string[]
}

function rejectUnknownFields(value: UnknownRecord, allowed: Set<string>, label: string): void {
  for (const key of Object.keys(value)) {
    if (!allowed.has(key)) fail(`${label} contains unexpected field ${key}`)
  }
}

function parseStrictYaml(source: string, label: string): unknown {
  const document = parseDocument(source, { uniqueKeys: true, maxAliasCount: 0 })
  if (document.errors.length > 0) fail(`${label}: ${document.errors[0].message}`)
  if (document.warnings.length > 0) fail(`${label}: ${document.warnings[0].message}`)
  visit(document, {
    Alias() {
      fail(`${label} must not use YAML aliases`)
    },
    Node(_key, node) {
      if (node.tag !== undefined) fail(`${label} must not use explicit YAML tags`)
      if ('anchor' in node && node.anchor !== undefined) fail(`${label} must not use YAML anchors`)
    },
  })
  return document.toJS({ maxAliasCount: 0 })
}

export function parseEventCatalogYaml(source: string): EventCatalog {
  return validateEventCatalog(parseStrictYaml(source, 'event catalog'))
}

export function parseProductMetricsYaml(source: string): ProductMetricCatalog {
  const parsed = parseStrictYaml(source, 'product metrics')
  const root = record(parsed, 'product metrics')
  if (root.schema_version !== 1 || !Array.isArray(root.metrics)) {
    fail('product metrics must use schema_version 1 and contain metrics')
  }
  return parsed as ProductMetricCatalog
}

function validateProperties(event: EventDefinition): void {
  const exceptions = new Set(event.legacy_exceptions ?? [])
  let hasUnboundedString = false
  for (const [key, rawProperty] of Object.entries(event.properties)) {
    if (!PROPERTY_NAME.test(key)) fail(`${event.contract_id} has invalid property name ${key}`)
    const normalizedKey = tokenize(key).join('_')
    if ((isPiiKey(key) || CATALOG_SENSITIVE_KEY.test(normalizedKey)) && !exceptions.has('sensitive_property_key')) {
      fail(`${event.contract_id} has banned sensitive property key ${key}`)
    }
    const property = record(rawProperty, `${event.contract_id}.${key}`)
    rejectUnknownFields(property, PROPERTY_FIELDS, `${event.contract_id}.${key}`)
    if (!PROPERTY_TYPES.has(nonEmptyString(property.type, `${event.contract_id}.${key}.type`))) {
      fail(`${event.contract_id}.${key} has unknown property type`)
    }
    if (typeof property.required !== 'boolean') fail(`${event.contract_id}.${key}.required must be boolean`)
    const cardinality = nonEmptyString(property.cardinality, `${event.contract_id}.${key}.cardinality`)
    if (!CARDINALITIES.has(cardinality)) fail(`${event.contract_id}.${key} has unknown cardinality`)
    if (property.type === 'string') {
      const values = property.values
      if (values !== undefined && (
        !Array.isArray(values) ||
        values.length === 0 ||
        values.some((item) => typeof item !== 'string' || item === '') ||
        new Set(values).size !== values.length
      )) {
        fail(`${event.contract_id}.${key}.values must be unique non-empty strings`)
      }
      const isUnbounded =
        cardinality !== 'bounded' ||
        values === undefined
      if (isUnbounded) {
        hasUnboundedString = true
        if (event.status !== 'legacy' || !exceptions.has('unbounded_string')) {
          fail(`${event.contract_id}.${key} is unbounded; only an explicit legacy unbounded_string exception may allow it`)
        }
      }
    }
    if (property.type !== 'string' && property.values !== undefined) {
      fail(`${event.contract_id}.${key}.values is only valid for strings`)
    }
    if (!['integer', 'number'].includes(property.type as string) &&
        (property.minimum !== undefined || property.maximum !== undefined)) {
      fail(`${event.contract_id}.${key} numeric bounds require a numeric property`)
    }
    for (const bound of ['minimum', 'maximum'] as const) {
      if (property[bound] !== undefined && !Number.isFinite(property[bound])) {
        fail(`${event.contract_id}.${key}.${bound} must be finite`)
      }
    }
    if (
      typeof property.minimum === 'number' &&
      typeof property.maximum === 'number' &&
      property.minimum > property.maximum
    ) {
      fail(`${event.contract_id}.${key} has reversed numeric bounds`)
    }
  }
  if (exceptions.has('unbounded_string') && !hasUnboundedString) {
    fail(`${event.contract_id} has an unnecessary unbounded_string exception`)
  }
}

export function validateEventCatalog(input: unknown): EventCatalog {
  const root = record(input, 'event catalog')
  rejectUnknownFields(root, new Set(['schema_version', 'events']), 'event catalog')
  if (root.schema_version !== 2 || !Array.isArray(root.events)) {
    fail('event catalog must use schema_version 2 and contain events')
  }

  const contractIds = new Set<string>()
  const tsSymbols = new Set<string>()
  const goSymbols = new Set<string>()
  const eventNames = new Set<string>()
  for (const rawEvent of root.events) {
    const raw = record(rawEvent, 'event')
    rejectUnknownFields(raw, EVENT_FIELDS, 'event')
    const event = raw as unknown as EventDefinition
    event.contract_id = nonEmptyString(event.contract_id, 'event.contract_id')
    if (!EVENT_NAME.test(event.contract_id) || contractIds.has(event.contract_id)) {
      fail(`invalid or duplicate contract_id ${event.contract_id}`)
    }
    contractIds.add(event.contract_id)
    event.name = nonEmptyString(event.name, `${event.contract_id}.name`)
    if (!['custom', 'vendor_reserved'].includes(event.kind)) fail(`${event.contract_id} has unknown event kind`)
    if (event.kind === 'custom' && !EVENT_NAME.test(event.name)) {
      fail(`${event.contract_id} has invalid custom snake_case event name`)
    }
    if (event.kind === 'vendor_reserved' && !VENDOR_EVENT_NAME.test(event.name)) {
      fail(`${event.contract_id} has invalid vendor-reserved event name`)
    }
    if (eventNames.has(event.name)) fail(`duplicate event name ${event.name}`)
    eventNames.add(event.name)
    const symbols = record(event.symbols, `${event.contract_id}.symbols`)
    rejectUnknownFields(symbols, new Set(['typescript', 'go']), `${event.contract_id}.symbols`)
    event.symbols = {
      typescript: stringArray(symbols.typescript, `${event.contract_id}.symbols.typescript`),
      go: stringArray(symbols.go, `${event.contract_id}.symbols.go`),
    }
    for (const symbol of event.symbols.typescript) {
      if (!TS_SYMBOL.test(symbol) || tsSymbols.has(symbol)) fail(`invalid or duplicate TypeScript symbol ${symbol}`)
      tsSymbols.add(symbol)
    }
    for (const symbol of event.symbols.go) {
      if (!GO_SYMBOL.test(symbol) || goSymbols.has(symbol)) fail(`invalid or duplicate Go symbol ${symbol}`)
      goSymbols.add(symbol)
    }
    event.producers = stringArray(event.producers, `${event.contract_id}.producers`)
    if (event.producers.length === 0 || event.producers.some((value) => !PRODUCERS.has(value))) {
      fail(`${event.contract_id} has unknown producer`)
    }
    const hasFrontendProducer = event.producers.includes('frontend')
    const hasMcpProducer = event.producers.includes('mcp_worker')
    const hasBackendProducer = event.producers.includes('backend')
    if (event.symbols.typescript.length > 0 && !hasFrontendProducer && !hasMcpProducer) {
      fail(`${event.contract_id} has TypeScript symbols without a TypeScript producer`)
    }
    if (event.symbols.go.length > 0 && !hasBackendProducer) {
      fail(`${event.contract_id} has Go symbols without the backend producer`)
    }
    if (event.symbols.typescript.length > 0 && hasFrontendProducer && hasMcpProducer) {
      fail(`${event.contract_id} has ambiguous TypeScript symbol producer ownership`)
    }
    if (!SUBJECTS.has(nonEmptyString(event.subject, `${event.contract_id}.subject`))) fail(`${event.contract_id} has unknown subject`)
    if (!CONSENT_CLASSES.has(nonEmptyString(event.consent, `${event.contract_id}.consent`))) fail(`${event.contract_id} has unknown consent class`)
    if (!PRIVACY_CLASSES.has(nonEmptyString(event.privacy, `${event.contract_id}.privacy`))) fail(`${event.contract_id} has unknown privacy class`)
    if (!STATUSES.has(nonEmptyString(event.status, `${event.contract_id}.status`))) fail(`${event.contract_id} has unknown status`)
    nonEmptyString(event.introduced, `${event.contract_id}.introduced`)
    if (raw.description !== undefined) nonEmptyString(raw.description, `${event.contract_id}.description`)
    if (event.deprecated !== undefined) nonEmptyString(event.deprecated, `${event.contract_id}.deprecated`)
    if (event.cutover !== undefined) nonEmptyString(event.cutover, `${event.contract_id}.cutover`)
    if (event.status === 'future' && event.cutover === undefined) {
      fail(`${event.contract_id} must declare future cutover metadata`)
    }
    if (event.status === 'deprecated' && event.deprecated === undefined) {
      fail(`${event.contract_id} must declare deprecation metadata`)
    }
    event.properties = record(event.properties, `${event.contract_id}.properties`)
    if (event.legacy_exceptions !== undefined) {
      event.legacy_exceptions = stringArray(event.legacy_exceptions, `${event.contract_id}.legacy_exceptions`)
      if (event.status !== 'legacy' || event.legacy_exceptions.some((value) => !LEGACY_EXCEPTIONS.has(value))) {
        fail(`${event.contract_id} has invalid legacy exceptions`)
      }
    }
    const hasCrossLayerCollision = event.legacy_exceptions?.includes('cross_layer_name_collision') ?? false
    if (hasCrossLayerCollision) {
      if (event.producers.length < 2) {
        fail(`${event.contract_id} has cross-layer collision metadata without multiple producers`)
      }
      if (!Array.isArray(event.ownership) || event.ownership.length !== event.producers.length) {
        fail(`${event.contract_id} must document ownership for every colliding producer`)
      }
      const ownershipProducers = new Set<string>()
      for (const rawOwner of event.ownership) {
        const owner = record(rawOwner, `${event.contract_id}.ownership`)
        rejectUnknownFields(owner, new Set(['producer', 'subject']), `${event.contract_id}.ownership`)
        const producer = nonEmptyString(owner.producer, `${event.contract_id}.ownership.producer`)
        const subject = nonEmptyString(owner.subject, `${event.contract_id}.ownership.subject`)
        if (!event.producers.includes(producer) || ownershipProducers.has(producer) || !SUBJECTS.has(subject)) {
          fail(`${event.contract_id} has ambiguous collision ownership`)
        }
        ownershipProducers.add(producer)
      }
      if (!['none_until_cutover', ...event.producers].includes(event.metric_owner ?? '')) {
        fail(`${event.contract_id} must declare a metric_owner`)
      }
    } else if (event.ownership !== undefined || event.metric_owner !== undefined) {
      fail(`${event.contract_id} has ownership metadata without a collision`)
    }
    if (event.producers.length > 1 && !hasCrossLayerCollision) {
      fail(`${event.contract_id} has multiple producers without explicit collision ownership`)
    }
    validateProperties(event)
  }
  return input as EventCatalog
}

export function validateProductMetrics(input: unknown, events: EventCatalog): ProductMetricCatalog {
  const root = record(input, 'product metrics')
  rejectUnknownFields(root, new Set(['schema_version', 'metrics']), 'product metrics')
  if (root.schema_version !== 1 || !Array.isArray(root.metrics)) {
    fail('product metrics must use schema_version 1 and contain metrics')
  }
  const knownEvents = new Set(events.events.map((event) => event.name))
  const futureEvents = new Set(
    events.events.filter((event) => event.status === 'future').map((event) => event.name),
  )
  const ids = new Set<string>()
  for (const rawMetric of root.metrics) {
    const metric = record(rawMetric, 'metric') as unknown as ProductMetric
    rejectUnknownFields(metric as unknown as UnknownRecord, METRIC_FIELDS, 'metric')
    metric.id = nonEmptyString(metric.id, 'metric.id')
    if (!METRIC_ID.test(metric.id) || ids.has(metric.id)) fail(`invalid or duplicate metric id ${metric.id}`)
    ids.add(metric.id)
    for (const field of METRIC_REQUIRED_STRING_FIELDS) {
      nonEmptyString(metric[field], `${metric.id}.${field}`)
    }
    metric.exclusions = stringArray(metric.exclusions, `${metric.id}.exclusions`)
    if (metric.exclusions.length === 0) fail(`${metric.id}.exclusions must not be empty`)
    if (metric.timezone !== 'UTC') fail(`${metric.id}.timezone must be UTC`)
    if (typeof metric.window !== 'string' || !DAY_DURATION.test(metric.window)) {
      fail(`${metric.id}.window must be a positive ISO-8601 day duration`)
    }
    if (!['live', 'planned'].includes(metric.availability)) fail(`${metric.id} has invalid availability`)
    metric.event_dependencies = stringArray(metric.event_dependencies, `${metric.id}.event_dependencies`)
    metric.future_event_dependencies = stringArray(metric.future_event_dependencies, `${metric.id}.future_event_dependencies`)
    const dependencies = [...metric.event_dependencies, ...metric.future_event_dependencies]
    const currentDependencySet = new Set(metric.event_dependencies)
    const futureDependencySet = new Set(metric.future_event_dependencies)
    if (dependencies.length === 0) fail(`${metric.id} must declare at least one event dependency`)
    if (currentDependencySet.size !== metric.event_dependencies.length) {
      fail(`${metric.id} has duplicate current event dependencies`)
    }
    if (futureDependencySet.size !== metric.future_event_dependencies.length) {
      fail(`${metric.id} has duplicate future event dependencies`)
    }
    if (metric.event_dependencies.some((dependency) => futureDependencySet.has(dependency))) {
      fail(`${metric.id} event dependency lists must be disjoint`)
    }
    for (const dependency of dependencies) {
      if (!knownEvents.has(dependency)) fail(`${metric.id} references unknown event ${dependency}`)
    }
    if (metric.event_dependencies.some((dependency) => futureEvents.has(dependency))) {
      fail(`${metric.id} must classify future events as future dependencies`)
    }
    if (metric.future_event_dependencies.some((dependency) => !futureEvents.has(dependency))) {
      fail(`${metric.id} has a future dependency that is not a future event`)
    }
    if (metric.availability === 'live' && metric.future_event_dependencies.length > 0) {
      fail(`${metric.id} cannot be live with future event dependencies`)
    }
  }
  return input as ProductMetricCatalog
}

function compareCodePoints(left: string, right: string): number {
  if (left < right) return -1
  if (left > right) return 1
  return 0
}

function gofmt(source: string): string {
  const directory = mkdtempSync(join(tmpdir(), 'posthog-events-'))
  const path = join(directory, 'events.go')
  try {
    writeFileSync(path, source)
    const result = Bun.spawnSync(['gofmt', path])
    if (result.exitCode !== 0) fail(`gofmt failed: ${result.stderr.toString().trim()}`)
    return result.stdout.toString()
  } finally {
    rmSync(directory, { recursive: true })
  }
}

interface SymbolEntry {
  symbol: string
  name: string
  description?: string
  event?: EventDefinition
}

function goIdentifier(value: string): string {
  return value
    .split('_')
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join('')
}

function renderGoPropertyTypes(entries: SymbolEntry[]): string[] {
  function enumTypeName(base: string, key: string): string {
    return `${base}${goIdentifier(key)}`
  }
  return entries.flatMap(({ symbol, event }) => {
    if (!event || Object.keys(event.properties).length === 0) return []
    const base = symbol.replace(/^Event/, '')
    const enumTypes = Object.entries(event.properties).flatMap(([key, property]) => {
      if (property.type !== 'string' || !Array.isArray(property.values)) return []
      const typeName = enumTypeName(base, key)
      return [
        `type ${typeName} string`,
        '',
        'const (',
        ...(property.values as string[]).map((value) => `\t${typeName}${goIdentifier(value)} ${typeName} = "${value}"`),
        ')',
        '',
      ]
    })
    const fields = Object.entries(event.properties).map(([key, property]) => {
      let typeName = 'string'
      if (property.type === 'string' && Array.isArray(property.values)) {
        typeName = enumTypeName(base, key)
      } else if (property.type === 'boolean') {
        typeName = 'bool'
      } else if (property.type === 'integer') {
        typeName = 'int64'
      } else if (property.type === 'number') {
        typeName = 'float64'
      }
      if (!property.required) {
        typeName = `*${typeName}`
      }
      return `\t${goIdentifier(key)} ${typeName} \`json:"${key}${property.required ? '' : ',omitempty'}"\``
    })
    const requiredProps = Object.entries(event.properties)
      .filter(([, property]) => property.required)
      .map(([key]) => `\t\t"${key}": p.${goIdentifier(key)},`)
    const optionalProps = Object.entries(event.properties)
      .filter(([, property]) => !property.required)
      .flatMap(([key]) => [
        `\tif p.${goIdentifier(key)} != nil {`,
        `\t\tproperties["${key}"] = *p.${goIdentifier(key)}`,
        '\t}',
      ])
    const propertiesMethod = optionalProps.length === 0
      ? [
          `func (p ${base}Payload) Properties() map[string]interface{} {`,
          '\treturn map[string]interface{}{',
          ...requiredProps,
          '\t}',
          '}',
        ]
      : [
          `func (p ${base}Payload) Properties() map[string]interface{} {`,
          '\tproperties := map[string]interface{}{',
          ...requiredProps,
          '\t}',
          ...optionalProps,
          '\treturn properties',
          '}',
        ]
    return [
      ...enumTypes,
      `type ${base}Payload struct {`,
      ...fields,
      '}',
      '',
      `func (${base}Payload) isGeneratedEvent() {}`,
      `func (${base}Payload) EventName() EventName { return ${symbol} }`,
      ...propertiesMethod,
      '',
    ]
  })
}

function renderGoContracts(entries: SymbolEntry[]): string[] {
  return [
    'var generatedEventContracts = map[EventName]EventContract{',
    ...entries.flatMap(({ symbol, event }) => {
      if (!event) return []
      const properties = Object.entries(event.properties)
      return [
        `\t${symbol}: {`,
        `\t\tConsent: "${event.consent}",`,
        `\t\tStatus: "${event.status}",`,
        `\t\tLegacyPropertiesUnreviewed: ${event.legacy_exceptions?.includes('property_schema_unreviewed') ?? false},`,
        '\t\tProperties: map[string]PropertyContract{',
        ...properties.map(([key, property]) => `\t\t\t"${key}": {Kind: "${property.type}", Required: ${property.required}, Values: []string{${Array.isArray(property.values) ? property.values.map((value) => `"${value}"`).join(', ') : ''}}, Minimum: ${typeof property.minimum === 'number' ? property.minimum : 'nil'}, Maximum: ${typeof property.maximum === 'number' ? property.maximum : 'nil'}},`),
        '\t\t},',
        '\t},',
      ]
    }),
    '}',
    '',
  ]
}

function renderDescription(description: string | undefined, indent: string, firstPrefix = ''): string[] {
  if (!description) return []
  return description.split(/\r?\n/).map((line, index) =>
    `${indent}// ${index === 0 ? firstPrefix : ''}${line}`,
  )
}

function renderTypeScriptConstants(
  constantName: string,
  typeName: string,
  entries: SymbolEntry[],
): string {
  return [
    '// Code generated by scripts/generate-posthog-events.ts; DO NOT EDIT.',
    '',
    `export const ${constantName} = {`,
    ...entries.flatMap(({ symbol, name, description }) => [
      ...renderDescription(description, '  '),
      `  ${symbol}: '${name}',`,
    ]),
    '} as const',
    '',
    `export type ${typeName} = (typeof ${constantName})[keyof typeof ${constantName}]`,
    '',
  ].join('\n')
}

function renderTypeScriptPropertyType(property: UnknownRecord): string {
  if (property.type === 'string') {
    if (Array.isArray(property.values)) {
      return property.values.map((value) => `'${value}'`).join(' | ')
    }
    return 'string'
  }
  if (property.type === 'boolean') return 'boolean'
  return 'number'
}

function renderFrontendTypeScript(entries: SymbolEntry[]): string {
  return [
    '// Code generated by scripts/generate-posthog-events.ts; DO NOT EDIT.',
    '',
    'export const ANALYTICS_EVENTS = {',
    ...entries.flatMap(({ symbol, name, description }) => [
      ...renderDescription(description, '  '),
      `  ${symbol}: '${name}',`,
    ]),
    '} as const',
    '',
    'export interface AnalyticsEventProperties {',
    ...entries.flatMap(({ name, event }) => {
      const properties = Object.entries(event?.properties ?? {})
      if (properties.length === 0) {
        const propertyType = event?.legacy_exceptions?.includes('property_schema_unreviewed')
          ? 'Record<string, unknown>'
          : 'Record<string, never>'
        return [`  '${name}': ${propertyType}`]
      }
      return [
        `  '${name}': {`,
        ...properties.map(([key, property]) => (
          `    ${key}${property.required ? '' : '?'}: ${renderTypeScriptPropertyType(property)}`
        )),
        '  }',
      ]
    }),
    '}',
    '',
    'export type AnalyticsEventName = keyof AnalyticsEventProperties',
    'export type AnalyticsEventArguments<Event extends AnalyticsEventName> =',
    '  keyof AnalyticsEventProperties[Event] extends never',
    '    ? [properties?: never]',
    '    : Record<never, never> extends AnalyticsEventProperties[Event]',
    '      ? [properties?: AnalyticsEventProperties[Event]]',
    '      : [properties: AnalyticsEventProperties[Event]]',
    '',
    'export const ANALYTICS_EVENTS_WITH_UNREVIEWED_PROPERTIES: ReadonlySet<AnalyticsEventName> =',
    '  new Set([',
    ...entries
      .filter(({ event }) => event?.legacy_exceptions?.includes('property_schema_unreviewed'))
      .map(({ name }) => `    '${name}',`),
    '  ])',
    '',
    'export interface AnalyticsPropertyContract {',
    "  type: 'boolean' | 'integer' | 'number' | 'string'",
    '  required: boolean',
    '  values?: readonly string[]',
    '  minimum?: number',
    '  maximum?: number',
    '}',
    '',
    'export const ANALYTICS_EVENT_CONTRACTS: Readonly<Record<',
    '  AnalyticsEventName,',
    '  Readonly<Record<string, AnalyticsPropertyContract>>',
    '>> = {',
    ...entries.flatMap(({ name, event }) => [
      `  '${name}': {`,
      ...Object.entries(event?.properties ?? {}).map(([key, property]) => {
        const fields = [`type: '${property.type}'`, `required: ${property.required}`]
        if (Array.isArray(property.values)) {
          fields.push(`values: [${property.values.map((value) => `'${value}'`).join(', ')}]`)
        }
        if (typeof property.minimum === 'number') fields.push(`minimum: ${property.minimum}`)
        if (typeof property.maximum === 'number') fields.push(`maximum: ${property.maximum}`)
        return `    ${key}: { ${fields.join(', ')} },`
      }),
      '  },',
    ]),
    '}',
    '',
    'function isValidAnalyticsProperty(',
    '  value: unknown,',
    '  contract: AnalyticsPropertyContract,',
    '): boolean {',
    '  switch (contract.type) {',
    "    case 'boolean':",
    "      return typeof value === 'boolean'",
    "    case 'integer':",
    "      return typeof value === 'number'",
    '        && Number.isInteger(value)',
    '        && (contract.minimum === undefined || value >= contract.minimum)',
    '        && (contract.maximum === undefined || value <= contract.maximum)',
    "    case 'number':",
    "      return typeof value === 'number'",
    '        && Number.isFinite(value)',
    '        && (contract.minimum === undefined || value >= contract.minimum)',
    '        && (contract.maximum === undefined || value <= contract.maximum)',
    "    case 'string':",
    "      return typeof value === 'string'",
    '        && (contract.values === undefined || contract.values.includes(value))',
    '  }',
    '}',
    '',
    'function isPlainAnalyticsProperties(value: unknown): value is Record<string, unknown> | undefined {',
    '  if (value === undefined) return true',
    "  if (value === null || typeof value !== 'object' || Array.isArray(value)) return false",
    '  try {',
    '    const prototype = Object.getPrototypeOf(value)',
    '    return prototype === Object.prototype || prototype === null',
    '  } catch {',
    '    return false',
    '  }',
    '}',
    '',
    'export function normalizeAnalyticsEventPayload(',
    '  event: string,',
    '  properties?: unknown,',
    '): Record<string, unknown> | null {',
    '  if (!Object.prototype.hasOwnProperty.call(ANALYTICS_EVENT_CONTRACTS, event)) return null',
    '  if (!isPlainAnalyticsProperties(properties)) return null',
    '  const input = (properties ?? {}) as Record<string, unknown>',
    '  const analyticsEvent = event as AnalyticsEventName',
    '  if (ANALYTICS_EVENTS_WITH_UNREVIEWED_PROPERTIES.has(analyticsEvent)) return {}',
    '  const contract = ANALYTICS_EVENT_CONTRACTS[analyticsEvent]',
    '  for (const key of Object.keys(input)) {',
    '    if (!Object.prototype.hasOwnProperty.call(contract, key)) return null',
    '  }',
    '  for (const [key, propertyContract] of Object.entries(contract)) {',
    '    const value = input[key]',
    '    if (value === undefined) {',
    '      if (propertyContract.required) return null',
    '      continue',
    '    }',
    '    if (!isValidAnalyticsProperty(value, propertyContract)) return null',
    '  }',
    '  return input',
    '}',
    '',
    'export function isValidAnalyticsEventPayload(',
    '  event: string,',
    '  properties?: unknown,',
    '): boolean {',
    '  return normalizeAnalyticsEventPayload(event, properties) !== null',
    '}',
    '',
  ].join('\n')
}

export function renderGeneratedArtifacts(
  events: EventCatalog,
  _metrics: ProductMetricCatalog,
): { typescript: string; mcpTypescript: string; go: string } {
  const tsEntries = events.events
    .filter((event) => !event.producers.includes('mcp_worker'))
    .flatMap((event) => event.symbols.typescript.map((symbol) => ({
      symbol,
      name: event.name,
      description: event.description,
      event,
    })))
    .sort((left, right) => compareCodePoints(left.symbol, right.symbol))
  const mcpEntries = events.events
    .filter((event) => event.producers.includes('mcp_worker'))
    .flatMap((event) => event.symbols.typescript.map((symbol) => ({ symbol, name: event.name, description: event.description })))
    .sort((left, right) => compareCodePoints(left.symbol, right.symbol))
  const goEntries = events.events
    .flatMap((event) => event.symbols.go.map((symbol) => ({ symbol, name: event.name, description: event.description, event })))
    .sort((left, right) => compareCodePoints(left.symbol, right.symbol))
  const typescript = renderFrontendTypeScript(tsEntries)
  const mcpTypescript = renderTypeScriptConstants('MCP_EVENTS', 'McpEvent', mcpEntries)
  const go = gofmt([
    '// Code generated by scripts/generate-posthog-events.ts; DO NOT EDIT.',
    '',
    'package posthog',
    '',
    'type EventName string',
    '',
    'const (',
    ...goEntries.flatMap(({ symbol, name, description }) => [
      ...renderDescription(description, '\t', `${symbol} `),
      `\t${symbol} = "${name}"`,
    ]),
    ')',
    '',
    ...renderGoPropertyTypes(goEntries),
    ...renderGoContracts(goEntries),
  ].join('\n'))
  return { typescript, mcpTypescript, go }
}

const OUTPUTS = {
  typescript: 'apps/web/src/shared/analytics/events.generated.ts',
  mcpTypescript: 'apps/mcp-server/src/analytics/events.generated.ts',
  go: 'backend/internal/integration/posthog/events_generated.go',
} as const

export function verifyGeneratedArtifacts(
  root: string,
  generated: ReturnType<typeof renderGeneratedArtifacts>,
): void {
  for (const [kind, relativePath] of Object.entries(OUTPUTS) as [keyof typeof OUTPUTS, string][]) {
    let current = ''
    try {
      current = readFileSync(resolve(root, relativePath), 'utf8')
    } catch {
      // Report missing artifacts using the same actionable stale-output error.
    }
    if (current !== generated[kind]) fail(`${relativePath} is stale; run bun run generate:posthog`)
  }
}

async function main(): Promise<void> {
  const root = resolve(import.meta.dir, '..')
  const events = parseEventCatalogYaml(readFileSync(resolve(root, 'config/posthog_events.yaml'), 'utf8'))
  const metrics = validateProductMetrics(
    parseProductMetricsYaml(readFileSync(resolve(root, 'config/product_metrics.yaml'), 'utf8')),
    events,
  )
  const generated = renderGeneratedArtifacts(events, metrics)
  const check = process.argv.includes('--check')
  if (check) {
    verifyGeneratedArtifacts(root, generated)
    return
  }
  for (const [kind, relativePath] of Object.entries(OUTPUTS) as [keyof typeof OUTPUTS, string][]) {
    const path = resolve(root, relativePath)
    await Bun.write(path, generated[kind])
  }
}

if (import.meta.main) {
  main().catch((error: unknown) => {
    console.error(error instanceof Error ? error.message : error)
    process.exit(1)
  })
}
