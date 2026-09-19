import { describe, expect, test } from 'bun:test'
import { readFileSync } from 'node:fs'
import { parse } from 'yaml'
import { projectResponse, responseCatalog, responseProfiles } from './projection'
import { validateResponseCatalog } from './catalog-validation'
import specIndex from '../spec/generated/spec-index.json'

const spec = parse(readFileSync(new URL('../../../../api/openapi.yaml', import.meta.url), 'utf8'))
const exposed = specIndex.entries.filter((entry) => !entry.excluded).map((entry) => entry.operationId)

describe('response contract', () => {
  test('compact profile retains access confirmation and source while omitting provider identities', () => {
    for (const access of [
      { access_resolved: false, access_source: '' },
      { access_resolved: true, access_source: 'comp' },
    ]) {
      const visible = { id: 'user', subscription_tier: 'premium', subscription_status: 'active', ...access }
      const result = projectResponse('getUserProfile', { ...visible, clerk_user_id: 'private', paddle_customer_id: 'private' })
      expect(result).toEqual({ value: visible, reduced: true })
    }
  })

  test('compact retains grouped selectors and the budget-counted transaction amount', () => {
    const group = { id: 'group', category_kind: 'group', group_name: 'Everyday', members: [{ category_kind: 'uncategorized' }] }
    expect(projectResponse('getBudget', { id: 'budget', categories: [{ ...group, created_at: 'noise' }] }).value).toEqual({ id: 'budget', categories: [group] })
    const transaction = { id: 'transaction', amount: 100, budget_expense_amount: 70 }
    const result = projectResponse('getCategoryTransactions', { total_amount: 70, transactions: [{ ...transaction, created_at: 'noise' }] })
    expect(result.value).toMatchObject({ total_amount: 70, transactions: [transaction] })
    expect((result.value as { transactions: Record<string, unknown>[] }).transactions[0]).not.toHaveProperty('created_at')
  })

  test('all exposed operations have a deliberate policy and schema fields are exhaustively classified', () => {
    expect(validateResponseCatalog(spec, exposed)).toEqual([])
    expect(Object.values(responseCatalog).filter((entry) => entry.kind === 'project')).toHaveLength(71)
    expect(Object.keys(responseCatalog)).toHaveLength(142)
  })

  test('new and removed schema fields invalidate the catalog', () => {
    const changed = structuredClone(spec)
    changed.components.schemas.Transaction.properties.new_financial_state = { type: 'string' }
    expect(validateResponseCatalog(changed, exposed).join(' ')).toContain('new_financial_state')
    delete changed.components.schemas.Transaction.properties.amount
    expect(validateResponseCatalog(changed, exposed).join(' ')).toContain('amount')
    expect(validateResponseCatalog(spec, [...exposed, 'futureOperation']).join(' ')).toContain('futureOperation')
  })

  test('full-detail recovery can never point at a mutation', () => {
    const changed = structuredClone(spec)
    changed.paths['/api/v1/accounts'].post = changed.paths['/api/v1/accounts'].get
    delete changed.paths['/api/v1/accounts'].get
    expect(validateResponseCatalog(changed, exposed).join(' ')).toContain('getAccounts: recovery must be an admitted GET read')
  })

  test('boost execution recovery survives deletion of the parent boost', () => {
    for (const operationId of ['getBoostExecutions', 'getUserBoostExecutions']) {
      expect(responseCatalog[operationId]).toMatchObject({ kind: 'project', recoveryOperationId: 'getUserBoostExecutions' })
    }
  })

  test('compact preserves financial invariants, envelope and null members', () => {
    const value = { $schema: 'https://api/schema', transactions: [null, { id: 'tx', amount: 0, pending: false, currency_code: 'CAD', tags: null, user_notes: '', user_id: 'caller', source: 'banking', data_source: 'plaid' }], limit: 25, total_count: 0, total_amount: -0, next_cursor: '' }
    const result = projectResponse('getTransactions', value)
    expect(result.reduced).toBe(true)
    expect(result.value).toEqual({ transactions: [null, { id: 'tx', amount: 0, pending: false, currency_code: 'CAD', tags: null, user_notes: '', source: 'banking', data_source: 'plaid' }], limit: 25, total_count: 0, total_amount: -0, next_cursor: '' })
    expect(projectResponse('getTransactions', value, 'full')).toEqual({ value, reduced: false })
  })

  test('custom and overridden categories never acquire stale provider displays', () => {
    const base = { id: 'tx', pfc_primary_display: 'Provider', pfc_detailed_display: 'Detail', pfc_primary_override_display: null, pfc_detailed_override_display: 'Override detail' }
    expect(projectResponse('getTransactionByID', { ...base, category_override_kind: 'pfc' }).value).toEqual({ id: 'tx', category_override_kind: 'pfc', effective_pfc_primary_display: null, effective_pfc_detailed_display: 'Override detail', reversed: false })
    expect(projectResponse('getTransactionByID', { ...base, category_override_kind: 'custom' }).value).toEqual({ id: 'tx', category_override_kind: 'custom', reversed: false })
    expect(projectResponse('getTransactionByID', { id: 'tx', category_override_kind: 'pfc', pfc_primary_display: 'Provider' }).value).not.toHaveProperty('effective_pfc_primary_display')
  })

  test('bare transaction reversal uses the provider-removed-posted predicate', () => {
    expect(projectResponse('getTransactionByID', { deleted_at: 'date', deleted_reason: 'provider_removed_posted' }).value).toEqual({ reversed: true, reversed_at: 'date' })
    expect(projectResponse('getTransactionByID', { deleted_at: 'date', deleted_reason: 'provider_removed_pending' }).value).toEqual({ reversed: false })
  })

  test('unknown projected fields and incompatible containers fall back atomically', () => {
    for (const value of [
      { transactions: [{ user_id: 'remove' }, { future_field: 1 }] },
      { transactions: [{ account: [] }] },
      { transactions: {} },
      { unexpected: true },
    ]) {
      const result = projectResponse('getTransactions', value)
      expect(result.value).toBe(value)
      expect(result.reduced).toBe(false)
      expect(result.warning).toBeDefined()
      expect(result.warning).not.toContain('future_field')
    }
  })

  test('opaque financial definitions and historical output are preserved', () => {
    const output = { skip_reason: 'limit', future_semantic_key: { amount: 123, user_id: 'meaningful' } }
    const result = projectResponse('getBoostExecutions', { executions: [{ id: 'exec', output_data: output, definition_snapshot: { arbitrary: 1 }, input_data: {}, user_id: 'caller' }], total: 1, limit: 20, offset: 0 })
    expect(result.value).toEqual({ executions: [{ id: 'exec', output_data: output }], total: 1, limit: 20, offset: 0 })
  })

  test('empty collections and already-focused responses are unchanged', () => {
    for (const [operation, value] of [['getUserRules', []], ['getTransactions', { transactions: [] }], ['getPrivacySettings', null], ['getFinancialSummary', { arbitrary: 1 }]] as const) {
      expect(projectResponse(operation, value)).toEqual({ value, reduced: false })
    }
    expect(Object.keys(responseProfiles).length).toBeGreaterThan(30)
  })

  test('closed accounts keep null current and dated last-known balances; embedded identities omit balances', () => {
    const account = { id: 'acct', user_account_id: 'logical', name: 'Bank', institution_name: 'Institution', mask: '1234', status: 'removed', currency_code: 'EUR', current_balance: null, available_balance: null, last_known_balance: 105, last_known_balance_at: '2026-01-01T00:00:00Z', removed_at: '2026-01-02T00:00:00Z', user_id: 'caller', plaid_account_id: 'provider' }
    expect(projectResponse('getAccounts', { accounts: [account], total_count: 1 }).value).toEqual({ accounts: [{ id: 'acct', user_account_id: 'logical', name: 'Bank', institution_name: 'Institution', mask: '1234', status: 'removed', currency_code: 'EUR', current_balance: null, available_balance: null, last_known_balance: 105, last_known_balance_at: '2026-01-01T00:00:00Z', removed_at: '2026-01-02T00:00:00Z' }], total_count: 1 })
    for (const operation of ['getTransactionByID', 'getSavingsGoal']) {
      expect(projectResponse(operation, { account }).value).toMatchObject({ account: { id: 'acct', user_account_id: 'logical', name: 'Bank', institution_name: 'Institution', mask: '1234', status: 'removed', currency_code: 'EUR' } })
      expect((projectResponse(operation, { account }).value as { account: object }).account).not.toHaveProperty('current_balance')
    }
  })

  test('investment activity preserves currency, sort date and economically distinct source fields', () => {
    const row = { id: 'investment', source: 'investment', data_source: 'plaid', amount: -123, date: '2026-01-01', sort_date: '2026-01-02', currency_code: 'CAD', investment_quantity: 1.25, investment_price: 98.4, investment_fees: 0, security_id: 'sec', ticker_symbol: 'ETF', user_id: 'caller' }
    const result = projectResponse('getTransactions', { transactions: [row], limit: 25 })
    expect(result.value).toEqual({ transactions: [{ id: 'investment', source: 'investment', data_source: 'plaid', amount: -123, date: '2026-01-01', sort_date: '2026-01-02', currency_code: 'CAD', investment_quantity: 1.25, investment_price: 98.4, investment_fees: 0, security_id: 'sec', ticker_symbol: 'ETF' }], limit: 25 })
    expect(projectResponse('getTransactions', { transactions: [{ id: 'legacy', source: 'investment' }] }).value).toEqual({ transactions: [{ id: 'legacy', source: 'investment' }] })
  })

  test('household actors and historical annotation identities are not mistaken for caller metadata', () => {
    expect(projectResponse('listHouseholdMembers', [{ user_id: 'other-member', role: 'owner', status: 'active', avatar_url: 'image' }]).value).toEqual([{ user_id: 'other-member', role: 'owner', status: 'active' }])
    expect(projectResponse('listHouseholdInvites', [{ inviter_user_id: 'inviter', accepted_user_id: 'accepted', invitee_email: 'member@example.com', created_at: 'date', updated_at: 'noise' }]).value).toEqual([{ inviter_user_id: 'inviter', accepted_user_id: 'accepted', invitee_email: 'member@example.com', created_at: 'date' }])
    const prior = { user_id: 'historical-value', changed_at: 'historical-date', tags: ['one'] }
    expect(projectResponse('getUserTransactionAnnotationHistory', { changes: [{ id: 'change', user_id: 'caller', transaction_id: 'retired-tx', prior, new: { tags: [] }, changed_at: 'date' }], total: 1, offset: 0, limit: 25 }).value).toEqual({ changes: [{ id: 'change', transaction_id: 'retired-tx', prior, new: { tags: [] }, changed_at: 'date' }], total: 1, offset: 0, limit: 25 })
  })

  test('invalid nested objects cannot partially compact earlier financial records', () => {
    const value = { accounts: [{ id: 'good', user_id: 'caller' }, 7], total_count: 2 }
    expect(projectResponse('getAccounts', value)).toMatchObject({ value, reduced: false, warning: expect.any(String) })
    expect(projectResponse('getAccounts', value).value).toBe(value)
    expect(projectResponse('getAccounts', { accounts: null })).toEqual({ value: { accounts: null }, reduced: false })
  })
})

