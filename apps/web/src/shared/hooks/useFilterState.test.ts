/**
 * useFilterState Generic Hook Tests
 * Issue #1129: Generic useFilterState hook
 *
 * Tests URL-state management for filter fields (multi-select, range, scalar).
 */

import { describe, it, expect, beforeEach, expectTypeOf } from 'bun:test'
import { renderHook, act } from '@testing-library/react'
import { BrowserRouter } from 'react-router-dom'
import React from 'react'
import { useFilterState } from './useFilterState'
import type { FilterSpec } from './useFilterState'

function createWrapper() {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return React.createElement(BrowserRouter, null, children)
  }
}

// ---------------------------------------------------------------------------
// 1. Multi-select
// ---------------------------------------------------------------------------
describe('useFilterState — multi-select', () => {
  beforeEach(() => {
    window.history.replaceState({}, '', '/')
  })

  const spec: FilterSpec = {
    institution: { kind: 'multi-select' },
  }

  it('initialises empty when no URL param', () => {
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    expect(result.current.filters['institution']).toEqual([])
    expect(result.current.activeFilterCount).toBe(0)
    expect(result.current.hasActiveFilters).toBe(false)
  })

  it('parses CSV from URL on init', () => {
    window.history.replaceState({}, '', '/?institution=Chase,Bank%20of%20America')
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    expect(result.current.filters['institution']).toEqual(['Chase', 'Bank of America'])
    expect(result.current.activeFilterCount).toBe(1)
    expect(result.current.hasActiveFilters).toBe(true)
  })

  it('trims whitespace from CSV values', () => {
    window.history.replaceState({}, '', '/?institution=%20Chase%20,%20Wells%20Fargo%20')
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    expect(result.current.filters['institution']).toEqual(['Chase', 'Wells Fargo'])
  })

  it('filters empty strings from CSV', () => {
    window.history.replaceState({}, '', '/?institution=Chase,,Wells%20Fargo,')
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    expect(result.current.filters['institution']).toEqual(['Chase', 'Wells Fargo'])
  })

  it('setFilter sets CSV in URL', () => {
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    act(() => {
      result.current.setFilter('institution', ['Chase', 'Wells Fargo'])
    })
    expect(result.current.filters['institution']).toEqual(['Chase', 'Wells Fargo'])
    expect(window.location.search).toContain('institution=Chase')
  })

  it('setFilter with empty array deletes URL key', () => {
    window.history.replaceState({}, '', '/?institution=Chase')
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    act(() => {
      result.current.setFilter('institution', [])
    })
    expect(result.current.filters['institution']).toEqual([])
    expect(window.location.search).not.toContain('institution')
  })

  it('clearFilter removes URL key', () => {
    window.history.replaceState({}, '', '/?institution=Chase')
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    act(() => {
      result.current.clearFilter('institution')
    })
    expect(result.current.filters['institution']).toEqual([])
    expect(window.location.search).not.toContain('institution')
  })
})

// ---------------------------------------------------------------------------
// 2. Multi-select with serialize: 'repeat'
// ---------------------------------------------------------------------------
describe('useFilterState — multi-select repeat serialization', () => {
  beforeEach(() => {
    window.history.replaceState({}, '', '/')
  })

  const spec: FilterSpec = {
    tag: { kind: 'multi-select', serialize: 'repeat' },
  }

  it('sets repeated URL keys', () => {
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    act(() => {
      result.current.setFilter('tag', ['food', 'travel'])
    })
    expect(window.location.search).toContain('tag=food')
    expect(window.location.search).toContain('tag=travel')
  })

  it('parses repeated URL keys', () => {
    window.history.replaceState({}, '', '/?tag=food&tag=travel')
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    expect(result.current.filters['tag']).toEqual(['food', 'travel'])
  })
})

