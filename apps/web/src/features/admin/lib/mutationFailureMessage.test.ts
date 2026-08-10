import { describe, it, expect } from 'bun:test'
import { mutationFailureMessage } from './mutationFailureMessage'

describe('mutationFailureMessage', () => {
  it('prefers the server-supplied detail over the transport message', () => {
    // A 409 from RetryDLQJob: axios's own `message` is useless here, the `detail` is the
    // only text that tells the operator nothing was enqueued.
    const error = Object.assign(new Error('Request failed with status code 409'), {
      response: {
        status: 409,
        data: { detail: 'a matching job already exists for this kind; retry was deduplicated and not enqueued' },
      },
    })

    expect(mutationFailureMessage(error)).toBe(
      'a matching job already exists for this kind; retry was deduplicated and not enqueued'
    )
  })

  it('falls back to the Error message when there is no response body', () => {
    expect(mutationFailureMessage(new Error('Network Error'))).toBe('Network Error')
  })

  it('falls back to the Error message when the response body has no detail', () => {
    const error = Object.assign(new Error('Request failed with status code 500'), {
      response: { status: 500, data: {} },
    })

    expect(mutationFailureMessage(error)).toBe('Request failed with status code 500')
  })

  it('never returns an empty string, so the toast is never blank', () => {
    expect(mutationFailureMessage(undefined)).toBe('unknown error')
    expect(mutationFailureMessage(null)).toBe('unknown error')
    expect(mutationFailureMessage('boom')).toBe('unknown error')
    expect(mutationFailureMessage(new Error(''))).toBe('unknown error')
  })
})