// Build representative complete objects from the independent OpenAPI schemas. This exercises
// every registered operation's real root/envelope/array topology, not just catalog cardinality.
function schemaFixture(raw: Record<string, unknown>, depth = 0): unknown {
  const schema = raw.$ref ? spec.components.schemas[String(raw.$ref).split('/').at(-1)!] : raw
  if (depth > 12) throw new Error('Unexpected recursive response schema')
  const types = Array.isArray(schema.type) ? schema.type : [schema.type]
  if (schema.properties) return Object.fromEntries(Object.entries(schema.properties).map(([key, value]) => [key, schemaFixture(value as Record<string, unknown>, depth + 1)]))
  if (schema.items) return [schemaFixture(schema.items, depth + 1), null]
  if (types.includes('boolean')) return false
  if (types.includes('integer') || types.includes('number')) return 0
  if (types.includes('string')) return schema.enum?.[0] ?? 'fixture'
  return { opaque_future_semantic_field: { amount: 9, user_id: 'retained' } }
}

describe('every registered response topology', () => {
  for (const [operationId, policy] of Object.entries(responseCatalog)) {
    if (policy.kind !== 'project') continue
    test(operationId, () => {
      const entry = specIndex.entries.find((entry) => entry.operationId === operationId)!
      const response = Object.entries(spec.paths[entry.path][entry.method.toLowerCase()].responses).find(([status]) => status.startsWith('2'))![1] as { content: Record<string, { schema: Record<string, unknown> }> }
      const full = schemaFixture(response.content['application/json']!.schema)
      const before = structuredClone(full)
      const compact = projectResponse(operationId, full)
      expect(compact.warning).toBeUndefined()
      expect(compact.reduced).toBe(true)
      expect(full).toEqual(before)
      expect(projectResponse(operationId, full, 'full')).toEqual({ value: full, reduced: false })
      const bytes = JSON.stringify(compact.value).length
      expect(bytes).toBeLessThan(JSON.stringify(full).length)
      // Runtime drift must fall back even when other list entries could have been reduced.
      const drifted = structuredClone(full) as Record<string, unknown> | Record<string, unknown>[]
      if (Array.isArray(drifted)) drifted[0]!.unknown_financial_field = 1
      else drifted.unknown_financial_field = 1
      expect(projectResponse(operationId, drifted)).toMatchObject({ value: drifted, reduced: false, warning: expect.any(String) })
    })
  }
})

