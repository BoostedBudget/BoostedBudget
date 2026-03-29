# New React Component

Scaffold a new React component following project patterns.

## Usage

```
/new-component [feature-name] [component-name]
```

Example: `/new-component transactions TransactionFilters`

## Feature Page Structure

For a new feature page, create this structure:

```
apps/web/src/features/featurename/
├── types.ts
├── constants.ts (if needed)
├── components/
│   ├── FeatureNamePage.tsx
│   ├── FeatureCard.tsx
│   ├── FeatureCard.css (if needed)
│   └── index.ts
├── hooks/
│   ├── useFeatureData.ts
│   ├── useFeatureData.test.tsx
│   └── index.ts
└── index.ts
```

## File Templates

### types.ts

```typescript
export interface Feature {
  id: string
  user_id: string
  name: string
  created_at: string
  updated_at: string
}

export interface FeatureFilters {
  search?: string
  status?: FeatureStatus
}

export type FeatureStatus = 'active' | 'inactive' | 'pending'
```

### hooks/useFeatureData.ts

```typescript
import { useState, useCallback, useMemo } from 'react'
import { useGetFeatures } from '@boostedbudget/api-client/endpoints/features/features'
import type { Feature, FeatureFilters } from '../types'

export interface UseFeatureDataOptions {
  filters?: FeatureFilters
  limit?: number
}

export interface UseFeatureDataResult {
  data: Feature[] | null
  loading: boolean
  error: Error | null
  total: number
  refetch: () => void
}

export function useFeatureData(options: UseFeatureDataOptions = {}): UseFeatureDataResult {
  const { filters, limit = 50 } = options

  const { data, isLoading, error, refetch } = useGetFeatures({
    ...filters,
    limit,
  })

  return {
    data: data?.features || null,
    loading: isLoading,
    error: error as Error | null,
    total: data?.total || 0,
    refetch,
  }
}
```

### hooks/useFeatureData.test.tsx

```typescript
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { useFeatureData } from './useFeatureData'

// Mock generated hooks with vi.hoisted
const { mockUseGetFeatures } = vi.hoisted(() => ({
  mockUseGetFeatures: vi.fn(),
}))

vi.mock('@boostedbudget/api-client/endpoints/features/features', () => ({
  useGetFeatures: mockUseGetFeatures,
}))

function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return ({ children }: { children: React.ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  )
}

describe('useFeatureData', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('returns loading state initially', () => {
    mockUseGetFeatures.mockReturnValue({
      data: undefined,
      isLoading: true,
      error: null,
      refetch: vi.fn(),
    })

    const { result } = renderHook(() => useFeatureData(), {
      wrapper: createWrapper(),
    })

    expect(result.current.loading).toBe(true)
    expect(result.current.data).toBeNull()
  })

  it('returns data when loaded', async () => {
    const mockData = {
      features: [{ id: '1', name: 'Test' }],
      total: 1,
    }

    mockUseGetFeatures.mockReturnValue({
      data: mockData,
      isLoading: false,
      error: null,
      refetch: vi.fn(),
    })

    const { result } = renderHook(() => useFeatureData(), {
      wrapper: createWrapper(),
    })

    expect(result.current.loading).toBe(false)
    expect(result.current.data).toEqual(mockData.features)
  })
})
```

### components/FeatureNamePage.tsx

```typescript
import { useState } from 'react'
import { useFeatureData } from '../hooks/useFeatureData'
import { FeatureCard } from './FeatureCard'
import type { FeatureFilters } from '../types'

export function FeatureNamePage() {
  const [filters, setFilters] = useState<FeatureFilters>({})

  const { data, loading, error, refetch } = useFeatureData({ filters })

  if (loading) {
    return <div>Loading...</div>
  }

  if (error) {
    return <div>Error: {error.message}</div>
  }

  return (
    <div className="container mx-auto p-4">
      <h1 className="text-2xl font-bold mb-4">Features</h1>

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
        {data?.map((feature) => (
          <FeatureCard key={feature.id} feature={feature} />
        ))}
      </div>
    </div>
  )
}
```

### components/FeatureCard.tsx

```typescript
import type { Feature } from '../types'

interface FeatureCardProps {
  feature: Feature
}

export function FeatureCard({ feature }: FeatureCardProps) {
  return (
    <div className="rounded-lg border p-4 shadow-sm">
      <h3 className="font-semibold">{feature.name}</h3>
      <p className="text-sm text-muted-foreground">
        Created: {new Date(feature.created_at).toLocaleDateString()}
      </p>
    </div>
  )
}
```

### components/index.ts

```typescript
export { FeatureNamePage } from './FeatureNamePage'
export { FeatureCard } from './FeatureCard'
```

### hooks/index.ts

```typescript
export { useFeatureData } from './useFeatureData'
export type { UseFeatureDataOptions, UseFeatureDataResult } from './useFeatureData'
```

### index.ts (feature root)

```typescript
export * from './components'
export * from './hooks'
export * from './types'
```

## Add Route

In `apps/web/src/App.tsx`:

```typescript
import { FeatureNamePage } from '@/features/featurename'

// In routes
<Route path="/features" element={
  <ProtectedRoute><FeatureNamePage /></ProtectedRoute>
} />
```

## Checklist

- [ ] types.ts with feature interfaces
- [ ] Custom hook wrapping generated API
- [ ] Hook tests with vi.hoisted mocks
- [ ] Page component with loading/error states
- [ ] Card/list components
- [ ] Barrel exports (index.ts)
- [ ] Route added to App.tsx

## Related

- `.claude/skills/typescript-patterns/` - TypeScript patterns
- `.claude/agents/frontend-reviewer.md` - Frontend review agent
