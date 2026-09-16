import { describe, expect, test } from 'bun:test'
import { readFileSync } from 'node:fs'

import { parseDocument } from 'yaml'

import {
  parsePostHogRolloutYaml,
  validatePostHogRollout,
} from './validate-posthog-rollout'
import {
  parseEventCatalogYaml,
  parseProductMetricsYaml,
  validateProductMetrics,
} from './generate-posthog-events'

const events = parseEventCatalogYaml(readFileSync('config/posthog_events.yaml', 'utf8'))
const metrics = validateProductMetrics(
  parseProductMetricsYaml(readFileSync('config/product_metrics.yaml', 'utf8')),
  events,
)

function validRollout(): Record<string, unknown> {
  return {
    schema_version: 1,
    project_isolation: {
      required: true,
      environments: [
        {
          name: 'staging',
          project_ref: 'boostedbudget-staging',
          app_origin: 'https://staging.boostedbudget.com',
          ingest_host: 'https://us.i.posthog.com',
          ui_host: 'https://us.posthog.com',
        },
        {
          name: 'production',
          project_ref: 'boostedbudget-production',
          app_origin: 'https://boostedbudget.com',
          ingest_host: 'https://us.i.posthog.com',
          ui_host: 'https://us.posthog.com',
        },
      ],
    },
    project_controls: {
      timezone: 'UTC',
      discard_client_ip: true,
      allowed_domains_exact: true,
      autocapture: false,
      session_replay: false,
      web_analytics: false,
      error_tracking: false,
      experiments: false,
      feature_flags: false,
      logs: false,
      tracing: false,
      support: true,
      project_rendered_widget: false,
      surveys: true,
    },
    runtime_authority: {
      analytics: 'consent_required',
      support: 'authenticated_user_request',
      surveys: 'authenticated_and_consent_required',
    },
    exclusion_cohort: {
      name: 'boostedbudget_internal_and_test',
      type: 'static',
      identifier: 'clerk_distinct_id',
      owner: 'Product Analytics',
      update_trigger: 'staff_or_test_identity_change',
      review_cadence: 'quarterly',
      identifiers_committed: false,
    },
    kill_switches: [
      { product: 'all_browser_products', mechanism: 'remove_project_token_and_redeploy' },
      { product: 'backend_analytics', mechanism: 'remove_project_token_and_restart' },
      { product: 'support', mechanism: 'disable_in_posthog_project' },
      { product: 'surveys', mechanism: 'archive_all_surveys' },
    ],
    ingestion_scrubbing: {
      mode: 'operator_managed_posthog_transformation',
      action: 'remove_disallowed_properties_from_catalog_events',
      allowed_event_source: 'config/posthog_events.yaml',
      property_policy: 'catalog_properties_plus_required_posthog_metadata',
      deployment_order: 'staging_then_production',
    },
    dashboards: [
      {
        id: 'mcp_product_health',
        status: 'ready_to_create',
        metric_ids: ['mcp_usage_and_outcomes'],
      },
      {
        id: 'core_product_health',
        status: 'blocked',
        metric_ids: metrics.metrics
          .filter(metric => metric.availability === 'planned')
          .map(metric => metric.id),
      },
    ],
    native_product_views: [
      { product: 'support', status: 'manual', authenticated_only: true },
      { product: 'surveys', status: 'manual', authenticated_only: true },
    ],
    alerts: [
      {
        id: 'mcp_success_rate_degradation',
        metric_id: 'mcp_usage_and_outcomes',
        status: 'deferred_until_baseline',
        check_interval: 'weekly',
        minimum_history_periods: 8,
        minimum_denominator: 100,
        detector: 'mad',
        simulate_before_enable: true,
      },
    ],
    coverage_gaps: [
      { id: 'remote_configuration_unapplied', severity: 'blocking', status: 'open' },
      { id: 'legacy_event_schemas_unreviewed', severity: 'measurement', status: 'open' },
    ],
  }
}