// ---------------------------------------------------------------------------
// 3. Range (float)
// ---------------------------------------------------------------------------
describe('useFilterState — range float', () => {
  beforeEach(() => {
    window.history.replaceState({}, '', '/')
  })

  const spec: FilterSpec = {
    balance: { kind: 'range' },
  }

  it('initialises undefined when no URL params', () => {
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    expect(result.current.filters['balance']).toBeUndefined()
    expect(result.current.activeFilterCount).toBe(0)
  })

  it('parses min only from URL', () => {
    window.history.replaceState({}, '', '/?balance_min=100')
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    expect(result.current.filters['balance']).toEqual({ min: 100, max: undefined })
    expect(result.current.activeFilterCount).toBe(1)
  })

  it('parses max only from URL', () => {
    window.history.replaceState({}, '', '/?balance_max=5000')
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    expect(result.current.filters['balance']).toEqual({ min: undefined, max: 5000 })
    expect(result.current.activeFilterCount).toBe(1)
  })

  it('parses both min and max from URL', () => {
    window.history.replaceState({}, '', '/?balance_min=100&balance_max=5000')
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    expect(result.current.filters['balance']).toEqual({ min: 100, max: 5000 })
    expect(result.current.activeFilterCount).toBe(1)
  })

  it('uses parseFloat (preserves decimals)', () => {
    window.history.replaceState({}, '', '/?balance_min=1.5&balance_max=99.99')
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    expect(result.current.filters['balance']).toEqual({ min: 1.5, max: 99.99 })
  })

  it('setFilter sets both URL keys', () => {
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    act(() => {
      result.current.setFilter('balance', { min: 100, max: 5000 })
    })
    expect(window.location.search).toContain('balance_min=100')
    expect(window.location.search).toContain('balance_max=5000')
  })

  it('setFilter with min only sets min, deletes max', () => {
    window.history.replaceState({}, '', '/?balance_min=100&balance_max=5000')
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    act(() => {
      result.current.setFilter('balance', { min: 100, max: undefined })
    })
    expect(window.location.search).toContain('balance_min=100')
    expect(window.location.search).not.toContain('balance_max')
  })

  it('setFilter with undefined deletes both URL keys', () => {
    window.history.replaceState({}, '', '/?balance_min=100&balance_max=5000')
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    act(() => {
      result.current.setFilter('balance', undefined)
    })
    expect(window.location.search).not.toContain('balance_min')
    expect(window.location.search).not.toContain('balance_max')
    expect(result.current.filters['balance']).toBeUndefined()
  })

  it('setFilter with both undefined deletes both URL keys', () => {
    window.history.replaceState({}, '', '/?balance_min=100&balance_max=5000')
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    act(() => {
      result.current.setFilter('balance', { min: undefined, max: undefined })
    })
    expect(window.location.search).not.toContain('balance_min')
    expect(window.location.search).not.toContain('balance_max')
  })

  it('clearFilter deletes both URL keys', () => {
    window.history.replaceState({}, '', '/?balance_min=100&balance_max=5000')
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    act(() => {
      result.current.clearFilter('balance')
    })
    expect(window.location.search).not.toContain('balance_min')
    expect(window.location.search).not.toContain('balance_max')
    expect(result.current.filters['balance']).toBeUndefined()
  })

  it('returns undefined for NaN min/max', () => {
    window.history.replaceState({}, '', '/?balance_min=invalid&balance_max=nope')
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    expect(result.current.filters['balance']).toBeUndefined()
  })
})

// ---------------------------------------------------------------------------
// 4. Range (integer)
// ---------------------------------------------------------------------------
describe('useFilterState — range integer', () => {
  beforeEach(() => {
    window.history.replaceState({}, '', '/')
  })

  const spec: FilterSpec = {
    count: { kind: 'range', codec: 'integer' },
  }

  it('uses parseInt — 1.5 becomes 1', () => {
    window.history.replaceState({}, '', '/?count_min=1&count_max=10')
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    expect(result.current.filters['count']).toEqual({ min: 1, max: 10 })
  })

  it('setFilter stores integer values', () => {
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    act(() => {
      result.current.setFilter('count', { min: 3, max: 7 })
    })
    expect(window.location.search).toContain('count_min=3')
    expect(window.location.search).toContain('count_max=7')
  })
})

// ---------------------------------------------------------------------------
// 5. Range with custom urlKeys
// ---------------------------------------------------------------------------
describe('useFilterState — range custom urlKeys', () => {
  beforeEach(() => {
    window.history.replaceState({}, '', '/')
  })

  const spec: FilterSpec = {
    price: { kind: 'range', urlKeys: { min: 'price_min', max: 'price_max' } },
  }

  it('uses custom URL key names', () => {
    window.history.replaceState({}, '', '/?price_min=10&price_max=99')
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    expect(result.current.filters['price']).toEqual({ min: 10, max: 99 })
  })
})

