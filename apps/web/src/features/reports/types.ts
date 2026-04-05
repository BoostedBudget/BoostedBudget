/**
 * Reports Feature Types
 * Issue #953: Spending Reports Page
 */

import type { DatePreset } from '@/shared/dates'

export interface ReportsFilters {
  preset: DatePreset
  startDate?: string
  endDate?: string
}

export interface SpendingCategory {
  category: string
  display_name: string
  total: number
  percentage: number
  transaction_count: number
}

export interface SpendingMerchant {
  merchant_name: string
  total: number
  transaction_count: number
  percentage: number
}

export interface CategorySpending {
  category: string
  display_name: string
  total: number
}

export interface SpendingTrendPeriod {
  period_start: string
  period_end: string
  categories: CategorySpending[]
  total: number
}

export type SortField = 'merchant_name' | 'total' | 'transaction_count' | 'percentage'
export type SortDirection = 'asc' | 'desc'
