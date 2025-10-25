import { describe, it, expect, beforeEach, vi } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import Cookies from 'js-cookie';
import { useCookieState } from './useCookieState';

// Mock js-cookie
vi.mock('js-cookie', () => ({
  default: {
    get: vi.fn(),
    set: vi.fn(),
    remove: vi.fn(),
  },
}));

// Typed mocks for better TypeScript support
const mockCookies = Cookies as unknown as {
  get: ReturnType<typeof vi.fn<(name: string) => string | undefined>>;
  set: ReturnType<typeof vi.fn<(name: string, value: string, options?: Cookies.CookieAttributes) => string | undefined>>;
  remove: ReturnType<typeof vi.fn<(name: string) => void>>;
};

describe('useCookieState', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  describe('Basic functionality', () => {
    it('should return undefined for non-existent cookie', () => {
      mockCookies.get.mockReturnValue(undefined);

      const { result } = renderHook(() => useCookieState('test-cookie'));

      expect(result.current[0]).toBeUndefined();
      expect(Cookies.get).toHaveBeenCalledWith('test-cookie');
    });

    it('should return parsed JSON object when cookie contains JSON', () => {
      const testData = { foo: 'bar', count: 42 };
      mockCookies.get.mockReturnValue(JSON.stringify(testData));

      const { result } = renderHook(() => useCookieState('test-cookie'));

      expect(result.current[0]).toEqual(testData);
    });

    it('should return string when cookie contains non-JSON', () => {
      mockCookies.get.mockReturnValue('simple-string');

      const { result } = renderHook(() => useCookieState('test-cookie'));

      expect(result.current[0]).toBe('simple-string');
    });

    it('should update cookie and state when setValue is called with object', () => {
      mockCookies.get.mockReturnValue(undefined);

      const { result } = renderHook(() => useCookieState('test-cookie'));

      const newValue = { hello: 'world' };
      act(() => {
        result.current[1](newValue);
      });

      expect(mockCookies.set).toHaveBeenCalledWith('test-cookie', JSON.stringify(newValue), undefined);
      expect(result.current[0]).toEqual(newValue);
    });

    it('should update cookie and state when setValue is called with string', () => {
      mockCookies.get.mockReturnValue(undefined);

      const { result } = renderHook(() => useCookieState('test-cookie'));

      act(() => {
        result.current[1]('test-value');
      });

      expect(mockCookies.set).toHaveBeenCalledWith('test-cookie', 'test-value', undefined);
      expect(result.current[0]).toBe('test-value');
    });

    it('should remove cookie and clear state when removeCookie is called', () => {
      const testData = { foo: 'bar' };
      mockCookies.get.mockReturnValue(JSON.stringify(testData));

      const { result } = renderHook(() => useCookieState('test-cookie'));

      expect(result.current[0]).toEqual(testData);

      act(() => {
        result.current[2](); // removeCookie
      });

      expect(mockCookies.remove).toHaveBeenCalledWith('test-cookie');
      expect(result.current[0]).toBeUndefined();
    });
  });

  describe('Cookie options', () => {
    it('should pass expires option to mockCookies.set', () => {
      mockCookies.get.mockReturnValue(undefined);

      const { result } = renderHook(() => useCookieState('test-cookie'));

      const expires = new Date('2026-01-01');
      act(() => {
        result.current[1]({ data: 'test' }, { expires });
      });

      expect(mockCookies.set).toHaveBeenCalledWith(
        'test-cookie',
        JSON.stringify({ data: 'test' }),
        { expires }
      );
    });

    it('should pass path option to mockCookies.set', () => {
      mockCookies.get.mockReturnValue(undefined);

      const { result } = renderHook(() => useCookieState('test-cookie'));

      act(() => {
        result.current[1]('value', { path: '/custom-path' });
      });

      expect(mockCookies.set).toHaveBeenCalledWith('test-cookie', 'value', { path: '/custom-path' });
    });

    it('should pass sameSite option to mockCookies.set', () => {
      mockCookies.get.mockReturnValue(undefined);

      const { result } = renderHook(() => useCookieState('test-cookie'));

      act(() => {
        result.current[1]('value', { sameSite: 'strict' });
      });

      expect(mockCookies.set).toHaveBeenCalledWith('test-cookie', 'value', { sameSite: 'strict' });
    });

    it('should pass secure option to mockCookies.set', () => {
      mockCookies.get.mockReturnValue(undefined);

      const { result } = renderHook(() => useCookieState('test-cookie'));

      act(() => {
        result.current[1]('value', { secure: true });
      });

      expect(mockCookies.set).toHaveBeenCalledWith('test-cookie', 'value', { secure: true });
    });

    it('should pass combined options to mockCookies.set', () => {
      mockCookies.get.mockReturnValue(undefined);

      const { result } = renderHook(() => useCookieState('test-cookie'));

      const expires = new Date('2026-01-01');
      const options = {
        expires,
        path: '/',
        sameSite: 'strict' as const,
        secure: true,
      };

      act(() => {
        result.current[1]({ data: 'test' }, options);
      });

      expect(mockCookies.set).toHaveBeenCalledWith(
        'test-cookie',
        JSON.stringify({ data: 'test' }),
        options
      );
    });
  });

  describe('JSON serialization', () => {
    it('should handle complex nested objects', () => {
      const complexObject = {
        user: { name: 'John', age: 30 },
        settings: { theme: 'dark', notifications: true },
        array: [1, 2, 3],
      };
      mockCookies.get.mockReturnValue(JSON.stringify(complexObject));

      const { result } = renderHook(() => useCookieState('test-cookie'));

      expect(result.current[0]).toEqual(complexObject);
    });

    it('should handle arrays', () => {
      const arrayData = ['item1', 'item2', 'item3'];
      mockCookies.get.mockReturnValue(JSON.stringify(arrayData));

      const { result } = renderHook(() => useCookieState('test-cookie'));

      expect(result.current[0]).toEqual(arrayData);
    });

    it('should handle Unicode characters in JSON', () => {
      const unicodeData = { message: 'Hello 世界 🌍' };
      mockCookies.get.mockReturnValue(JSON.stringify(unicodeData));

      const { result } = renderHook(() => useCookieState('test-cookie'));

      expect(result.current[0]).toEqual(unicodeData);
    });

    it('should serialize complex objects when setting', () => {
      mockCookies.get.mockReturnValue(undefined);

      const { result } = renderHook(() => useCookieState('test-cookie'));

      const complexData = {
        nested: { deep: { value: 42 } },
        array: [{ id: 1 }, { id: 2 }],
      };

      act(() => {
        result.current[1](complexData);
      });

      expect(mockCookies.set).toHaveBeenCalledWith(
        'test-cookie',
        JSON.stringify(complexData),
        undefined
      );
    });
  });

  describe('Edge cases', () => {
    it('should handle empty string value', () => {
      mockCookies.get.mockReturnValue('');

      const { result } = renderHook(() => useCookieState('test-cookie'));

      expect(result.current[0]).toBe('');
    });

    it('should handle cookie that exists but has malformed JSON', () => {
      mockCookies.get.mockReturnValue('{invalid json}');

      const { result } = renderHook(() => useCookieState('test-cookie'));

      // Should fall back to returning the raw string
      expect(result.current[0]).toBe('{invalid json}');
    });

    it('should handle boolean values', () => {
      mockCookies.get.mockReturnValue(undefined);

      const { result } = renderHook(() => useCookieState('test-cookie'));

      act(() => {
        result.current[1](true);
      });

      expect(mockCookies.set).toHaveBeenCalledWith('test-cookie', 'true', undefined);
    });

    it('should handle number values', () => {
      mockCookies.get.mockReturnValue(undefined);

      const { result } = renderHook(() => useCookieState('test-cookie'));

      act(() => {
        result.current[1](42);
      });

      expect(mockCookies.set).toHaveBeenCalledWith('test-cookie', '42', undefined);
    });

    it('should handle null values', () => {
      mockCookies.get.mockReturnValue(undefined);

      const { result } = renderHook(() => useCookieState('test-cookie'));

      act(() => {
        result.current[1](null);
      });

      expect(mockCookies.set).toHaveBeenCalledWith('test-cookie', 'null', undefined);
    });
  });

  describe('TypeScript type safety', () => {
    it('should work with typed generic parameter', () => {
      interface TestData {
        id: string;
        name: string;
      }

      const testData: TestData = { id: '123', name: 'Test' };
      mockCookies.get.mockReturnValue(JSON.stringify(testData));

      const { result } = renderHook(() => useCookieState<TestData>('test-cookie'));

      expect(result.current[0]).toEqual(testData);

      act(() => {
        result.current[1]({ id: '456', name: 'Updated' });
      });

      expect(result.current[0]).toEqual({ id: '456', name: 'Updated' });
    });
  });

  describe('React state updates', () => {
    it('should trigger re-render when cookie value changes', () => {
      mockCookies.get.mockReturnValue(undefined);

      const { result } = renderHook(() => useCookieState('test-cookie'));

      expect(result.current[0]).toBeUndefined();

      act(() => {
        result.current[1]('first-value');
      });

      expect(result.current[0]).toBe('first-value');

      act(() => {
        result.current[1]('second-value');
      });

      expect(result.current[0]).toBe('second-value');
    });
  });
});