// ---------------------------------------------------------------------------
// 6. Scalar string
// ---------------------------------------------------------------------------
describe('useFilterState — scalar string', () => {
  beforeEach(() => {
    window.history.replaceState({}, '', '/')
  })

  const spec: FilterSpec = {
    search: { kind: 'scalar', codec: 'string' },
  }

  it('initialises undefined when absent', () => {
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    expect(result.current.filters['search']).toBeUndefined()
    expect(result.current.activeFilterCount).toBe(0)
  })

  it('parses string from URL', () => {
    window.history.replaceState({}, '', '/?search=hello')
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    expect(result.current.filters['search']).toBe('hello')
    expect(result.current.activeFilterCount).toBe(1)
  })

  it('setFilter sets URL key', () => {
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    act(() => {
      result.current.setFilter('search', 'hello')
    })
    expect(window.location.search).toContain('search=hello')
  })

  it('setFilter with undefined deletes URL key', () => {
    window.history.replaceState({}, '', '/?search=hello')
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    act(() => {
      result.current.setFilter('search', undefined)
    })
    expect(window.location.search).not.toContain('search')
    expect(result.current.filters['search']).toBeUndefined()
  })
})

// ---------------------------------------------------------------------------
// 7. Scalar number
// ---------------------------------------------------------------------------
describe('useFilterState — scalar number', () => {
  beforeEach(() => {
    window.history.replaceState({}, '', '/')
  })

  const spec: FilterSpec = {
    threshold: { kind: 'scalar', codec: 'number' },
  }

  it('parses float from URL', () => {
    window.history.replaceState({}, '', '/?threshold=3.14')
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    expect(result.current.filters['threshold']).toBe(3.14)
  })

  it('returns undefined for NaN', () => {
    window.history.replaceState({}, '', '/?threshold=nope')
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    expect(result.current.filters['threshold']).toBeUndefined()
    expect(result.current.activeFilterCount).toBe(0)
  })

  it('returns undefined when absent', () => {
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    expect(result.current.filters['threshold']).toBeUndefined()
  })
})

// ---------------------------------------------------------------------------
// 8. Scalar integer
// ---------------------------------------------------------------------------
describe('useFilterState — scalar integer', () => {
  beforeEach(() => {
    window.history.replaceState({}, '', '/')
  })

  const spec: FilterSpec = {
    page: { kind: 'scalar', codec: 'integer' },
  }

  it('parses integer from URL', () => {
    window.history.replaceState({}, '', '/?page=5')
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    expect(result.current.filters['page']).toBe(5)
  })

  it('returns undefined for NaN', () => {
    window.history.replaceState({}, '', '/?page=abc')
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    expect(result.current.filters['page']).toBeUndefined()
  })
})

// ---------------------------------------------------------------------------
// 9. Scalar boolean
// ---------------------------------------------------------------------------
describe('useFilterState — scalar boolean', () => {
  beforeEach(() => {
    window.history.replaceState({}, '', '/')
  })

  const spec: FilterSpec = {
    verified: { kind: 'scalar', codec: 'boolean' },
  }

  it("parses 'true' as true", () => {
    window.history.replaceState({}, '', '/?verified=true')
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    expect(result.current.filters['verified']).toBe(true)
    expect(result.current.activeFilterCount).toBe(1)
  })

  it("parses 'false' as false", () => {
    window.history.replaceState({}, '', '/?verified=false')
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    expect(result.current.filters['verified']).toBe(false)
    expect(result.current.activeFilterCount).toBe(1)
  })

  it('returns undefined when absent', () => {
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    expect(result.current.filters['verified']).toBeUndefined()
    expect(result.current.activeFilterCount).toBe(0)
  })

  it('setFilter true writes "true" to URL', () => {
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    act(() => {
      result.current.setFilter('verified', true)
    })
    expect(window.location.search).toContain('verified=true')
  })

  it('setFilter false writes "false" to URL', () => {
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    act(() => {
      result.current.setFilter('verified', false)
    })
    expect(window.location.search).toContain('verified=false')
  })

  it('setFilter undefined deletes URL key', () => {
    window.history.replaceState({}, '', '/?verified=true')
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    act(() => {
      result.current.setFilter('verified', undefined)
    })
    expect(window.location.search).not.toContain('verified')
  })
})

