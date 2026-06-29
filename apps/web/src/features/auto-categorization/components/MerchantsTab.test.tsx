/**
 * MerchantsTab Tests
 * Issue #957: Merchant Name Handling — per-user overrides + merge/dedupe UI
 * TDD RED → GREEN
 */

import { describe, it, expect, mock, beforeEach } from 'bun:test'
import React from 'react'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import type { MerchantSummary } from '@boostedbudget/api-client/models/merchantSummary'
import {
  mockUseGetMerchants,
  mockUseGetMerchantDuplicateSuggestions,
  mockSetMerchantOverrideMutateAsync,
  mockUseSetMerchantOverride,
  mockUseClearMerchantOverride,
  mockClearMerchantOverrideMutateAsync,
  mockMergeMerchantsMutateAsync,
  mockUseMergeMerchants,
} from '@/test/mocks/merchants-api'

mock.module('@/shared/components/DataTable', () => ({
  DataTable: ({ data, columns, rowActions, emptyState, maskedColumnIds }: {
    data: unknown[]
    columns?: { id: string; render?: (row: unknown) => React.ReactNode }[]
    rowActions?: (row: unknown) => React.ReactNode
    emptyState?: React.ReactNode
    maskedColumnIds?: string[]
  }) => (
    <div>
      {data.length === 0 && <>{emptyState}</>}
      {data.map((row: unknown, i: number) => {
        const merchant = row as MerchantSummary
        const spendCol = columns?.find((c) => c.id === 'total_spend')
        const isMasked = maskedColumnIds?.includes('total_spend')
        return (
          <div key={i} data-testid={`merchant-row-${i}`}>
            <span data-testid={`merchant-name-${i}`}>{merchant.effective_display_name}</span>
            <span data-testid={`merchant-txn-count-${i}`}>{merchant.transaction_count}</span>
            {spendCol && (
              <td
                data-testid={`spend-cell-${i}`}
                {...(isMasked ? { 'data-ph-mask': '' } : {})}
              >
                {spendCol.render?.(row)}
              </td>
            )}
            {rowActions?.(row)}
          </div>
        )
      })}
    </div>
  ),
  useTablePreferences: () => ({
    columnVisibility: {},
    onColumnVisibilityChange: mock(() => {}),
    columnOrder: [],
    onColumnOrderChange: mock(() => {}),
  }),
}))

import { MerchantsTab } from './MerchantsTab'

function createWrapper() {
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })}>
      {children}
    </QueryClientProvider>
  )
}

const merchant1: MerchantSummary = {
  merchant_id: 'merch-1',
  effective_display_name: 'Whole Foods',
  normalized_name: 'Whole Foods',
  raw_variants: ['WHOLEFDSMRKT #123', 'WHOLE FOODS MARKET'],
  total_spend: 450.50,
  transaction_count: 12,
  dominant_category: 'FOOD_AND_DRINK',
  has_override: false,
  is_merged: false,
}

const merchant2: MerchantSummary = {
  merchant_id: 'merch-2',
  effective_display_name: 'My Coffee Shop',
  normalized_name: 'Coffee Shop',
  raw_variants: ['SQ *COFFEE SHOP 12B'],
  total_spend: 8750,
  transaction_count: 7,
  dominant_category: 'FOOD_AND_DRINK',
  has_override: true,
  is_merged: false,
}

