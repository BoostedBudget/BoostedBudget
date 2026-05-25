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

export interface SpendingMerchant {
  merchant_id?: string | null
  merchant_name: string
  total: number
  transaction_count: number
  percentage: number
}

export type SortField = 'merchant_name' | 'total' | 'transaction_count' | 'percentage'
export type SortDirection = 'asc' | 'desc'
