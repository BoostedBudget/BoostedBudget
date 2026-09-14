import { expect, type Page } from '@playwright/test'

/** Register before mockAuthDependencies so missing privacy interception fails without a live request. */
export async function rejectUnmockedPrivacy(page: Page) {
  await page.route(url => url.pathname === '/api/v1/user/privacy', route =>
    route.fulfill({ status: 401, json: { detail: 'Privacy dependency was not mocked' } })
  )
}

/** Observe both promises immediately, including when navigation fails. */
export async function navigateWithPrivacyLoad(page: Page, navigate: () => Promise<unknown>) {
  const [response] = await Promise.all([
    page.waitForResponse(response =>
      new URL(response.url()).pathname === '/api/v1/user/privacy' &&
      response.request().method() === 'GET'
    ),
    navigate(),
  ])
  expect(response.status(), 'authenticated privacy settings must load from the mocked privacy API').toBe(200)
}