describe('MerchantsTab', () => {
  beforeEach(() => {
    // Clear all mocks
    mockUseGetMerchants.mockClear()
    mockUseGetMerchantDuplicateSuggestions.mockClear()
    mockUseSetMerchantOverride.mockClear()
    mockUseClearMerchantOverride.mockClear()
    mockUseMergeMerchants.mockClear()
    mockSetMerchantOverrideMutateAsync.mockClear()
    mockClearMerchantOverrideMutateAsync.mockClear()
    mockMergeMerchantsMutateAsync.mockClear()

    // Set default return values
    mockUseGetMerchants.mockReturnValue({ data: [merchant1, merchant2], isLoading: false, error: null, refetch: mock(() => Promise.resolve()) })
    mockUseGetMerchantDuplicateSuggestions.mockReturnValue({ data: [], isLoading: false, error: null })
    mockUseSetMerchantOverride.mockReturnValue({ mutateAsync: mockSetMerchantOverrideMutateAsync, isPending: false, isError: false, error: null })
    mockUseClearMerchantOverride.mockReturnValue({ mutateAsync: mockClearMerchantOverrideMutateAsync, isPending: false, isError: false, error: null })
    mockUseMergeMerchants.mockReturnValue({ mutateAsync: mockMergeMerchantsMutateAsync, isPending: false, isError: false, error: null })
  })

  it('renders the DataTable with merchant rows', async () => {
    render(<MerchantsTab />, { wrapper: createWrapper() })
    await waitFor(() => {
      expect(screen.getByTestId('data-table')).toBeInTheDocument()
      expect(screen.getByTestId('merchant-row-0')).toBeInTheDocument()
      expect(screen.getByTestId('merchant-row-1')).toBeInTheDocument()
    })
    expect(screen.getByTestId('merchant-name-0')).toHaveTextContent('Whole Foods')
    expect(screen.getByTestId('merchant-name-1')).toHaveTextContent('My Coffee Shop')
  })

  it('shows empty state when no merchants', async () => {
    mockUseGetMerchants.mockReturnValue({ data: [], isLoading: false, error: null })
    render(<MerchantsTab />, { wrapper: createWrapper() })
    await waitFor(() => {
      expect(screen.getByTestId('empty-state')).toBeInTheDocument()
    })
  })

  it('shows loading state while fetching merchants', async () => {
    mockUseGetMerchants.mockReturnValue({ data: undefined, isLoading: true, error: null })
    render(<MerchantsTab />, { wrapper: createWrapper() })
    expect(screen.queryByTestId('merchant-row-0')).not.toBeInTheDocument()
  })

  it('shows error state when merchants fetch fails', async () => {
    mockUseGetMerchants.mockReturnValue({ data: undefined, isLoading: false, error: new Error('Network error') })
    render(<MerchantsTab />, { wrapper: createWrapper() })
    await waitFor(() => {
      expect(screen.getByTestId('merchants-error')).toBeInTheDocument()
    })
    expect(screen.queryByTestId('data-table')).not.toBeInTheDocument()
  })

  it('shows rename action for each merchant row', async () => {
    render(<MerchantsTab />, { wrapper: createWrapper() })
    await waitFor(() => {
      expect(screen.getByTestId('action-rename-0')).toBeInTheDocument()
    })
  })

  it('opens rename modal when Rename action is clicked', async () => {
    render(<MerchantsTab />, { wrapper: createWrapper() })
    await waitFor(() => {
      expect(screen.getByTestId('action-rename-0')).toBeInTheDocument()
    })
    fireEvent.click(screen.getByTestId('action-rename-0'))
    await waitFor(() => {
      expect(screen.getByTestId('rename-modal')).toBeInTheDocument()
    })
  })

  it('submits override when rename form is saved', async () => {
    render(<MerchantsTab />, { wrapper: createWrapper() })
    await waitFor(() => {
      expect(screen.getByTestId('action-rename-0')).toBeInTheDocument()
    })
    fireEvent.click(screen.getByTestId('action-rename-0'))
    await waitFor(() => {
      expect(screen.getByTestId('rename-modal')).toBeInTheDocument()
    })
    const input = screen.getByTestId('rename-input')
    fireEvent.change(input, { target: { value: 'Whole Foods Market' } })
    fireEvent.click(screen.getByTestId('rename-save-btn'))
    await waitFor(() => {
      expect(mockSetMerchantOverrideMutateAsync).toHaveBeenCalledWith(
        expect.objectContaining({
          merchantId: 'merch-1',
          data: expect.objectContaining({ display_name: 'Whole Foods Market' }),
        })
      )
    })
  })

  it('shows clear-override action only for merchants with an override', async () => {
    render(<MerchantsTab />, { wrapper: createWrapper() })
    await waitFor(() => {
      expect(screen.getByTestId('data-table')).toBeInTheDocument()
    })
    // merchant2 has has_override: true
    expect(screen.queryByTestId('action-clear-override-0')).not.toBeInTheDocument()
    expect(screen.getByTestId('action-clear-override-1')).toBeInTheDocument()
  })

  it('calls clearMerchantOverride when clear-override action is clicked', async () => {
    render(<MerchantsTab />, { wrapper: createWrapper() })
    await waitFor(() => {
      expect(screen.getByTestId('action-clear-override-1')).toBeInTheDocument()
    })
    fireEvent.click(screen.getByTestId('action-clear-override-1'))
    await waitFor(() => {
      expect(mockClearMerchantOverrideMutateAsync).toHaveBeenCalledWith(
        expect.objectContaining({ merchantId: 'merch-2' })
      )
    })
  })

  it('shows duplicate suggestions when they exist', async () => {
    const duplicates: MerchantSummary[][] = [
      [merchant1, { ...merchant2, merchant_id: 'merch-3', effective_display_name: 'WF Market', normalized_name: 'WF Market' }],
    ]
    mockUseGetMerchantDuplicateSuggestions.mockReturnValue({
      data: duplicates,
      isLoading: false,
      error: null,
    })
    render(<MerchantsTab />, { wrapper: createWrapper() })
    await waitFor(() => {
      expect(screen.getByTestId('duplicate-suggestions')).toBeInTheDocument()
    })
  })

  it('does not show duplicate suggestions section when list is empty', async () => {
    mockUseGetMerchantDuplicateSuggestions.mockReturnValue({ data: [], isLoading: false, error: null })
    render(<MerchantsTab />, { wrapper: createWrapper() })
    await waitFor(() => {
      expect(screen.getByTestId('data-table')).toBeInTheDocument()
    })
    expect(screen.queryByTestId('duplicate-suggestions')).not.toBeInTheDocument()
  })

  // G19: data-ph-mask on spend cells — verifies maskedColumnIds is wired correctly.
  it('spend column carries data-ph-mask for PII masking', async () => {
    render(<MerchantsTab />, { wrapper: createWrapper() })
    await waitFor(() => expect(screen.getByTestId('spend-cell-0')).toBeInTheDocument())
    expect(screen.getByTestId('spend-cell-0')).toHaveAttribute('data-ph-mask')
    expect(screen.getByTestId('spend-cell-0')).toHaveTextContent('$450.50')
  })

  it('spend column renders formatted currency', async () => {
    render(<MerchantsTab />, { wrapper: createWrapper() })
    await waitFor(() => expect(screen.getByTestId('spend-cell-1')).toBeInTheDocument())
    // merchant2.total_spend = 8750
    expect(screen.getByTestId('spend-cell-1')).toHaveTextContent('$8750.00')
  })

  // G34: rename modal must use the shared Radix Dialog — hand-rolled div has no Escape handler.
  it('closes rename modal when Escape key is pressed', async () => {
    render(<MerchantsTab />, { wrapper: createWrapper() })
    await waitFor(() => {
      expect(screen.getByTestId('action-rename-0')).toBeInTheDocument()
    })
    fireEvent.click(screen.getByTestId('action-rename-0'))
    await waitFor(() => {
      expect(screen.getByTestId('rename-modal')).toBeInTheDocument()
    })
    // Radix Dialog handles Escape natively; a hand-rolled div does not.
    fireEvent.keyDown(document.activeElement ?? document.body, { key: 'Escape', code: 'Escape', keyCode: 27 })
    await waitFor(() => {
      expect(screen.queryByTestId('rename-modal')).not.toBeInTheDocument()
    })
  })

  it('calls mergeMerchants when merge button clicked in suggestions', async () => {
    const group: MerchantSummary[] = [
      merchant1,
      { ...merchant2, merchant_id: 'merch-3', effective_display_name: 'WF Market', normalized_name: 'WF Market' },
    ]
    mockUseGetMerchantDuplicateSuggestions.mockReturnValue({
      data: [group],
      isLoading: false,
      error: null,
    })
    render(<MerchantsTab />, { wrapper: createWrapper() })
    await waitFor(() => {
      expect(screen.getByTestId('duplicate-suggestions')).toBeInTheDocument()
    })
    fireEvent.click(screen.getByTestId('merge-group-0'))
    await waitFor(() => {
      expect(mockMergeMerchantsMutateAsync).toHaveBeenCalledWith(
        expect.objectContaining({
          data: expect.objectContaining({
            merchant_ids: expect.arrayContaining(['merch-1', 'merch-3']),
            target_merchant_id: 'merch-1',
          }),
        })
      )
    })
  })
})