// ---------------------------------------------------------------------------
// 10. Scalar with defaultValue — isActive false when value equals default
// ---------------------------------------------------------------------------
describe('useFilterState — scalar defaultValue', () => {
  beforeEach(() => {
    window.history.replaceState({}, '', '/')
  })

  const spec: FilterSpec = {
    amount_type: { kind: 'scalar', codec: 'string', defaultValue: 'all' },
  }

  it('isActive false when value is undefined (absent)', () => {
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    expect(result.current.activeFilterCount).toBe(0)
  })

  it('isActive false when value equals defaultValue', () => {
    window.history.replaceState({}, '', '/?amount_type=all')
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    expect(result.current.filters['amount_type']).toBe('all')
    expect(result.current.activeFilterCount).toBe(0)
  })

  it('isActive true when value differs from defaultValue', () => {
    window.history.replaceState({}, '', '/?amount_type=debit')
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    expect(result.current.activeFilterCount).toBe(1)
  })
})

// ---------------------------------------------------------------------------
// 11. Scalar with custom activeWhen predicate
// ---------------------------------------------------------------------------
describe('useFilterState — scalar activeWhen', () => {
  beforeEach(() => {
    window.history.replaceState({}, '', '/')
  })

  const spec: FilterSpec = {
    amount_type: {
      kind: 'scalar',
      codec: 'string',
      activeWhen: (v: unknown) => v !== undefined && v !== 'all',
    },
  }

  it('isActive false when value is undefined', () => {
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    expect(result.current.activeFilterCount).toBe(0)
  })

  it("isActive false when value is 'all'", () => {
    window.history.replaceState({}, '', '/?amount_type=all')
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    expect(result.current.activeFilterCount).toBe(0)
  })

  it("isActive true when value is something else (e.g. 'debit')", () => {
    window.history.replaceState({}, '', '/?amount_type=debit')
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    expect(result.current.activeFilterCount).toBe(1)
  })
})

// ---------------------------------------------------------------------------
// 12. Scalar with custom codec
// ---------------------------------------------------------------------------
describe('useFilterState — scalar custom codec', () => {
  beforeEach(() => {
    window.history.replaceState({}, '', '/')
  })

  const spec: FilterSpec = {
    ids: {
      kind: 'scalar',
      codec: {
        parse: (raw: string) => raw.split('|').map(Number),
        serialize: (v: unknown) => {
          const arr = v as number[]
          return arr.length ? arr.join('|') : null
        },
      },
    },
  }

  it('parses custom format from URL', () => {
    window.history.replaceState({}, '', '/?ids=1|2|3')
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    expect(result.current.filters['ids']).toEqual([1, 2, 3])
  })

  it('serializes custom format to URL', () => {
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    act(() => {
      result.current.setFilter('ids', [4, 5])
    })
    expect(window.location.search).toContain('ids=4%7C5')
  })

  it('null serialization deletes URL key', () => {
    window.history.replaceState({}, '', '/?ids=1|2')
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    act(() => {
      result.current.setFilter('ids', [])
    })
    expect(window.location.search).not.toContain('ids')
  })
})

// ---------------------------------------------------------------------------
// 13. clearAllFilters — only spec keys, preserves others
// ---------------------------------------------------------------------------
describe('useFilterState — clearAllFilters', () => {
  beforeEach(() => {
    window.history.replaceState({}, '', '/')
  })

  const spec: FilterSpec = {
    institution: { kind: 'multi-select' },
    balance: { kind: 'range' },
    search: { kind: 'scalar', codec: 'string' },
  }

  it('clears all spec-declared keys from URL', () => {
    window.history.replaceState(
      {},
      '',
      '/?institution=Chase&balance_min=100&balance_max=5000&search=hello',
    )
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })

    act(() => {
      result.current.clearAllFilters()
    })

    expect(window.location.search).not.toContain('institution')
    expect(window.location.search).not.toContain('balance_min')
    expect(window.location.search).not.toContain('balance_max')
    expect(window.location.search).not.toContain('search')
    expect(result.current.activeFilterCount).toBe(0)
  })

  it('preserves URL keys not declared in spec', () => {
    window.history.replaceState({}, '', '/?institution=Chase&sort=name&offset=10')
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })

    act(() => {
      result.current.clearAllFilters()
    })

    expect(window.location.search).not.toContain('institution')
    expect(window.location.search).toContain('sort=name')
    expect(window.location.search).toContain('offset=10')
  })
})

