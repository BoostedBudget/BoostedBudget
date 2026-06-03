import { useCallback } from 'react'
import { useSearchParams } from 'react-router-dom'

export interface UseUrlStateOptions<T> {
  key: string
  parse: (raw: string | null) => T
  serialize: (value: T) => string | null
  defaultValue: T
  isDefault?: (value: T) => boolean
}

export interface UseUrlStateReturn<T> {
  value: T
  setValue: (next: T) => void
  reset: () => void
  isDefault: boolean
}

export function useUrlState<T>(opts: UseUrlStateOptions<T>): UseUrlStateReturn<T> {
  const { key, parse, serialize, defaultValue, isDefault: isDefaultFn } = opts
  const [searchParams, setSearchParams] = useSearchParams()

  const raw = searchParams.get(key)
  const parsed = raw !== null ? parse(raw) : null
  const value: T = parsed !== null ? parsed : defaultValue

  const isDefault = isDefaultFn
    ? isDefaultFn(value)
    : value === defaultValue || (value === null && defaultValue === null)

  const setValue = useCallback(
    (next: T) => {
      setSearchParams(
        (prev) => {
          const params = new URLSearchParams(prev)
          const serialized = serialize(next)
          if (serialized === null) {
            params.delete(key)
          } else {
            params.set(key, serialized)
          }
          return params
        },
        { replace: true },
      )
    },
    [key, serialize, setSearchParams],
  )

  const reset = useCallback(() => {
    setSearchParams(
      (prev) => {
        const params = new URLSearchParams(prev)
        params.delete(key)
        return params
      },
      { replace: true },
    )
  }, [key, setSearchParams])

  return { value, setValue, reset, isDefault }
}
