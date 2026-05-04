import type { Page } from '@playwright/test'
import { apiClient } from './client'

export interface BudgetFixture {
  id: string
  name: string
  description: string
}

const FIXTURE_DESCRIPTION = 'e2e-fixture-v1'
const FIXTURE_NAME = 'E2E Monthly Budget'

// Checks GET /api/v1/budgets for fixture budget. If not found, creates one via POST.
// Makes specs resilient to seed drift while failing loudly on assertion errors.
export async function ensureBudgetExists(page: Page): Promise<BudgetFixture> {
  const client = await apiClient(page)

  const listResponse = await client.get('/api/v1/budgets')

  if (listResponse.status() === 401) {
    // Clerk not yet hydrated in beforeAll; SQL seed creates fixture budget
    console.warn(
      'ensureBudgetExists: auth unavailable — trusting SQL seed for fixture budget'
    )
    return { id: 'seed-placeholder', name: FIXTURE_NAME, description: FIXTURE_DESCRIPTION }
  }

  if (listResponse.ok()) {
    const body = await listResponse.json()
    const budgets: Array<{ id: string; name: string; description: string }> =
      body.budgets ?? body ?? []
    const existing = budgets.find((b) => b.description === FIXTURE_DESCRIPTION)
    if (existing) return existing
  }

  const now = new Date()
  const startDate = new Date(now.getFullYear(), now.getMonth(), 1)
    .toISOString()
    .split('T')[0]

  const createResponse = await client.post('/api/v1/budgets', {
    name: FIXTURE_NAME,
    description: FIXTURE_DESCRIPTION,
    period: 'monthly',
    start_date: `${startDate}T00:00:00Z`,
    amount: 5000,
    expected_income: 7000,
    currency_code: 'USD',
  })

  if (createResponse.status() === 401) {
    console.warn(
      'ensureBudgetExists: auth unavailable for create — trusting SQL seed for fixture budget'
    )
    return { id: 'seed-placeholder', name: FIXTURE_NAME, description: FIXTURE_DESCRIPTION }
  }

  if (!createResponse.ok()) {
    const body = await createResponse.text()
    throw new Error(
      `ensureBudgetExists: failed to create fixture budget (${createResponse.status()}): ${body}`
    )
  }

  const created = await createResponse.json()
  return created.budget ?? created
}
