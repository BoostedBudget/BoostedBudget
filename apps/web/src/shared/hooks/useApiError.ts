import { AxiosError } from 'axios'
import { Sentry } from '../lib/sentry'

export interface ApiError {
  status: number
  title: string
  detail: string
  code?: string
  requestId?: string
}

/**
 * Parses an unknown error into a typed ApiError.
 * Handles Axios errors with RFC 9457 response bodies.
 */
export function parseApiError(error: unknown): ApiError {
  if (error instanceof AxiosError && error.response) {
    const { status, data, headers } = error.response
    return {
      status,
      title: data?.title ?? 'Error',
      detail: data?.detail ?? data?.message ?? 'An unexpected error occurred',
      code: data?.code,
      requestId: headers?.['x-request-id'],
    }
  }

  if (error instanceof Error) {
    return {
      status: 0,
      title: 'Error',
      detail: error.message,
    }
  }

  return {
    status: 0,
    title: 'Unknown Error',
    detail: 'An unexpected error occurred',
  }
}

/**
 * Hook that provides a handleError function for use in components.
 */
export function useApiErrorHandler(): { handleError: (error: unknown) => ApiError } {
  const handleError = (error: unknown): ApiError => {
    const parsed = parseApiError(error)

    if (parsed.status >= 500 || parsed.status === 0) {
      Sentry.captureException(error instanceof Error ? error : new Error(parsed.detail), {
        extra: { requestId: parsed.requestId, status: parsed.status },
      })
    }

    return parsed
  }

  return { handleError }
}
