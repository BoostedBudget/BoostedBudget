/**
 * Top Merchants Table
 * Issue #953: Spending Reports Page
 *
 * Sortable table showing top merchants by spending.
 */

import { useState } from 'react'
import type { SpendingByMerchantResponse } from '@boostedbudget/api-client/models'
import { formatCurrencyFull } from '@/shared/charts'
import type { SortField, SortDirection } from '../types'

interface TopMerchantsTableProps {
  data: SpendingByMerchantResponse | null
  loading: boolean
  onMerchantClick?: (merchantName: string) => void
  className?: string
}

export function TopMerchantsTable({
  data,
  loading,
  onMerchantClick,
  className,
}: TopMerchantsTableProps) {
  const [sortField, setSortField] = useState<SortField>('total')
  const [sortDirection, setSortDirection] = useState<SortDirection>('desc')

  const handleSort = (field: SortField) => {
    if (field === sortField) {
      setSortDirection((d) => (d === 'asc' ? 'desc' : 'asc'))
    } else {
      setSortField(field)
      setSortDirection('desc')
    }
  }

  const merchants = [...(data?.merchants ?? [])].sort((a, b) => {
    const aVal = a[sortField]
    const bVal = b[sortField]
    const cmp = typeof aVal === 'string' ? aVal.localeCompare(bVal as string) : (aVal as number) - (bVal as number)
    return sortDirection === 'asc' ? cmp : -cmp
  })

  if (loading) {
    return (
      <div className={className} aria-busy="true" aria-label="Loading merchants" data-testid="merchants-table">
        <div className="h-48 animate-pulse rounded-lg bg-muted" />
      </div>
    )
  }

  if (merchants.length === 0) {
    return (
      <div className={className} role="status" data-testid="merchants-table">
        <p className="text-sm text-muted-foreground">No merchant data for this period.</p>
      </div>
    )
  }

  const sortIndicator = (field: SortField) => {
    if (sortField !== field) return null
    return sortDirection === 'asc' ? ' ↑' : ' ↓'
  }

  const getAriaSort = (field: SortField) => {
    if (sortField !== field) return 'none'
    return sortDirection === 'asc' ? 'ascending' : 'descending'
  }

  const renderHeaderCell = (field: SortField, label: string, isRightAligned = false) => (
    <th
      className={`cursor-pointer pb-2 pr-4 font-medium hover:text-foreground ${isRightAligned ? 'text-right' : ''}`}
      onClick={() => handleSort(field)}
      aria-sort={getAriaSort(field)}
    >
      {label}{sortIndicator(field)}
    </th>
  )

  return (
    <div className={className} data-testid="merchants-table">
      <table className="w-full text-sm" role="table">
        <thead>
          <tr className="border-b border-border text-left text-muted-foreground">
            {renderHeaderCell('merchant_name', 'Merchant')}
            {renderHeaderCell('total', 'Total', true)}
            {renderHeaderCell('transaction_count', 'Transactions', true)}
            {renderHeaderCell('percentage', '%', true)}
          </tr>
        </thead>
        <tbody>
          {merchants.map((merchant) => (
            <tr
              key={merchant.merchant_name}
              className={`border-b border-border/50 ${onMerchantClick ? 'cursor-pointer hover:bg-muted/50' : ''}`}
              onClick={() => onMerchantClick?.(merchant.merchant_name)}
            >
              <td className="py-2 pr-4 font-medium">{merchant.merchant_name}</td>
              <td className="py-2 pr-4 text-right" data-ph-mask>
                {formatCurrencyFull(merchant.total)}
              </td>
              <td className="py-2 pr-4 text-right text-muted-foreground">
                {merchant.transaction_count}
              </td>
              <td className="py-2 text-right text-muted-foreground">
                {merchant.percentage.toFixed(1)}%
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
