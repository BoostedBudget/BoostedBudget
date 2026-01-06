---
name: typescript-patterns
description: "TypeScript/React patterns for BoostedBudget: feature structure, hooks, components, testing, and state management."
---

# TypeScript/React Patterns Guide

## Directory Structure

```
frontend/src/
├── api/
│   ├── generated/          # Orval-generated (DO NOT EDIT)
│   │   ├── endpoints/      # React Query hooks
│   │   └── models/         # TypeScript types
│   └── client.ts           # Axios instance with auth
├── features/               # Feature modules
│   └── featurename/
│       ├── types.ts
│       ├── constants.ts
│       ├── components/
│       ├── hooks/
│       └── index.ts
├── shared/                 # Cross-cutting utilities
│   ├── components/
│   ├── hooks/
│   └── utils/
├── components/
│   ├── ui/                 # shadcn/ui components
│   └── layout/             # App layout
└── auth/                   # Clerk integration
```

## Feature Module Pattern

### types.ts

```typescript
// features/transactions/types.ts

export interface Transaction {
  id: string
  user_id: string
  account_id: string
  amount: number
  date: string
  merchant_name?: string
  category?: string
  tags?: string[]
  pending: boolean
  created_at: string
  updated_at: string
}

export interface TransactionFilters {
  account_id?: string
  start_date?: string
  end_date?: string
  categories?: string[]
  tags?: string[]
  min_amount?: number
  max_amount?: number
  amount_type?: AmountType
  search?: string
}

export type AmountType = 'income' | 'expense' | 'all'

export type DatePreset = '7d' | '30d' | '90d' | '1y' | 'all' | 'custom'
```

### constants.ts

```typescript
// features/transactions/constants.ts

export const AMOUNT_TYPES = {
  all: 'All',
  income: 'Income',
  expense: 'Expense',
} as const

export const DATE_PRESETS: Record<DatePreset, string> = {
  '7d': 'Last 7 days',
  '30d': 'Last 30 days',
  '90d': 'Last 90 days',
  '1y': 'Last year',
  all: 'All time',
  custom: 'Custom range',
}

export const DEFAULT_PAGE_SIZE = 50
```

## Custom Hook Pattern

### Data Fetching Hook

```typescript
// features/transactions/hooks/useTransactions.ts

import { useState, useCallback, useMemo } from 'react'
import {
  useGetTransactions,
  getGetTransactionsQueryKey,
} from '@/api/generated/endpoints/transactions/transactions'
import type { Transaction, TransactionFilters } from '../types'

export interface UseTransactionsOptions {
  filters?: TransactionFilters
  limit?: number
  initialOffset?: number
}

export interface UseTransactionsResult {
  transactions: Transaction[]
  total: number
  loading: boolean
  error: Error | null
  hasNextPage: boolean
  hasPreviousPage: boolean
  nextPage: () => void
  previousPage: () => void
  refetch: () => void
}

export function useTransactions(options: UseTransactionsOptions = {}): UseTransactionsResult {
  const { filters = {}, limit = 50, initialOffset = 0 } = options
  const [offset, setOffset] = useState(initialOffset)

  const { data, isLoading, error, refetch } = useGetTransactions({
    ...filters,
    limit,
    offset,
  })

  const nextPage = useCallback(() => {
    setOffset((prev) => prev + limit)
  }, [limit])

  const previousPage = useCallback(() => {
    setOffset((prev) => Math.max(0, prev - limit))
  }, [limit])

  const total = data?.total ?? 0
  const hasNextPage = offset + limit < total
  const hasPreviousPage = offset > 0

  return {
    transactions: data?.transactions ?? [],
    total,
    loading: isLoading,
    error: error as Error | null,
    hasNextPage,
    hasPreviousPage,
    nextPage,
    previousPage,
    refetch,
  }
}
```

### Mutation Hook

```typescript
// features/transactions/hooks/useUpdateTransaction.ts

import { useQueryClient } from '@tanstack/react-query'
import {
  useUpdateTransactionTags,
  getGetTransactionsQueryKey,
} from '@/api/generated/endpoints/transactions/transactions'

export function useUpdateTransactionTags() {
  const queryClient = useQueryClient()

  const mutation = useUpdateTransactionTags({
    mutation: {
      onSuccess: () => {
        // Invalidate transactions query to refetch
        queryClient.invalidateQueries({
          queryKey: getGetTransactionsQueryKey(),
        })
      },
    },
  })

  return mutation
}
```

