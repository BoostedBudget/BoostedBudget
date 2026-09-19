import { readFileSync } from 'node:fs'

import { parseDocument, visit } from 'yaml'

import {
  parseEventCatalogYaml,
  parseProductMetricsYaml,
  type ProductMetricCatalog,
  validateProductMetrics,
} from './generate-posthog-events'

type UnknownRecord = Record<string, unknown>

export interface RolloutEnvironment {
  name: 'staging' | 'production'
  project_ref: string
  app_origin: string
  ingest_host: string
  ui_host: string
}

export interface RolloutDashboard {
  id: string
  status: 'ready_to_create' | 'blocked'
  metric_ids: string[]
}

export interface PostHogRollout {
  schema_version: 1
  project_isolation: {
    required: true
    environments: RolloutEnvironment[]
  }
  project_controls: Record<string, boolean | 'UTC'>
  session_replay_target: boolean
  enablement_gates: Record<string, string>
  identified_diagnostics_opt_out_certification: { status: 'blocked' | 'go' }
  runtime_authority: Record<string, string>
  exclusion_cohort: Record<string, unknown>
  kill_switches: Array<{ product: string; mechanism: string }>
  ingestion_scrubbing: Record<string, string>
  dashboards: RolloutDashboard[]
  native_product_views: Array<{ product: string; status: string; authenticated_only: boolean }>
  alerts: Array<Record<string, unknown>>
  coverage_gaps: Array<{ id: string; severity: string; status: string }>
}

const DEFERRED_PRODUCTS = [
  'error_tracking',
  'experiments',
  'feature_flags',
  'logs',
  'tracing',
] as const

const REQUIRED_REPLAY_GATE = 'staging_inspection_required'
const REPLAY_RUNBOOK_PATH = 'docs/operations/posthog-rollout.md'
const REPLAY_RUNBOOK_HEADING = '## Replay enablement gate'
const IDENTIFIED_DIAGNOSTICS_CERTIFICATION_STATUSES = new Set(['blocked', 'go'])
const REQUIRED_INGESTION_SURVIVAL_GAP_ID = 'unset_ingestion_survival_unverified'

