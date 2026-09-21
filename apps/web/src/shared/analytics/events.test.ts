import { describe, expect, test } from 'bun:test'
import { ANALYTICS_EVENTS, normalizeAnalyticsEventPayload } from './events'

describe('ANALYTICS_EVENTS', () => {
  test('has no duplicate event values', () => {
    const values = Object.values(ANALYTICS_EVENTS)
    const uniqueValues = new Set(values)
    expect(uniqueValues.size).toBe(values.length)
  })

  // Issue #1411: public MCP marketing page + landing band
  test('has the MCP marketing/landing analytics event constants', () => {
    expect(ANALYTICS_EVENTS.MCP_PAGE_VIEWED).toBe('mcp_page_viewed')
    expect(ANALYTICS_EVENTS.MCP_BAND_CTA_CLICKED).toBe('mcp_band_cta_clicked')
    expect(ANALYTICS_EVENTS.MCP_PAGE_CTA_CLICKED).toBe('mcp_page_cta_clicked')
    expect(ANALYTICS_EVENTS.MCP_SETUP_TAB_CHANGED).toBe('mcp_setup_tab_changed')
    expect(ANALYTICS_EVENTS.MCP_CONFIG_COPIED).toBe('mcp_config_copied')
  })

  // Issue #1413: Admin subscription override (comp tier apply/remove)
  test('has the admin subscription override analytics event constants', () => {
    expect(ANALYTICS_EVENTS.ADMIN_SUBSCRIPTION_OVERRIDE_APPLIED).toBe('admin_subscription_override_applied')
    expect(ANALYTICS_EVENTS.ADMIN_SUBSCRIPTION_OVERRIDE_REMOVED).toBe('admin_subscription_override_removed')
  })

  // Issue #1685: the persisted incomplete-actuals reasons notice on the budget page
  test('has the budget actuals incomplete reasons notice event', () => {
    expect(ANALYTICS_EVENTS.BUDGET_ACTUALS_INCOMPLETE_REASONS_VIEWED).toBe(
      'budget_actuals_incomplete_reasons_viewed'
    )
  })

  test('accepts a bounded reason count for the notice event and nothing else', () => {
    const event = ANALYTICS_EVENTS.BUDGET_ACTUALS_INCOMPLETE_REASONS_VIEWED

    expect(normalizeAnalyticsEventPayload(event, { reason_count: 2 })).toEqual({ reason_count: 2 })
    expect(normalizeAnalyticsEventPayload(event)).toBeNull()
    expect(normalizeAnalyticsEventPayload(event, { reason_count: 0 })).toBeNull()
    expect(normalizeAnalyticsEventPayload(event, { reason_count: 1.5 })).toBeNull()
    expect(
      normalizeAnalyticsEventPayload(event, {
        reason_count: 1,
        reasons: ['currency_mismatch'],
      })
    ).toBeNull()
    // Catalogued events keep routing through the generated validator.
    expect(normalizeAnalyticsEventPayload('unknown_event', {})).toBeNull()
  })
})
