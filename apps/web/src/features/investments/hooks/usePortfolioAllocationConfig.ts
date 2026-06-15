/**
 * usePortfolioAllocationConfig Hook
 * Issue #1296: localStorage-persisted allocation configuration for the Portfolio tab.
 *
 * Config is persisted under `bb:portfolio-alloc:{clerkUserId}` to keep it
 * user-scoped, matching the same namespacing convention as useTablePreferences.
 *
 * When the user is not authenticated, returns defaults and setConfig is a no-op.
 */

import { useState, useCallback } from 'react'
import { useUser } from '@clerk/react-router'

export interface AllocationConfig {
  /** Comma-value list of additional asset classes to include (e.g. ['cash', 'liabilities']). */
  include: string[]
  /** Bucketing dimension: 'security_type' | 'asset_class' | 'account' */
  group_by: string
  /** Whether to show assets-only or net of liabilities: 'assets' | 'net' */
  basis: string
}

const DEFAULT_CONFIG: AllocationConfig = {
  include: [],
  group_by: 'security_type',
  basis: 'assets',
}

// Valid API parameter values — guards against corrupt or stale localStorage sending
// unknown params to the allocation endpoint.
const VALID_GROUP_BY = new Set(['security_type', 'asset_class', 'account'])
const VALID_BASIS = new Set(['assets', 'net'])
const VALID_INCLUDE = new Set(['cash', 'liabilities'])

function storageKey(userId: string): string {
  return `bb:portfolio-alloc:${userId}`
}

function loadConfig(userId: string): AllocationConfig {
  let raw: string | null
  try {
    raw = localStorage.getItem(storageKey(userId))
  } catch {
    return DEFAULT_CONFIG
  }

  if (raw === null) return DEFAULT_CONFIG

  try {
    const parsed = JSON.parse(raw) as Partial<AllocationConfig>
    return {
      include: Array.isArray(parsed.include)
        ? (parsed.include as unknown[]).filter((v): v is string => typeof v === 'string' && VALID_INCLUDE.has(v))
        : DEFAULT_CONFIG.include,
      group_by:
        typeof parsed.group_by === 'string' && VALID_GROUP_BY.has(parsed.group_by)
          ? parsed.group_by
          : DEFAULT_CONFIG.group_by,
      basis:
        typeof parsed.basis === 'string' && VALID_BASIS.has(parsed.basis)
          ? parsed.basis
          : DEFAULT_CONFIG.basis,
    }
  } catch {
    return DEFAULT_CONFIG
  }
}

function saveConfig(userId: string, config: AllocationConfig): void {
  try {
    localStorage.setItem(storageKey(userId), JSON.stringify(config))
  } catch {
    // ignore storage quota errors
  }
}

/**
 * Returns `{ config, setConfig }` for the holistic allocation view configuration.
 *
 * - Persisted per Clerk user ID in localStorage.
 * - Defaults: include=[], group_by='security_type', basis='assets'.
 * - When unauthenticated: returns defaults, setConfig is a no-op.
 */
export function usePortfolioAllocationConfig(): {
  config: AllocationConfig
  setConfig: (next: AllocationConfig) => void
} {
  const { user } = useUser()
  const userId = user?.id ?? null

  const [config, setConfigState] = useState<AllocationConfig>(() =>
    userId ? loadConfig(userId) : DEFAULT_CONFIG,
  )

  const [prevUserId, setPrevUserId] = useState(userId)
  if (userId !== prevUserId) {
    setPrevUserId(userId)
    setConfigState(userId ? loadConfig(userId) : DEFAULT_CONFIG)
  }

  const setConfig = useCallback(
    (next: AllocationConfig) => {
      if (!userId) return
      setConfigState(next)
      saveConfig(userId, next)
    },
    [userId],
  )

  return { config, setConfig }
}
