import { test, expect, type Page } from '@playwright/test'
import type { PrivacySettings } from '@boostedbudget/api-client/models'
import { mockAuthDependencies } from './helpers/api-mocks'
import { navigateWithPrivacyLoad } from './helpers/privacy-consent'

const privacyPath = '/api/v1/user/privacy'

// Browser fetch exercises page.route; APIRequestContext would bypass the helper.
async function request(page: Page, path = privacyPath, method = 'GET', body?: unknown) {
  return page.evaluate(async ({ path, method, body }) => {
    const response = await fetch(path, {
      method,
      headers: { 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body),
    })
    return { status: response.status, body: await response.json() }
  }, { path, method, body })
}

async function prepare(page: Page) {
  // Register fallback first: unhandled requests must never reach a live API.
  await page.route('**/api/v1/**', route => route.fulfill({
    status: 418, json: { sentinel: 'unhandled' },
  }))
  await mockAuthDependencies(page)
  await page.route('**/privacy-contract', route => route.fulfill({
    contentType: 'text/html', body: '<!doctype html><title>Privacy route contract</title>',
  }))
  await page.goto('/privacy-contract')
}

test('privacy GET and PUT return the flat contract and retain only supported boolean updates', async ({ page, context }) => {
  await prepare(page)
  const initial = await request(page)
  expect(initial.status).toBe(200)
  const contract = {
    id: expect.any(String), user_id: expect.any(String),
    created_at: expect.any(String), updated_at: expect.any(String),
    gdpr_consent_timestamp: expect.any(String),
    gdpr_necessary_consent: true, gdpr_analytics_consent: false,
    gdpr_advertising_consent: false, data_sharing_enabled: false,
    include_in_aggregate_stats: true, marketing_emails_enabled: false,
    product_updates_enabled: true, security_alerts_enabled: true,
  } satisfies { [Field in keyof PrivacySettings]: unknown }
  expect(initial.body).toEqual(contract)

  const updates = {
    gdpr_necessary_consent: true, gdpr_analytics_consent: true,
    gdpr_advertising_consent: true, data_sharing_enabled: true,
    include_in_aggregate_stats: false, marketing_emails_enabled: true,
    product_updates_enabled: false, security_alerts_enabled: false,
  }
  const updated = await request(page, `${privacyPath}?source=consent`, 'PUT', {
    ...updates, id: 'forged', user_id: 'forged', created_at: 'forged',
    updated_at: 'forged', gdpr_consent_timestamp: 'forged', unsupported: true,
  })
  expect(updated).toEqual({ status: 200, body: { ...initial.body, ...updates } })
  expect(await request(page, privacyPath, 'PUT', {
    gdpr_analytics_consent: 'false', security_alerts_enabled: null,
  })).toEqual(updated)
  await page.reload()
  expect(await request(page)).toEqual(updated)

  const otherPage = await context.newPage()
  try {
    await prepare(otherPage)
    expect(await request(otherPage)).toEqual(initial)
  } finally {
    await otherPage.close()
  }
})

test('privacy overrides win and unrelated paths and methods fall through', async ({ page }) => {
  await prepare(page)
  for (const method of ['POST', 'PATCH', 'DELETE']) {
    expect(await request(page, privacyPath, method, {})).toEqual({
      status: 418, body: { sentinel: 'unhandled' },
    })
  }
  for (const path of [`${privacyPath}/export`, `${privacyPath}-other`, '/api/v1/unrelated']) {
    expect(await request(page, path)).toEqual({ status: 418, body: { sentinel: 'unhandled' } })
  }
  await page.route(url => url.pathname === privacyPath, route => route.fulfill({
    status: 403, json: { detail: 'Privacy update refused' },
  }))
  for (const method of ['GET', 'PUT']) {
    expect(await request(page, privacyPath, method, method === 'PUT' ? {} : undefined)).toEqual({
      status: 403, body: { detail: 'Privacy update refused' },
    })
  }
})

test('privacy navigation waits for the server-authoritative GET without a PUT', async ({ page }) => {
  const methods: string[] = []
  await page.route(url => url.pathname === privacyPath, route => {
    methods.push(route.request().method())
    return route.fulfill({ status: 200, json: {} })
  })
  await page.route('**/privacy-load-contract', route => route.fulfill({
    contentType: 'text/html',
    body: `<script>fetch('${privacyPath}')</script>`,
  }))

  await navigateWithPrivacyLoad(page, () => page.goto('/privacy-load-contract'))

  expect(methods).toEqual(['GET'])
})
