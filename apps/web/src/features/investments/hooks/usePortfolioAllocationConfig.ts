import { useState, useCallback } from 'react'
import { useUser } from '@clerk/react-router'
import type { GetPortfolioAllocationBreakdownGroupBy } from '@boostedbudget/api-client/models/getPortfolioAllocationBreakdownGroupBy'
import type { GetPortfolioAllocationBreakdownBasis } from '@boostedbudget/api-client/models/getPortfolioAllocationBreakdownBasis'

export type { GetPortfolioAllocationBreakdownGroupBy, GetPortfolioAllocationBreakdownBasis }

export interface AllocationConfig {
  include: string[]
  group_by: GetPortfolioAllocationBreakdownGroupBy
  basis: GetPortfolioAllocationBreakdownBasis
}

const DEFAULT_CONFIG: AllocationConfig = {
  include: [],
  group_by: 'security_type',
  basis: 'assets',
}

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

export function usePortfolioAllocationConfig() {
  const { user } = useUser()
  const userId = user?.id

  const [config, setConfigState] = useState<AllocationConfig>(() =>
    userId ? loadConfig(userId) : DEFAULT_CONFIG,
  )

  const [prevUserId, setPrevUserId] = useState(userId)
  if (userId !== prevUserId) {
    setPrevUserId(userId)
    if (userId) {
      setConfigState(loadConfig(userId))
    } else {
      setConfigState(DEFAULT_CONFIG)
    }
  }

  const setConfig = useCallback(
    (next: AllocationConfig) => {
      if (userId) {
        setConfigState(next)
        saveConfig(userId, next)
      }
    },
    [userId],
  )

  return { config, setConfig }
}