describe('PostHog rollout validation', () => {
  test('accepts a separate-project rollout whose ready dashboards use only live metrics', () => {
    const rollout = validatePostHogRollout(validRollout(), metrics)
    expect(rollout.dashboards[0].metric_ids).toEqual(['mcp_usage_and_outcomes'])
  })

  test('rejects shared, wildcard, or non-HTTPS environment boundaries', () => {
    const shared = validRollout() as any
    shared.project_isolation.environments[1].project_ref = 'boostedbudget-staging'
    expect(() => validatePostHogRollout(shared, metrics)).toThrow(/separate PostHog project/i)

    const wildcard = validRollout() as any
    wildcard.project_isolation.environments[0].app_origin = 'https://*.boostedbudget.com'
    expect(() => validatePostHogRollout(wildcard, metrics)).toThrow(/exact HTTPS origin/i)
  })

  test('rejects enabling deferred telemetry products', () => {
    for (const product of ['session_replay', 'web_analytics', 'error_tracking', 'experiments', 'feature_flags', 'logs', 'tracing']) {
      const rollout = validRollout() as any
      rollout.project_controls[product] = true
      expect(() => validatePostHogRollout(rollout, metrics)).toThrow(product)
    }
  })

  test('rejects a ready dashboard backed by a planned metric', () => {
    const rollout = validRollout() as any
    rollout.dashboards[1].status = 'ready_to_create'
    expect(() => validatePostHogRollout(rollout, metrics)).toThrow(/planned metric/i)
  })

  test('requires every live metric to be represented by a ready dashboard', () => {
    const rollout = validRollout() as any
    rollout.dashboards.shift()
    expect(() => validatePostHogRollout(rollout, metrics)).toThrow(/live metric.*mcp_usage_and_outcomes/i)
  })

  test('requires authenticated-only native Support and Surveys views', () => {
    const rollout = validRollout() as any
    rollout.native_product_views[0].authenticated_only = false
    expect(() => validatePostHogRollout(rollout, metrics)).toThrow(/support.*authenticated/i)
  })

  test('rejects the PostHog-rendered Support launcher even when Support remains enabled', () => {
    const rollout = validRollout() as any
    rollout.project_controls.project_rendered_widget = true
    expect(() => validatePostHogRollout(rollout, metrics)).toThrow(/project-rendered.*launcher/i)
  })

  test('rejects noisy alerts without a baseline, sample floor, and simulation', () => {
    const rollout = validRollout() as any
    rollout.alerts[0].minimum_history_periods = 2
    expect(() => validatePostHogRollout(rollout, metrics)).toThrow(/history periods/i)

    rollout.alerts[0].minimum_history_periods = 8
    rollout.alerts[0].minimum_denominator = 0
    expect(() => validatePostHogRollout(rollout, metrics)).toThrow(/denominator/i)
  })

  test('requires a privately maintained static exclusion cohort', () => {
    const rollout = validRollout() as any
    rollout.exclusion_cohort.identifiers_committed = true
    expect(() => validatePostHogRollout(rollout, metrics)).toThrow(/identifiers.*must not be committed/i)
  })

  test('keeps unapplied remote configuration explicit as a blocking gap', () => {
    const rollout = validRollout() as any
    rollout.coverage_gaps = []
    expect(() => validatePostHogRollout(rollout, metrics)).toThrow(/remote_configuration_unapplied/i)
  })

  test('validates the checked-in rollout manifest', () => {
    const rollout = parsePostHogRolloutYaml(
      readFileSync('config/posthog_rollout.yaml', 'utf8'),
      metrics,
    )
    expect(rollout.project_isolation.environments.map(environment => environment.name)).toEqual([
      'staging',
      'production',
    ])
  })

  test('wires rollout validation and its inputs into the PostHog CI gate', () => {
    const packageJson = JSON.parse(readFileSync('package.json', 'utf8')) as {
      scripts: Record<string, string>
    }
    expect(packageJson.scripts['validate:posthog']).toContain('validate-posthog-rollout.ts')

    const workflow = parseDocument(readFileSync('.github/workflows/test.yml', 'utf8')).toJS() as any
    const filterStep = workflow.jobs['detect-changes'].steps.find((step: any) => step.id === 'filter')
    const filters = parseDocument(filterStep.with.filters).toJS() as Record<string, string[]>
    expect(filters.posthog_contract).toContain('config/posthog_rollout.yaml')
    expect(filters.posthog_contract).toContain('scripts/validate-posthog-rollout.ts')
    expect(filters.posthog_contract).toContain('scripts/validate-posthog-rollout.test.ts')
  })
})
