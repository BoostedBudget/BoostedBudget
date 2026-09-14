import { expect, type Page } from '@playwright/test'

const PRIVACY_PATH = '/api/v1/user/privacy'

/** Register before mockAuthDependencies so missing privacy interception fails without a live request. */
export async function rejectUnmockedPrivacy(page: Page): Promise<void> {
  await page.route(url => url.pathname === PRIVACY_PATH, route =>
    route.fulfill({ status: 401, json: { detail: 'Privacy dependency was not mocked' } })
  )
}

/** Observe both promises immediately, including when navigation fails. */
export async function navigateWithPrivacyLoad(
  page: Page,
  navigate: () => Promise<unknown>,
): Promise<void> {
  const [response] = await Promise.all([
    page.waitForResponse(response =>
      new URL(response.url()).pathname === PRIVACY_PATH &&
      response.request().method() === 'GET'
    ),
    navigate(),
  ])
  expect(response.status(), 'authenticated privacy settings must load from the mocked privacy API').toBe(200)
}