## Component Patterns

### Page Component

```typescript
// features/transactions/components/TransactionsPage.tsx

import { useState } from 'react'
import { useTransactions } from '../hooks/useTransactions'
import { TransactionTable } from './TransactionTable'
import { TransactionFilters } from './TransactionFilters'
import type { TransactionFilters as Filters } from '../types'

export function TransactionsPage() {
  const [filters, setFilters] = useState<Filters>({})

  const {
    transactions,
    total,
    loading,
    error,
    hasNextPage,
    hasPreviousPage,
    nextPage,
    previousPage,
  } = useTransactions({ filters })

  if (error) {
    return (
      <div className="p-4 text-destructive">
        Error loading transactions: {error.message}
      </div>
    )
  }

  return (
    <div className="container mx-auto p-4 space-y-4">
      <h1 className="text-2xl font-bold">Transactions</h1>

      <TransactionFilters
        filters={filters}
        onChange={setFilters}
      />

      <TransactionTable
        transactions={transactions}
        loading={loading}
      />

      <div className="flex justify-between items-center">
        <span className="text-sm text-muted-foreground">
          Showing {transactions.length} of {total}
        </span>
        <div className="space-x-2">
          <Button
            onClick={previousPage}
            disabled={!hasPreviousPage}
          >
            Previous
          </Button>
          <Button
            onClick={nextPage}
            disabled={!hasNextPage}
          >
            Next
          </Button>
        </div>
      </div>
    </div>
  )
}
```

### Presentational Component

```typescript
// features/transactions/components/TransactionCard.tsx

import { formatCurrency, formatDate } from '@/shared/utils/format'
import type { Transaction } from '../types'

interface TransactionCardProps {
  transaction: Transaction
  onTagsChange?: (tags: string[]) => void
}

export function TransactionCard({ transaction, onTagsChange }: TransactionCardProps) {
  const isIncome = transaction.amount > 0

  return (
    <div className="rounded-lg border p-4 space-y-2">
      <div className="flex justify-between items-start">
        <div>
          <h3 className="font-medium">{transaction.merchant_name || 'Unknown'}</h3>
          <p className="text-sm text-muted-foreground">
            {formatDate(transaction.date)}
          </p>
        </div>
        <span className={isIncome ? 'text-green-600' : 'text-red-600'}>
          {formatCurrency(transaction.amount)}
        </span>
      </div>

      {transaction.category && (
        <span className="inline-block px-2 py-1 text-xs bg-secondary rounded">
          {transaction.category}
        </span>
      )}
    </div>
  )
}
```

## CVA Component Pattern

```typescript
// components/ui/button.tsx

import { forwardRef } from 'react'
import { Slot } from '@radix-ui/react-slot'
import { cva, type VariantProps } from 'class-variance-authority'
import { cn } from '@/lib/utils'

const buttonVariants = cva(
  'inline-flex items-center justify-center rounded-md text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 disabled:pointer-events-none disabled:opacity-50',
  {
    variants: {
      variant: {
        default: 'bg-primary text-primary-foreground hover:bg-primary/90',
        destructive: 'bg-destructive text-white hover:bg-destructive/90',
        outline: 'border bg-background shadow-xs hover:bg-accent',
        secondary: 'bg-secondary text-secondary-foreground hover:bg-secondary/80',
        ghost: 'hover:bg-accent hover:text-accent-foreground',
        link: 'text-primary underline-offset-4 hover:underline',
      },
      size: {
        default: 'h-9 px-4 py-2',
        sm: 'h-8 px-3 text-xs',
        lg: 'h-10 px-6',
        icon: 'h-9 w-9',
      },
    },
    defaultVariants: {
      variant: 'default',
      size: 'default',
    },
  }
)

interface ButtonProps
  extends React.ButtonHTMLAttributes<HTMLButtonElement>,
    VariantProps<typeof buttonVariants> {
  asChild?: boolean
}

const Button = forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant, size, asChild = false, ...props }, ref) => {
    const Comp = asChild ? Slot : 'button'
    return (
      <Comp
        className={cn(buttonVariants({ variant, size }), className)}
        ref={ref}
        {...props}
      />
    )
  }
)
Button.displayName = 'Button'

export { Button, buttonVariants }
```