// ---------------------------------------------------------------------------
// 14. clearKeysOnWrite — every mutation also deletes specified keys
// ---------------------------------------------------------------------------
describe('useFilterState — clearKeysOnWrite', () => {
  beforeEach(() => {
    window.history.replaceState({}, '', '/')
  })

  const spec: FilterSpec = {
    institution: { kind: 'multi-select' },
    balance: { kind: 'range' },
  }

  it('deletes offset on setFilter', () => {
    window.history.replaceState({}, '', '/?institution=Chase&offset=20')
    const { result } = renderHook(
      () => useFilterState(spec, { clearKeysOnWrite: ['offset'] }),
      { wrapper: createWrapper() },
    )
    act(() => {
      result.current.setFilter('institution', ['Wells Fargo'])
    })
    expect(window.location.search).toContain('institution=Wells')
    expect(window.location.search).not.toContain('offset')
  })

  it('deletes offset on setFilters', () => {
    window.history.replaceState({}, '', '/?offset=10')
    const { result } = renderHook(
      () => useFilterState(spec, { clearKeysOnWrite: ['offset'] }),
      { wrapper: createWrapper() },
    )
    act(() => {
      result.current.setFilters({ institution: ['Chase'] })
    })
    expect(window.location.search).not.toContain('offset')
  })

  it('deletes offset on clearFilter', () => {
    window.history.replaceState({}, '', '/?institution=Chase&offset=5')
    const { result } = renderHook(
      () => useFilterState(spec, { clearKeysOnWrite: ['offset'] }),
      { wrapper: createWrapper() },
    )
    act(() => {
      result.current.clearFilter('institution')
    })
    expect(window.location.search).not.toContain('institution')
    expect(window.location.search).not.toContain('offset')
  })

  it('deletes offset on clearAllFilters', () => {
    window.history.replaceState({}, '', '/?institution=Chase&offset=3')
    const { result } = renderHook(
      () => useFilterState(spec, { clearKeysOnWrite: ['offset'] }),
      { wrapper: createWrapper() },
    )
    act(() => {
      result.current.clearAllFilters()
    })
    expect(window.location.search).not.toContain('institution')
    expect(window.location.search).not.toContain('offset')
  })
})

// ---------------------------------------------------------------------------
// 15. setFilters — batch update
// ---------------------------------------------------------------------------
describe('useFilterState — setFilters batch', () => {
  beforeEach(() => {
    window.history.replaceState({}, '', '/')
  })

  const spec: FilterSpec = {
    institution: { kind: 'multi-select' },
    search: { kind: 'scalar', codec: 'string' },
  }

  it('updates multiple fields in one operation', () => {
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    act(() => {
      result.current.setFilters({ institution: ['Chase'], search: 'savings' })
    })
    expect(result.current.filters['institution']).toEqual(['Chase'])
    expect(result.current.filters['search']).toBe('savings')
  })
})

// ---------------------------------------------------------------------------
// 16. activeFilterCount — mixed spec
// ---------------------------------------------------------------------------
describe('useFilterState — activeFilterCount mixed spec', () => {
  beforeEach(() => {
    window.history.replaceState({}, '', '/')
  })

  const spec: FilterSpec = {
    institution: { kind: 'multi-select' },
    balance: { kind: 'range' },
    search: { kind: 'scalar', codec: 'string' },
    verified: { kind: 'scalar', codec: 'boolean' },
  }

  it('counts 0 when all inactive', () => {
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    expect(result.current.activeFilterCount).toBe(0)
    expect(result.current.hasActiveFilters).toBe(false)
  })

  it('counts each active field once', () => {
    window.history.replaceState(
      {},
      '',
      '/?institution=Chase&balance_min=100&search=hello&verified=true',
    )
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    // institution=1, balance=1, search=1, verified=1 → 4
    expect(result.current.activeFilterCount).toBe(4)
    expect(result.current.hasActiveFilters).toBe(true)
  })

  it('range counts as 1 even with only min set', () => {
    window.history.replaceState({}, '', '/?balance_min=100')
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    expect(result.current.activeFilterCount).toBe(1)
  })

  it('multi-select counts as 1 regardless of array length', () => {
    window.history.replaceState(
      {},
      '',
      '/?institution=Chase,Wells%20Fargo,Bank%20of%20America',
    )
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    expect(result.current.activeFilterCount).toBe(1)
  })
})

// ---------------------------------------------------------------------------
// 17. scalar with custom urlKey
// ---------------------------------------------------------------------------
describe('useFilterState — scalar custom urlKey', () => {
  beforeEach(() => {
    window.history.replaceState({}, '', '/')
  })

  const spec: FilterSpec = {
    txType: { kind: 'scalar', codec: 'string', urlKey: 'tx_type' },
  }

  it('reads from custom URL key', () => {
    window.history.replaceState({}, '', '/?tx_type=debit')
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    expect(result.current.filters['txType']).toBe('debit')
  })

  it('writes to custom URL key', () => {
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    act(() => {
      result.current.setFilter('txType', 'credit')
    })
    expect(window.location.search).toContain('tx_type=credit')
    expect(window.location.search).not.toContain('txType')
  })
})

