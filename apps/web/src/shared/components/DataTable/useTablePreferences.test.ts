import { describe, it, expect, beforeEach } from 'bun:test'
import { renderHook, act } from '@testing-library/react'
import { mockUseUser, mockClerkUser } from '@/test/mocks/clerk'
import { useTablePreferences } from './useTablePreferences'

const TABLE_ID = 'test-table'
const KNOWN_IDS = ['name', 'amount', 'date']
const DEFAULT_VERSION = KNOWN_IDS.join(',')
const USER_ID = mockClerkUser.id
const STORAGE_KEY = `bb:table-prefs:${USER_ID}:${TABLE_ID}`

const DEFAULT_PREFS = {
  sizing: {},
  columnOrder: [],
  columnVisibility: {},
}

function makeStoredPrefs(overrides?: object) {
  return JSON.stringify({
    version: DEFAULT_VERSION,
    sizing: {},
    columnOrder: [],
    columnVisibility: {},
    ...overrides,
  })
}

describe('useTablePreferences', () => {
  beforeEach(() => {
    localStorage.clear()
    mockUseUser.mockReturnValue({ user: null, isLoaded: true, isSignedIn: false })
  })

  // Test 1: User null → returns defaults, setter is a no-op
  describe('when user is null (not authenticated)', () => {
    it('returns defaults', () => {
      const { result } = renderHook(() =>
        useTablePreferences(TABLE_ID, KNOWN_IDS)
      )
      expect(result.current.sizing).toEqual(DEFAULT_PREFS.sizing)
      expect(result.current.columnOrder).toEqual(DEFAULT_PREFS.columnOrder)
      expect(result.current.columnVisibility).toEqual(DEFAULT_PREFS.columnVisibility)
    })

    it('setter is a no-op — nothing written to localStorage', () => {
      const { result } = renderHook(() =>
        useTablePreferences(TABLE_ID, KNOWN_IDS)
      )
      act(() => {
        result.current.onSizingChange({ name: 200 })
      })
      expect(localStorage.getItem(STORAGE_KEY)).toBeNull()
    })

    it('onColumnOrderChange is a no-op', () => {
      const { result } = renderHook(() =>
        useTablePreferences(TABLE_ID, KNOWN_IDS)
      )
      act(() => {
        result.current.onColumnOrderChange(['date', 'name', 'amount'])
      })
      expect(localStorage.getItem(STORAGE_KEY)).toBeNull()
    })

    it('onColumnVisibilityChange is a no-op', () => {
      const { result } = renderHook(() =>
        useTablePreferences(TABLE_ID, KNOWN_IDS)
      )
      act(() => {
        result.current.onColumnVisibilityChange({ amount: false })
      })
      expect(localStorage.getItem(STORAGE_KEY)).toBeNull()
    })
  })

  // Test 2: User logged in → setter writes to the correct key
  describe('when user is logged in', () => {
    beforeEach(() => {
      mockUseUser.mockReturnValue({
        user: mockClerkUser,
        isLoaded: true,
        isSignedIn: true,
      })
    })

    it('writes sizing to localStorage on onSizingChange', () => {
      const { result } = renderHook(() =>
        useTablePreferences(TABLE_ID, KNOWN_IDS)
      )
      act(() => {
        result.current.onSizingChange({ name: 200, amount: 150 })
      })
      const stored = JSON.parse(localStorage.getItem(STORAGE_KEY)!)
      expect(stored.sizing).toEqual({ name: 200, amount: 150 })
    })

    it('writes correct storage key: bb:table-prefs:${userId}:${tableId}', () => {
      const { result } = renderHook(() =>
        useTablePreferences(TABLE_ID, KNOWN_IDS)
      )
      act(() => {
        result.current.onSizingChange({ name: 120 })
      })
      expect(localStorage.getItem(STORAGE_KEY)).not.toBeNull()
      // Other keys must not exist
      expect(localStorage.length).toBe(1)
    })

    it('writes columnOrder on onColumnOrderChange', () => {
      const { result } = renderHook(() =>
        useTablePreferences(TABLE_ID, KNOWN_IDS)
      )
      act(() => {
        result.current.onColumnOrderChange(['date', 'name', 'amount'])
      })
      const stored = JSON.parse(localStorage.getItem(STORAGE_KEY)!)
      expect(stored.columnOrder).toEqual(['date', 'name', 'amount'])
    })

    it('writes columnVisibility on onColumnVisibilityChange', () => {
      const { result } = renderHook(() =>
        useTablePreferences(TABLE_ID, KNOWN_IDS)
      )
      act(() => {
        result.current.onColumnVisibilityChange({ amount: false })
      })
      const stored = JSON.parse(localStorage.getItem(STORAGE_KEY)!)
      expect(stored.columnVisibility).toEqual({ amount: false })
    })

    it('persists the version string as knownColumnIds joined by comma', () => {
      const { result } = renderHook(() =>
        useTablePreferences(TABLE_ID, KNOWN_IDS)
      )
      act(() => {
        result.current.onSizingChange({ name: 100 })
      })
      const stored = JSON.parse(localStorage.getItem(STORAGE_KEY)!)
      expect(stored.version).toBe(DEFAULT_VERSION)
    })
  })

  // Test 3: User A's prefs don't leak to User B
  describe('user isolation', () => {
    it("user A's prefs don't appear under user B's key", () => {
      const userA = { ...mockClerkUser, id: 'user_aaa' }
      const userB = { ...mockClerkUser, id: 'user_bbb' }

      mockUseUser.mockReturnValue({ user: userA, isLoaded: true, isSignedIn: true })
      const { result: resultA } = renderHook(() =>
        useTablePreferences(TABLE_ID, KNOWN_IDS)
      )
      act(() => {
        resultA.current.onSizingChange({ name: 999 })
      })

      mockUseUser.mockReturnValue({ user: userB, isLoaded: true, isSignedIn: true })
      const { result: resultB } = renderHook(() =>
        useTablePreferences(TABLE_ID, KNOWN_IDS)
      )

      // User B's hook should return defaults (nothing stored for userB)
      expect(resultB.current.sizing).toEqual({})

      // User A's data is stored under user A's key
      expect(
        JSON.parse(localStorage.getItem(`bb:table-prefs:user_aaa:${TABLE_ID}`)!).sizing
      ).toEqual({ name: 999 })
      // User B's key doesn't exist
      expect(localStorage.getItem(`bb:table-prefs:user_bbb:${TABLE_ID}`)).toBeNull()
    })
  })

  // Test 4: Corrupted JSON → caught, returns defaults
  describe('corrupted JSON in localStorage', () => {
    it('returns defaults and does not crash', () => {
      mockUseUser.mockReturnValue({
        user: mockClerkUser,
        isLoaded: true,
        isSignedIn: true,
      })
      localStorage.setItem(STORAGE_KEY, '{this is not json!!')

      const { result } = renderHook(() =>
        useTablePreferences(TABLE_ID, KNOWN_IDS)
      )
      expect(result.current.sizing).toEqual({})
      expect(result.current.columnOrder).toEqual([])
      expect(result.current.columnVisibility).toEqual({})
    })
  })

  // Test 5: Unknown column id in stored prefs → dropped on read
  describe('unknown column ids in stored prefs', () => {
    it('drops sizing entries for unknown column ids', () => {
      mockUseUser.mockReturnValue({
        user: mockClerkUser,
        isLoaded: true,
        isSignedIn: true,
      })
      localStorage.setItem(
        STORAGE_KEY,
        makeStoredPrefs({ sizing: { name: 100, unknown_col: 200, amount: 150 } })
      )

      const { result } = renderHook(() =>
        useTablePreferences(TABLE_ID, KNOWN_IDS)
      )
      // unknown_col is not in KNOWN_IDS → dropped
      expect(result.current.sizing).toEqual({ name: 100, amount: 150 })
    })

    it('drops columnOrder entries for unknown column ids', () => {
      mockUseUser.mockReturnValue({
        user: mockClerkUser,
        isLoaded: true,
        isSignedIn: true,
      })
      localStorage.setItem(
        STORAGE_KEY,
        makeStoredPrefs({ columnOrder: ['name', 'ghost_col', 'amount', 'date'] })
      )

      const { result } = renderHook(() =>
        useTablePreferences(TABLE_ID, KNOWN_IDS)
      )
      expect(result.current.columnOrder).toEqual(['name', 'amount', 'date'])
    })

    it('drops columnVisibility entries for unknown column ids', () => {
      mockUseUser.mockReturnValue({
        user: mockClerkUser,
        isLoaded: true,
        isSignedIn: true,
      })
      localStorage.setItem(
        STORAGE_KEY,
        makeStoredPrefs({ columnVisibility: { name: true, phantom: false, amount: false } })
      )

      const { result } = renderHook(() =>
        useTablePreferences(TABLE_ID, KNOWN_IDS)
      )
      expect(result.current.columnVisibility).toEqual({ name: true, amount: false })
    })
  })

  // Test 6: Version mismatch → resets to defaults and persists the reset
  describe('version mismatch (different knownColumnIds)', () => {
    it('resets to defaults when stored version does not match current knownColumnIds', () => {
      mockUseUser.mockReturnValue({
        user: mockClerkUser,
        isLoaded: true,
        isSignedIn: true,
      })
      // Store with old version (different column set)
      localStorage.setItem(
        STORAGE_KEY,
        JSON.stringify({
          version: 'old_col,another_col',
          sizing: { old_col: 300 },
          columnOrder: ['old_col', 'another_col'],
          columnVisibility: { old_col: false },
        })
      )

      const { result } = renderHook(() =>
        useTablePreferences(TABLE_ID, KNOWN_IDS)
      )
      expect(result.current.sizing).toEqual({})
      expect(result.current.columnOrder).toEqual([])
      expect(result.current.columnVisibility).toEqual({})
    })

    it('persists the reset to localStorage with new version', () => {
      mockUseUser.mockReturnValue({
        user: mockClerkUser,
        isLoaded: true,
        isSignedIn: true,
      })
      localStorage.setItem(
        STORAGE_KEY,
        JSON.stringify({
          version: 'stale,version,string',
          sizing: { stale: 100 },
          columnOrder: ['stale'],
          columnVisibility: {},
        })
      )

      renderHook(() => useTablePreferences(TABLE_ID, KNOWN_IDS))

      const stored = JSON.parse(localStorage.getItem(STORAGE_KEY)!)
      expect(stored.version).toBe(DEFAULT_VERSION)
      expect(stored.sizing).toEqual({})
    })
  })

  // Test: two setters fired in sequence — neither save should lose the other's value
  describe('concurrent setter writes', () => {
    it('firing onSizingChange then onColumnOrderChange preserves both values in localStorage', () => {
      mockUseUser.mockReturnValue({
        user: mockClerkUser,
        isLoaded: true,
        isSignedIn: true,
      })

      const { result } = renderHook(() =>
        useTablePreferences(TABLE_ID, KNOWN_IDS)
      )

      act(() => {
        result.current.onSizingChange({ name: 200 })
        result.current.onColumnOrderChange(['amount', 'name', 'date'])
      })

      const stored = JSON.parse(localStorage.getItem(STORAGE_KEY)!)
      // Both values must be present — stale closure would wipe one of them
      expect(stored.sizing).toEqual({ name: 200 })
      expect(stored.columnOrder).toEqual(['amount', 'name', 'date'])
    })
  })

  // Test 7: Re-mount with same user → prefs restored from localStorage
  describe('persistence across remounts', () => {
    it('restores prefs from localStorage on re-mount', () => {
      mockUseUser.mockReturnValue({
        user: mockClerkUser,
        isLoaded: true,
        isSignedIn: true,
      })
      localStorage.setItem(
        STORAGE_KEY,
        makeStoredPrefs({
          sizing: { name: 180, amount: 120 },
          columnOrder: ['amount', 'name', 'date'],
          columnVisibility: { date: false },
        })
      )

      const { result } = renderHook(() =>
        useTablePreferences(TABLE_ID, KNOWN_IDS)
      )
      expect(result.current.sizing).toEqual({ name: 180, amount: 120 })
      expect(result.current.columnOrder).toEqual(['amount', 'name', 'date'])
      expect(result.current.columnVisibility).toEqual({ date: false })
    })
  })
})