## Testing Pattern

### Hook Test with vi.hoisted

```typescript
// features/transactions/hooks/useTransactions.test.tsx

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { useTransactions } from './useTransactions'

// IMPORTANT: Use vi.hoisted for mocks that need early evaluation
const { mockUseGetTransactions } = vi.hoisted(() => ({
  mockUseGetTransactions: vi.fn(),
}))

vi.mock('@/api/generated/endpoints/transactions/transactions', () => ({
  useGetTransactions: mockUseGetTransactions,
  getGetTransactionsQueryKey: vi.fn(() => ['transactions']),
}))

function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
    },
  })
  return ({ children }: { children: React.ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  )
}

describe('useTransactions', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('returns loading state initially', () => {
    mockUseGetTransactions.mockReturnValue({
      data: undefined,
      isLoading: true,
      error: null,
      refetch: vi.fn(),
    })

    const { result } = renderHook(() => useTransactions(), {
      wrapper: createWrapper(),
    })

    expect(result.current.loading).toBe(true)
    expect(result.current.transactions).toEqual([])
  })

  it('returns transactions when loaded', () => {
    const mockTransactions = [
      { id: '1', amount: -50, merchant_name: 'Store' },
    ]

    mockUseGetTransactions.mockReturnValue({
      data: { transactions: mockTransactions, total: 1 },
      isLoading: false,
      error: null,
      refetch: vi.fn(),
    })

    const { result } = renderHook(() => useTransactions(), {
      wrapper: createWrapper(),
    })

    expect(result.current.loading).toBe(false)
    expect(result.current.transactions).toEqual(mockTransactions)
    expect(result.current.total).toBe(1)
  })
})
```

### Component Test

```typescript
// features/transactions/components/TransactionCard.test.tsx

import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { TransactionCard } from './TransactionCard'

describe('TransactionCard', () => {
  const mockTransaction = {
    id: '1',
    user_id: 'user-1',
    account_id: 'account-1',
    amount: -50.00,
    date: '2024-01-15',
    merchant_name: 'Coffee Shop',
    category: 'Food & Drink',
    pending: false,
    created_at: '2024-01-15T10:00:00Z',
    updated_at: '2024-01-15T10:00:00Z',
  }

  it('renders transaction details', () => {
    render(<TransactionCard transaction={mockTransaction} />)

    expect(screen.getByText('Coffee Shop')).toBeInTheDocument()
    expect(screen.getByText('Food & Drink')).toBeInTheDocument()
  })

  it('shows negative amount in red', () => {
    render(<TransactionCard transaction={mockTransaction} />)

    const amount = screen.getByText(/\$50\.00/)
    expect(amount).toHaveClass('text-red-600')
  })
})
```

## Barrel Exports

### components/index.ts

```typescript
export { TransactionsPage } from './TransactionsPage'
export { TransactionCard } from './TransactionCard'
export { TransactionTable } from './TransactionTable'
export { TransactionFilters } from './TransactionFilters'
```

### hooks/index.ts

```typescript
export { useTransactions } from './useTransactions'
export type { UseTransactionsOptions, UseTransactionsResult } from './useTransactions'
```

### Feature index.ts

```typescript
export * from './components'
export * from './hooks'
export * from './types'
export * from './constants'
```

## Quick Reference

| Pattern | Location | Purpose |
|---------|----------|---------|
| Types | `features/*/types.ts` | Feature-specific types |
| Constants | `features/*/constants.ts` | Feature constants |
| Data Hook | `features/*/hooks/use*.ts` | Wrap generated API hooks |
| Page | `features/*/components/*Page.tsx` | Top-level page component |
| Card | `features/*/components/*Card.tsx` | Presentational component |
| UI | `components/ui/*.tsx` | CVA-styled shadcn components |
| Test | `*.test.tsx` | Tests with vi.hoisted mocks |
