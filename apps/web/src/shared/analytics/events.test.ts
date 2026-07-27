import { describe, expect, test } from 'bun:test'
import { ANALYTICS_EVENTS } from './events'

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
})
