import { describe, expect, test } from 'bun:test'
import type { Page, Response } from '@playwright/test'
import { navigateWithPrivacyLoad } from './privacy-consent'

function response(method: string, status: number): Response {
  return {
    url: () => 'http://localhost/api/v1/user/privacy',
    request: () => ({ method: () => method }),
    status: () => status,
  } as Response
}

describe('navigateWithPrivacyLoad', () => {
  test('waits for the successful privacy GET and ignores a privacy PUT', async () => {
    const putResponse = response('PUT', 204)
    const getResponse = response('GET', 200)
    let navigated = false
    const page = {
      waitForResponse: async (predicate: (response: Response) => boolean) => {
        if (predicate(putResponse)) return putResponse
        if (predicate(getResponse)) return getResponse
        throw new Error('No privacy response matched')
      },
    } as Page

    await navigateWithPrivacyLoad(page, async () => {
      navigated = true
    })

    expect(navigated).toBe(true)
  })
})
