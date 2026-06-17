/**
 * Widget settings normalization
 * Issue #1297: Investment widgets — per-widget settings persistence
 *
 * The backend stores settings as an opaque {[key:string]: unknown} blob (map[string]any in Go).
 * This module is the FE boundary that converts that raw value into typed, default-filled settings.
 * Widgets MUST call normalizeWidgetSettings() before reading any settings field — never access raw.
 */

import type {
  WidgetType,
  WidgetSettings,
  PortfolioHoldingsSettings,
  AssetAllocationSettings,
} from '../types'

const PORTFOLIO_HOLDINGS_DEFAULTS: PortfolioHoldingsSettings = {
  topN: 5,
  sortBy: 'value',
}

const ASSET_ALLOCATION_DEFAULTS: AssetAllocationSettings = {
  groupBy: 'security_type',
}

const VALID_TOP_N = new Set([5, 10, 20])
const VALID_SORT_BY = new Set(['value', 'day_change'])
const VALID_GROUP_BY = new Set(['security_type', 'asset_class', 'account'])

/** Coerce raw value to object, returning {} for non-objects. */
function asObject(raw: unknown): Record<string, unknown> {
  return raw !== null && typeof raw === 'object' && !Array.isArray(raw)
    ? (raw as Record<string, unknown>)
    : {}
}

function normalizePortfolioHoldings(
  raw: Record<string, unknown>
): PortfolioHoldingsSettings {
  const rawTopN = raw['topN']
  const topN =
    typeof rawTopN === 'number' && VALID_TOP_N.has(rawTopN)
      ? (rawTopN as 5 | 10 | 20)
      : PORTFOLIO_HOLDINGS_DEFAULTS.topN

  const rawSortBy = raw['sortBy']
  const sortBy =
    typeof rawSortBy === 'string' && VALID_SORT_BY.has(rawSortBy)
      ? (rawSortBy as 'value' | 'day_change')
      : PORTFOLIO_HOLDINGS_DEFAULTS.sortBy

  return { topN, sortBy }
}

function normalizeAssetAllocation(
  raw: Record<string, unknown>
): AssetAllocationSettings {
  const rawGroupBy = raw['groupBy']
  const groupBy =
    typeof rawGroupBy === 'string' && VALID_GROUP_BY.has(rawGroupBy)
      ? (rawGroupBy as 'security_type' | 'asset_class' | 'account')
      : ASSET_ALLOCATION_DEFAULTS.groupBy

  return { groupBy }
}

/**
 * Normalize per-widget settings from the raw backend blob into typed values.
 *
 * - For widgets with no configurable settings, returns an empty object `{}`.
 * - For configurable widgets, fills in defaults for missing/invalid fields.
 * - Never throws regardless of input shape.
 */
export function normalizeWidgetSettings(
  type: WidgetType,
  raw: Record<string, unknown> | undefined
): WidgetSettings {
  const obj = asObject(raw)
  switch (type) {
    case 'portfolio-holdings':
      return normalizePortfolioHoldings(obj)
    case 'asset-allocation':
      return normalizeAssetAllocation(obj)
    default:
      return {} as WidgetSettings
  }
}