const REQUIRED_KILL_SWITCHES = new Map([
  ['all_browser_products', 'remove_project_token_and_redeploy'],
  ['backend_analytics', 'remove_project_token_and_restart'],
  ['support', 'disable_in_posthog_project'],
  ['surveys', 'archive_all_surveys'],
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

function string(value: unknown, label: string): string {
  if (typeof value !== 'string' || value.trim() === '') fail(`${label} must be a non-empty string`)
  return value
}

function stringArray(value: unknown, label: string): string[] {
  if (!Array.isArray(value) || value.some(item => typeof item !== 'string' || item === '')) {
    fail(`${label} must be an array of non-empty strings`)
  }
  return value as string[]
}

function exactHttpsOrigin(value: unknown, label: string): string {
  const origin = string(value, label)
  let parsed: URL
  try {
    parsed = new URL(origin)
  } catch {
    fail(`${label} must be an exact HTTPS origin`)
  }
  if (
    parsed.protocol !== 'https:' ||
    parsed.origin !== origin ||
    parsed.username !== '' ||
    parsed.password !== '' ||
    parsed.hostname.includes('*')
  ) {
    fail(`${label} must be an exact HTTPS origin`)
  }
  return origin
}

function parseStrictYaml(source: string): unknown {
  const document = parseDocument(source, { uniqueKeys: true, maxAliasCount: 0 })
  if (document.errors.length > 0) fail(`PostHog rollout: ${document.errors[0].message}`)
  if (document.warnings.length > 0) fail(`PostHog rollout: ${document.warnings[0].message}`)
  visit(document, {
    Alias() {
      fail('PostHog rollout must not use YAML aliases')
    },
    Node(_key, node) {
      if (node.tag !== undefined || ('anchor' in node && node.anchor !== undefined)) {
        fail('PostHog rollout must not use tags or anchors')
      }
    },
  })
  return document.toJS({ maxAliasCount: 0 })
}

function validateEnvironments(root: UnknownRecord): RolloutEnvironment[] {
  const isolation = record(root.project_isolation, 'project_isolation')
  if (isolation.required !== true || !Array.isArray(isolation.environments)) {
    fail('project_isolation must require environment-isolated projects')
  }
  const environments = isolation.environments.map((raw, index) => {
    const environment = record(raw, `project_isolation.environments[${index}]`)
    const name = string(environment.name, `environment ${index} name`)
    if (!['staging', 'production'].includes(name)) fail(`unknown rollout environment ${name}`)
    return {
      name: name as RolloutEnvironment['name'],
      project_ref: string(environment.project_ref, `${name}.project_ref`),
      app_origin: exactHttpsOrigin(environment.app_origin, `${name}.app_origin`),
      ingest_host: exactHttpsOrigin(environment.ingest_host, `${name}.ingest_host`),
      ui_host: exactHttpsOrigin(environment.ui_host, `${name}.ui_host`),
    }
  })
  if (environments.map(environment => environment.name).join(',') !== 'staging,production') {
    fail('project isolation must define staging then production')
  }
  if (new Set(environments.map(environment => environment.project_ref)).size !== environments.length) {
    fail('staging and production must use a separate PostHog project')
  }
  return environments
}

function validateProjectControls(root: UnknownRecord): void {
  const controls = record(root.project_controls, 'project_controls')
  if (controls.timezone !== 'UTC') fail('project_controls.timezone must be UTC')
  for (const enabled of ['discard_client_ip', 'allowed_domains_exact', 'support', 'surveys']) {
    if (controls[enabled] !== true) fail(`project_controls.${enabled} must be true`)
  }
  if (controls.project_rendered_widget !== false) {
    fail('project-rendered Support launcher must remain disabled; the authenticated app control is authoritative')
  }
  if (controls.autocapture !== false) fail('project_controls.autocapture must be false')
  if (controls.web_analytics !== true) fail('project_controls.web_analytics must be true')
  if (controls.heatmaps !== false) fail('project_controls.heatmaps must be false')
  for (const product of DEFERRED_PRODUCTS) {
    if (controls[product] !== false) fail(`project_controls.${product} must remain false for #1651 or later`)
  }
}

function replayRunbookSource(): string {
  try {
    return readFileSync(REPLAY_RUNBOOK_PATH, 'utf8')
  } catch {
    fail(`cannot read ${REPLAY_RUNBOOK_PATH}; the replay enablement gate must be documented there`)
  }
}

export function validateReplayEnablement(root: UnknownRecord, runbookSource: string): void {
  if (typeof root.session_replay_target !== 'boolean') {
    fail('session_replay_target must be a boolean')
  }
  const gates = record(root.enablement_gates, 'enablement_gates')
  for (const [name, value] of Object.entries(gates)) {
    if (typeof value !== 'string' || value.trim() === '') {
      fail(`enablement_gates.${name} must be a non-empty string`)
    }
  }
  const replayGate = gates.session_replay
  if (root.session_replay_target === true && replayGate !== REQUIRED_REPLAY_GATE) {
    fail(`session_replay_target requires enablement_gates.session_replay = ${REQUIRED_REPLAY_GATE}`)
  }
  if (replayGate !== undefined && !runbookSource.includes(REPLAY_RUNBOOK_HEADING)) {
    fail(`enablement_gates.session_replay requires the "${REPLAY_RUNBOOK_HEADING}" section in ${REPLAY_RUNBOOK_PATH}`)
  }
}

function identifiedDiagnosticsCertificationStatus(root: UnknownRecord): string {
  const certification = record(
    root.identified_diagnostics_opt_out_certification,
    'identified_diagnostics_opt_out_certification',
  )
  const status = string(
    certification.status,
    'identified_diagnostics_opt_out_certification.status',
  )
  if (!IDENTIFIED_DIAGNOSTICS_CERTIFICATION_STATUSES.has(status)) {
    fail("identified_diagnostics_opt_out_certification.status must be 'blocked' or 'go'")
  }
  return status
}

function validateAuthorityAndCohort(root: UnknownRecord): void {
  const authority = record(root.runtime_authority, 'runtime_authority')
  const expectedAuthority: Record<string, string> = {
    analytics: 'consent_required',
    support: 'authenticated_user_request',
    surveys: 'authenticated_and_consent_required',
  }
  for (const [product, expected] of Object.entries(expectedAuthority)) {
    if (authority[product] !== expected) fail(`${product} runtime authority must be ${expected}`)
  }

  const cohort = record(root.exclusion_cohort, 'exclusion_cohort')
  if (
    cohort.type !== 'static' ||
    cohort.identifier !== 'clerk_distinct_id' ||
    cohort.identifiers_committed !== false
  ) {
    fail('exclusion cohort must be static, use Clerk distinct IDs, and identifiers must not be committed')
  }
  for (const key of ['name', 'owner', 'update_trigger', 'review_cadence']) {
    string(cohort[key], `exclusion_cohort.${key}`)
  }
}

function validateKillSwitches(root: UnknownRecord): void {
  if (!Array.isArray(root.kill_switches)) fail('kill_switches must be a list')
  const switches = new Map(root.kill_switches.map((raw, index) => {
    const killSwitch = record(raw, `kill_switches[${index}]`)
    return [string(killSwitch.product, 'kill switch product'), string(killSwitch.mechanism, 'kill switch mechanism')]
  }))
  for (const [product, mechanism] of REQUIRED_KILL_SWITCHES) {
    if (switches.get(product) !== mechanism) fail(`${product} must have the documented ${mechanism} kill switch`)
  }
}

function validateScrubbing(root: UnknownRecord): void {
  const scrubbing = record(root.ingestion_scrubbing, 'ingestion_scrubbing')
  const expected: Record<string, string> = {
    mode: 'operator_managed_posthog_transformation',
    action: 'remove_disallowed_properties_from_catalog_events',
    allowed_event_source: 'config/posthog_events.yaml',
    property_policy: 'catalog_properties_plus_required_posthog_metadata',
    deployment_order: 'staging_then_production',
  }
  for (const [key, value] of Object.entries(expected)) {
    if (scrubbing[key] !== value) fail(`ingestion_scrubbing.${key} must be ${value}`)
  }
}

function validateDashboards(
  root: UnknownRecord,
  metrics: ProductMetricCatalog,
): RolloutDashboard[] {
  if (!Array.isArray(root.dashboards)) fail('dashboards must be a list')
  const availability = new Map(metrics.metrics.map(metric => [metric.id, metric.availability]))
  const dashboards = root.dashboards.map((raw, index) => {
    const dashboard = record(raw, `dashboards[${index}]`)
    const id = string(dashboard.id, `dashboards[${index}].id`)
    const status = string(dashboard.status, `${id}.status`)
    if (!['ready_to_create', 'blocked'].includes(status)) fail(`${id} has invalid dashboard status`)
    const metricIds = stringArray(dashboard.metric_ids, `${id}.metric_ids`)
    if (metricIds.length === 0 || new Set(metricIds).size !== metricIds.length) {
      fail(`${id} must reference unique metrics`)
    }
    for (const metricId of metricIds) {
      const metricAvailability = availability.get(metricId)
      if (!metricAvailability) fail(`${id} references unknown metric ${metricId}`)
      if (status === 'ready_to_create' && metricAvailability === 'planned') {
        fail(`${id} cannot be ready with planned metric ${metricId}`)
      }
      if (status === 'blocked' && metricAvailability === 'live') {
        fail(`${id} must not block live metric ${metricId}`)
      }
    }
    return { id, status: status as RolloutDashboard['status'], metric_ids: metricIds }
  })

  const representedMetrics = new Map<string, RolloutDashboard['status']>()
  for (const dashboard of dashboards) {
    for (const metricId of dashboard.metric_ids) {
      if (representedMetrics.has(metricId)) fail(`metric ${metricId} appears on multiple rollout dashboards`)
      representedMetrics.set(metricId, dashboard.status)
    }
  }
  for (const metric of metrics.metrics) {
    const expectedStatus = metric.availability === 'live' ? 'ready_to_create' : 'blocked'
    if (representedMetrics.get(metric.id) !== expectedStatus) {
      fail(`${metric.availability} metric ${metric.id} must be represented by a ${expectedStatus} dashboard`)
    }
  }
  return dashboards
}

function validateNativeViews(root: UnknownRecord): void {
  if (!Array.isArray(root.native_product_views)) fail('native_product_views must be a list')
  const views = new Map(root.native_product_views.map((raw, index) => {
    const view = record(raw, `native_product_views[${index}]`)
    return [string(view.product, 'native product'), view]
  }))
  for (const product of ['support', 'surveys']) {
    const view = views.get(product)
    if (!view || view.authenticated_only !== true) fail(`${product} native view must be authenticated only`)
    if (view.status !== 'manual') fail(`${product} native view must document manual PostHog setup`)
  }
}

function validateAlerts(root: UnknownRecord, metrics: ProductMetricCatalog): void {
  if (!Array.isArray(root.alerts)) fail('alerts must be a list')
  const liveMetrics = new Set(metrics.metrics.filter(metric => metric.availability === 'live').map(metric => metric.id))
  for (const [index, raw] of root.alerts.entries()) {
    const alert = record(raw, `alerts[${index}]`)
    const id = string(alert.id, `alerts[${index}].id`)
    if (!liveMetrics.has(string(alert.metric_id, `${id}.metric_id`))) fail(`${id} must use a live metric`)
    if (alert.status !== 'deferred_until_baseline') fail(`${id} must remain deferred until baseline`)
    if (alert.check_interval !== 'weekly') fail(`${id} must use a weekly check interval`)
    if (typeof alert.minimum_history_periods !== 'number' || alert.minimum_history_periods < 8) {
      fail(`${id} must require at least 8 history periods`)
    }
    if (typeof alert.minimum_denominator !== 'number' || alert.minimum_denominator < 100) {
      fail(`${id} must require a denominator of at least 100`)
    }
    if (alert.detector !== 'mad' || alert.simulate_before_enable !== true) {
      fail(`${id} must use a simulated MAD detector`)
    }
  }
}

function validateCoverageGaps(root: UnknownRecord, certificationStatus: string): void {
  if (!Array.isArray(root.coverage_gaps)) fail('coverage_gaps must be a list')
  const gaps = root.coverage_gaps.map((raw, index) => {
    const gap = record(raw, `coverage_gaps[${index}]`)
    return {
      id: string(gap.id, `coverage_gaps[${index}].id`),
      severity: string(gap.severity, `coverage_gaps[${index}].severity`),
      status: string(gap.status, `coverage_gaps[${index}].status`),
    }
  })
  if (!gaps.some(gap => gap.id === 'remote_configuration_unapplied' && gap.status === 'open')) {
    fail('coverage_gaps must keep remote_configuration_unapplied open until operator verification')
  }
  const survivalGap = gaps.find(gap => gap.id === REQUIRED_INGESTION_SURVIVAL_GAP_ID)
  if (!survivalGap) {
    fail(`coverage_gaps must include ${REQUIRED_INGESTION_SURVIVAL_GAP_ID} until the staging opt-out-survival check passes (fail-closed)`)
  }
  if (certificationStatus === 'go' && survivalGap.status === 'open') {
    fail('identified_diagnostics_opt_out_certification cannot be go while unset_ingestion_survival_unverified is open')
  }
}

export function validatePostHogRollout(
  input: unknown,
  metrics: ProductMetricCatalog,
): PostHogRollout {
  const root = record(input, 'PostHog rollout')
  if (root.schema_version !== 1) fail('PostHog rollout must use schema_version 1')
  const environments = validateEnvironments(root)
  validateProjectControls(root)
  validateReplayEnablement(root, replayRunbookSource())
  const certificationStatus = identifiedDiagnosticsCertificationStatus(root)
  validateAuthorityAndCohort(root)
  validateKillSwitches(root)
  validateScrubbing(root)
  const dashboards = validateDashboards(root, metrics)
  validateNativeViews(root)
  validateAlerts(root, metrics)
  validateCoverageGaps(root, certificationStatus)
  const rollout = input as PostHogRollout
  rollout.project_isolation.environments = environments
  rollout.dashboards = dashboards
  return rollout
}

export function parsePostHogRolloutYaml(
  source: string,
  metrics: ProductMetricCatalog,
): PostHogRollout {
  return validatePostHogRollout(parseStrictYaml(source), metrics)
}

if (import.meta.main) {
  const events = parseEventCatalogYaml(readFileSync('config/posthog_events.yaml', 'utf8'))
  const metrics = validateProductMetrics(
    parseProductMetricsYaml(readFileSync('config/product_metrics.yaml', 'utf8')),
    events,
  )
  parsePostHogRolloutYaml(readFileSync('config/posthog_rollout.yaml', 'utf8'), metrics)
  console.log('PostHog rollout manifest is valid')
}