describe('independent semantic preservation contracts', () => {
  test('rule definitions preserve matching, targets, quarantine and concurrency controls', () => {
    const expected = { id: 'rule', name: 'Groceries', description: 'Weekly shop', conditions: [{ field: 'amount', operator: 'gt', value: 5 }], condition_logic: 'and', enabled: false, priority: 2, quarantined: true, quarantine_reason: 'uncoercible_condition_value', version: 9, target_kind: 'pfc', target_pfc_primary: 'FOOD_AND_DRINK', target_pfc_detailed: 'FOOD_AND_DRINK_GROCERIES', target_tags: ['weekly'], tag_behavior: 'append', last_matched_at: 'yesterday', match_count: 5, override_count: 1 }
    expect(projectResponse('getRuleByID', { ...expected, user_id: 'caller', created_at: 'noise', updated_at: 'noise' }).value).toEqual(expected)
  })

  test('boost definitions preserve opaque actions, triggers and execution statistics', () => {
    const expected = { id: 'boost', name: 'Save', description: 'Deposit suggestions', boost_type: 'scheduled', conditions: [{ field: 'balance', value: 100 }], condition_logic: 'or', actions: { unknown_future_action: { amount: 12, user_id: 'semantic' } }, trigger_config: { schedule: '0 0 * * *', created_at: 'semantic' }, priority: 0, enabled: false, execution_count: 12, last_executed_at: 'today', last_successful_at: 'yesterday' }
    expect(projectResponse('getBoostByID', { ...expected, user_id: 'caller', created_at: 'noise', updated_at: 'noise' }).value).toEqual(expected)
  })

  test('budgets and categories preserve income, monetary values and matching relationships', () => {
    const category = { id: 'category', budget_id: 'budget', allocated_amount: 100, spent_amount: 0, category_kind: 'custom', custom_category_id: 'custom', custom_parent_id: 'parent', custom_parent_pfc_primary: 'INCOME', custom_grandparent_pfc_primary: 'OTHER', managed_by_goal_id: 'goal', savings_goal_id: 'goal', savings_goal_name: 'Emergency', match_logic: 'and', tags: ['important'], auto_created: true, pfc_primary: 'INCOME', pfc_primary_display: 'Income', pfc_detailed: 'INCOME_WAGES', pfc_detailed_display: 'Wages' }
    const expected = { id: 'budget', amount: 900, expected_income: 1200, income_amount: 1000, income_sources: [{ category: 'INCOME_WAGES', amount: 1200 }], currency_code: 'CAD', period: 'monthly', start_date: '2026-01-01', end_date: '2026-01-31', series_id: 'series', is_active: false, categories: [category] }
    expect(projectResponse('getBudget', { ...expected, categories: [{ ...category, created_at: 'noise', updated_at: 'noise' }], user_id: 'caller', created_at: 'noise' }).value).toEqual(expected)
    expect(projectResponse('updateBudgetCategory', { ...category, created_at: 'noise' }).value).toEqual(category)
  })

  test('goals preserve calculation freshness and contributions preserve ledger controls', () => {
    const goal = { id: 'goal', target_amount: 1000, current_amount: 0, currency_code: 'EUR', progress_basis: 'contributions', balance_as_of: null, last_calculated_at: 'yesterday', account_id: 'account', user_account_id: 'logical', account_percentage: 50, deadline: '2027-01-01', is_completed: false, completed_at: null, deleted_at: 'today' }
    expect(projectResponse('getSavingsGoal', { ...goal, user_id: 'caller', created_at: 'noise' }).value).toEqual(goal)
    const contribution = { id: 'contribution', savings_goal_id: 'goal', amount: 5, source: 'transaction', transaction_id: 'tx', occurred_on: '2026-01-01', status: 'rejected', confirmed_by_user: false, managed_by_reconciliation: true, archived_at: 'yesterday' }
    expect(projectResponse('rejectSavingsGoalContribution', { ...contribution, created_at: 'noise', updated_at: 'noise' }).value).toEqual(contribution)
  })

  test('holdings and investment transactions preserve values and unavailable-state evidence', () => {
    const holding = { id: 'holding', account_id: 'account', user_account_id: 'logical', security_id: 'security', security_name: 'Fund', security_type: 'etf', ticker_symbol: 'FUND', quantity: 1.25, currency_code: 'EUR', institution_price: 9, institution_value: 11.25, cost_basis: null, gain_loss: null, gain_loss_pct: null, day_change: null, day_change_pct: null, day_change_as_of: '2026-01-01', day_change_is_stale: true, day_change_unavailable_reason: 'currency_mismatch' }
    expect(projectResponse('getHoldings', { holdings: [{ ...holding, user_id: 'caller' }], count: 1 }).value).toEqual({ holdings: [holding], count: 1 })
    const transaction = { id: 'tx', account_id: 'account', user_account_id: 'logical', security_id: 'security', security_name: 'Fund', ticker_symbol: 'FUND', date: '2026-01-01', activity_type: 'investment', type: 'buy', subtype: 'buy', name: 'Fund purchase', amount: 11.25, currency_code: 'EUR', quantity: 1.25, price: 9, fees: 0 }
    expect(projectResponse('getInvestmentTransactions', { transactions: [{ ...transaction, user_id: 'caller', created_at: 'noise', plaid_security_id: 'provider', plaid_investment_transaction_id: 'provider' }], limit: 25, next_cursor: 'next' }).value).toEqual({ transactions: [transaction], limit: 25, next_cursor: 'next' })
  })

  test('notifications preserve full message, creation time and actionable relationships', () => {
    const expected = { id: 'notification', title: 'Review suggestion', message: 'Full detailed message, including the reason and amount.', created_at: 'today', read: false, type: 'transfer_suggestion', interaction_pattern: 'action', action_label: 'Review', action_url: '/suggestions/one', transfer_suggestion_id: 'one' }
    expect(projectResponse('getNotifications', [{ ...expected, sent_email: true, email_sent_at: 'noise', updated_at: 'noise', user_id: 'caller' }]).value).toEqual([expected])
  })

  test('transfer mutation outcomes preserve matching evidence and lifecycle dates', () => {
    const suggestion = { id: 'suggestion', amount: 10, actual_amount: 9, currency_code: 'CAD', from_account_id: 'from', from_user_account_id: 'logical-from', from_account_name: 'Checking', to_account_id: 'to', to_user_account_id: 'logical-to', to_account_name: 'Savings', reason: 'Surplus', user_notes: 'Keep this note', status: 'completed', match_state: 'completed', completion_source: 'matched_transaction', matched_transaction_id: 'tx', matched_transaction_at: 'today', completed_at: 'today', user_action_at: 'today', expires_at: 'tomorrow', created_at: 'yesterday', source: 'boost', boost_id: 'boost', boost_execution_id: 'execution', boost_name: 'Save surplus' }
    expect(projectResponse('confirmTransferSuggestionMatch', { message: 'Confirmed', suggestion: { ...suggestion, updated_at: 'noise', user_id: 'caller' } }).value).toEqual({ message: 'Confirmed', suggestion })
  })

  test('recipe specifications preserve every executable argument including future opaque data', () => {
    const expected = { id: 'recipe', name: 'Details', description: 'Read complete history', enabled: false, schedule: '', spec: [{ operationId: 'getTransactions', detail: 'full', parameters: { limit: 25 }, pathParams: { id: 'id' }, body: { future: { user_id: 'semantic', created_at: 'semantic' } } }] }
    expect(projectResponse('getMCPRecipe', { ...expected, user_id: 'caller', created_at: 'noise', updated_at: 'noise' }).value).toEqual(expected)
  })

  test('profile and privacy preserve user-visible state, consent and retention dates', () => {
    const user = { id: 'user', email: 'member@example.com', email_verified: false, primary_region: 'us-east-1', timezone: 'America/New_York', timezone_source: 'user', is_active: false, is_deleted: true, deletion_requested_at: 'today', deletion_scheduled_at: 'tomorrow', retention_started_at: 'today', retention_expires_at: 'next-month', subscription_tier: 'premium', subscription_status: 'past_due', effective_tier: 'premium', effective_status: 'past_due', had_trial: true, trial_days_remaining: 0, trial_started_at: 'past', trial_ends_at: 'past', subscription_started_at: 'past', subscription_ends_at: 'future', past_due_started_at: 'today', is_comped: false, comp_tier: '', comp_ends_at: null, manual_tier_override: '', manual_tier_ends_at: null }
    expect(projectResponse('getUserProfile', { ...user, clerk_user_id: 'internal', paddle_customer_id: 'internal', paddle_subscription_id: 'internal', retention_notice_30d_sent_at: 'noise', created_at: 'noise', updated_at: 'noise' }).value).toEqual(user)
    const privacy = { id: 'privacy', data_sharing_enabled: false, gdpr_analytics_consent: false, gdpr_consent_timestamp: 'today', gdpr_interaction_capture_consent: false, gdpr_necessary_consent: true, identified_diagnostics_enabled: true, include_in_aggregate_stats: false, marketing_emails_enabled: false, product_updates_enabled: true, security_alerts_enabled: true }
    expect(projectResponse('getPrivacySettings', { ...privacy, user_id: 'caller', created_at: 'noise', updated_at: 'noise' }).value).toEqual(privacy)
  })
})