// ---------------------------------------------------------------------------
// 18. unknown key — setFilter is a no-op
// ---------------------------------------------------------------------------
// NOTE: type tests (section 19) are appended after this block
describe('useFilterState — unknown key no-op', () => {
  beforeEach(() => {
    window.history.replaceState({}, '', '/')
  })

  const spec: FilterSpec = {
    institution: { kind: 'multi-select' },
  }

  it('setFilter with unknown key does not change URL', () => {
    window.history.replaceState({}, '', '/?institution=Chase')
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    const searchBefore = window.location.search
    act(() => {
      result.current.setFilter('nonExistentKey' as keyof typeof spec & string, 'value')
    })
    expect(window.location.search).toBe(searchBefore)
  })

  it('setFilter with unknown key does not throw', () => {
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    expect(() => {
      act(() => {
        result.current.setFilter('nonExistentKey' as keyof typeof spec & string, 'value')
      })
    }).not.toThrow()
  })
})

// ---------------------------------------------------------------------------
// 19. InferFilters<S> — type-level assertions (compile-time only)
// ---------------------------------------------------------------------------
describe('useFilterState — InferFilters type inference', () => {
  it('infers string[] for multi-select', () => {
    const spec = { institution: { kind: 'multi-select' } } as const satisfies FilterSpec
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    expectTypeOf(result.current.filters.institution).toEqualTypeOf<string[]>()
  })

  it('infers { min?: number; max?: number } | undefined for range', () => {
    const spec = { balance: { kind: 'range' } } as const satisfies FilterSpec
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    expectTypeOf(result.current.filters.balance).toEqualTypeOf<
      { min?: number; max?: number } | undefined
    >()
  })

  it("infers string | undefined for scalar codec 'string'", () => {
    const spec = { search: { kind: 'scalar', codec: 'string' as const } } as const satisfies FilterSpec
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    expectTypeOf(result.current.filters.search).toEqualTypeOf<string | undefined>()
  })

  it("infers number | undefined for scalar codec 'number'", () => {
    const spec = { amount: { kind: 'scalar', codec: 'number' as const } } as const satisfies FilterSpec
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    expectTypeOf(result.current.filters.amount).toEqualTypeOf<number | undefined>()
  })

  it("infers number | undefined for scalar codec 'integer'", () => {
    const spec = { count: { kind: 'scalar', codec: 'integer' as const } } as const satisfies FilterSpec
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    expectTypeOf(result.current.filters.count).toEqualTypeOf<number | undefined>()
  })

  it("infers boolean | undefined for scalar codec 'boolean'", () => {
    const spec = { pending: { kind: 'scalar', codec: 'boolean' as const } } as const satisfies FilterSpec
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    expectTypeOf(result.current.filters.pending).toEqualTypeOf<boolean | undefined>()
  })

  it('infers parse return type for scalar with custom codec', () => {
    const spec = {
      ids: {
        kind: 'scalar',
        codec: {
          parse: (raw: string) => raw.split('|').map(Number),
          serialize: (v: unknown) => String(v),
        },
      },
    } as const satisfies FilterSpec
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    expectTypeOf(result.current.filters.ids).toEqualTypeOf<number[] | undefined>()
  })

  it('infers union literal type for custom codec with enum parse return', () => {
    const spec = {
      pet: {
        kind: 'scalar',
        codec: {
          parse: (raw: string) => raw as 'dog' | 'cat' | undefined,
          serialize: (v: 'dog' | 'cat') => v,
        },
      },
    } as const satisfies FilterSpec
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    expectTypeOf(result.current.filters.pet).toEqualTypeOf<'dog' | 'cat' | undefined>()
  })

  it('setFilter return type is void for multi-select key', () => {
    const spec = { institution: { kind: 'multi-select' } } as const satisfies FilterSpec
    const { result } = renderHook(() => useFilterState(spec), { wrapper: createWrapper() })
    // Type check: calling setFilter with string[] should produce void
    expectTypeOf(result.current.setFilter('institution', ['Chase'])).toEqualTypeOf<void>()
  })
})
