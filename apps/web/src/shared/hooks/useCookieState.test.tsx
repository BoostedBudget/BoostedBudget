import { describe, it, expect, beforeEach, mock } from 'bun:test'
import { renderHook, act } from '@testing-library/react'
import { useCookieState } from './useCookieState'

// Create mock functions for js-cookie
const mockGet = mock(() => undefined as string | undefined)
const mockSet = mock(() => undefined as string | undefined)
const mockRemove = mock(() => {})

// Mock js-cookie module
mock.module('js-cookie', () => ({
  default: {
    get: mockGet,
    set: mockSet,
    remove: mockRemove,
  },
}))

describe('useCookieState', () => {
  beforeEach(() => {
    mockGet.mockClear()
    mockSet.mockClear()
    mockRemove.mockClear()
  })

  describe('Basic functionality', () => {
    it('should return undefined for non-existent cookie', () => {
      mockGet.mockReturnValue(undefined)

      const { result } = renderHook(() => useCookieState('test-cookie'))

      expect(result.current[0]).toBeUndefined()
      expect(mockGet).toHaveBeenCalledWith('test-cookie')
    })

    it('should return parsed JSON object when cookie contains JSON', () => {
      const testData = { foo: 'bar', count: 42 }
      mockGet.mockReturnValue(JSON.stringify(testData))

      const { result } = renderHook(() => useCookieState('test-cookie'))

      expect(result.current[0]).toEqual(testData)
    })

    it('should return string when cookie contains non-JSON', () => {
      mockGet.mockReturnValue('simple-string')

      const { result } = renderHook(() => useCookieState('test-cookie'))

      expect(result.current[0]).toBe('simple-string')
    })

    it('should update cookie and state when setValue is called with object', () => {
      mockGet.mockReturnValue(undefined)

      const { result } = renderHook(() => useCookieState('test-cookie'))

      const newValue = { hello: 'world' }
      act(() => {
        result.current[1](newValue)
      })

      expect(mockSet).toHaveBeenCalledWith('test-cookie', JSON.stringify(newValue), undefined)
      expect(result.current[0]).toEqual(newValue)
    })

    it('should update cookie and state when setValue is called with string', () => {
      mockGet.mockReturnValue(undefined)

      const { result } = renderHook(() => useCookieState('test-cookie'))

      act(() => {
        result.current[1]('test-value')
      })

      expect(mockSet).toHaveBeenCalledWith('test-cookie', 'test-value', undefined)
      expect(result.current[0]).toBe('test-value')
    })

    it('should remove cookie and clear state when removeCookie is called', () => {
      const testData = { foo: 'bar' }
      mockGet.mockReturnValue(JSON.stringify(testData))

      const { result } = renderHook(() => useCookieState('test-cookie'))

      expect(result.current[0]).toEqual(testData)

      act(() => {
        result.current[2]() // removeCookie
      })

      expect(mockRemove).toHaveBeenCalledWith('test-cookie')
      expect(result.current[0]).toBeUndefined()
    })
  })

  describe('Cookie options', () => {
    it('should pass expires option to set', () => {
      mockGet.mockReturnValue(undefined)

      const { result } = renderHook(() => useCookieState('test-cookie'))

      const expires = new Date('2026-01-01')
      act(() => {
        result.current[1]({ data: 'test' }, { expires })
      })

      expect(mockSet).toHaveBeenCalledWith(
        'test-cookie',
        JSON.stringify({ data: 'test' }),
        { expires }
      )
    })

    it('should pass path option to set', () => {
      mockGet.mockReturnValue(undefined)

      const { result } = renderHook(() => useCookieState('test-cookie'))

      act(() => {
        result.current[1]('value', { path: '/custom-path' })
      })

      expect(mockSet).toHaveBeenCalledWith('test-cookie', 'value', { path: '/custom-path' })
    })

    it('should pass sameSite option to set', () => {
      mockGet.mockReturnValue(undefined)

      const { result } = renderHook(() => useCookieState('test-cookie'))

      act(() => {
        result.current[1]('value', { sameSite: 'strict' })
      })

      expect(mockSet).toHaveBeenCalledWith('test-cookie', 'value', { sameSite: 'strict' })
    })

    it('should pass secure option to set', () => {
      mockGet.mockReturnValue(undefined)

      const { result } = renderHook(() => useCookieState('test-cookie'))

      act(() => {
        result.current[1]('value', { secure: true })
      })

      expect(mockSet).toHaveBeenCalledWith('test-cookie', 'value', { secure: true })
    })

    it('should pass combined options to set', () => {
      mockGet.mockReturnValue(undefined)

      const { result } = renderHook(() => useCookieState('test-cookie'))

      const expires = new Date('2026-01-01')
      const options = {
        expires,
        path: '/',
        sameSite: 'strict' as const,
        secure: true,
      }

      act(() => {
        result.current[1]({ data: 'test' }, options)
      })

      expect(mockSet).toHaveBeenCalledWith(
        'test-cookie',
        JSON.stringify({ data: 'test' }),
        options
      )
    })
  })

  describe('JSON serialization', () => {
    it('should handle complex nested objects', () => {
      const complexObject = {
        user: { name: 'John', age: 30 },
        settings: { theme: 'dark', notifications: true },
        array: [1, 2, 3],
      }
      mockGet.mockReturnValue(JSON.stringify(complexObject))

      const { result } = renderHook(() => useCookieState('test-cookie'))

      expect(result.current[0]).toEqual(complexObject)
    })

    it('should handle arrays', () => {
      const arrayData = ['item1', 'item2', 'item3']
      mockGet.mockReturnValue(JSON.stringify(arrayData))

      const { result } = renderHook(() => useCookieState('test-cookie'))

      expect(result.current[0]).toEqual(arrayData)
    })

    it('should handle Unicode characters in JSON', () => {
      const unicodeData = { message: 'Hello 世界 🌍' }
      mockGet.mockReturnValue(JSON.stringify(unicodeData))

      const { result } = renderHook(() => useCookieState('test-cookie'))

      expect(result.current[0]).toEqual(unicodeData)
    })

    it('should serialize complex objects when setting', () => {
      mockGet.mockReturnValue(undefined)

      const { result } = renderHook(() => useCookieState('test-cookie'))

      const complexData = {
        nested: { deep: { value: 42 } },
        array: [{ id: 1 }, { id: 2 }],
      }

      act(() => {
        result.current[1](complexData)
      })

      expect(mockSet).toHaveBeenCalledWith(
        'test-cookie',
        JSON.stringify(complexData),
        undefined
      )
    })
  })

  describe('Edge cases', () => {
    it('should handle empty string value', () => {
      mockGet.mockReturnValue('')

      const { result } = renderHook(() => useCookieState('test-cookie'))

      expect(result.current[0]).toBe('')
    })

    it('should handle cookie that exists but has malformed JSON', () => {
      mockGet.mockReturnValue('{invalid json}')

      const { result } = renderHook(() => useCookieState('test-cookie'))

      // Should fall back to returning the raw string
      expect(result.current[0]).toBe('{invalid json}')
    })

    it('should handle boolean values', () => {
      mockGet.mockReturnValue(undefined)

      const { result } = renderHook(() => useCookieState('test-cookie'))

      act(() => {
        result.current[1](true)
      })

      expect(mockSet).toHaveBeenCalledWith('test-cookie', 'true', undefined)
    })

    it('should handle number values', () => {
      mockGet.mockReturnValue(undefined)

      const { result } = renderHook(() => useCookieState('test-cookie'))

      act(() => {
        result.current[1](42)
      })

      expect(mockSet).toHaveBeenCalledWith('test-cookie', '42', undefined)
    })

    it('should handle null values', () => {
      mockGet.mockReturnValue(undefined)

      const { result } = renderHook(() => useCookieState('test-cookie'))

      act(() => {
        result.current[1](null)
      })

      expect(mockSet).toHaveBeenCalledWith('test-cookie', 'null', undefined)
    })
  })

  describe('TypeScript type safety', () => {
    it('should work with typed generic parameter', () => {
      interface TestData {
        id: string
        name: string
      }

      const testData: TestData = { id: '123', name: 'Test' }
      mockGet.mockReturnValue(JSON.stringify(testData))

      const { result } = renderHook(() => useCookieState<TestData>('test-cookie'))

      expect(result.current[0]).toEqual(testData)

      act(() => {
        result.current[1]({ id: '456', name: 'Updated' })
      })

      expect(result.current[0]).toEqual({ id: '456', name: 'Updated' })
    })
  })

  describe('React state updates', () => {
    it('should trigger re-render when cookie value changes', () => {
      mockGet.mockReturnValue(undefined)

      const { result } = renderHook(() => useCookieState('test-cookie'))

      expect(result.current[0]).toBeUndefined()

      act(() => {
        result.current[1]('first-value')
      })

      expect(result.current[0]).toBe('first-value')

      act(() => {
        result.current[1]('second-value')
      })

      expect(result.current[0]).toBe('second-value')
    })
  })
})
