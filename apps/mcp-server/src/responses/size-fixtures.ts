/** Deterministic synthetic API-shaped examples; never customer exports or padding. */
const stamp = '2026-08-01T12:00:00Z'
const owner = '00000000-0000-4000-8000-000000000002'
const metadata = { user_id: owner, created_at: stamp, updated_at: stamp }
const rows = (count: number, make: (index: number) => Record<string, unknown>) => Array.from({ length: count }, (_, i) => make(i))
const account = (i: number) => ({
  ...metadata, id: `account-${i}`, user_account_id: `user-account-${i}`, name: ['Everyday Checking', 'Emergency Savings', 'Travel Fund'][i % 3],
  institution_name: 'Synthetic Credit Union', type: 'depository', subtype: i % 2 ? 'savings' : 'checking',
  status: 'active', mask: String(1000 + i), current_balance: 1450.35 + i * 250, available_balance: 1400 + i * 250,
  currency_code: 'USD', plaid_account_id: `synthetic-provider-account-${i}`, linked_item_id: 'synthetic-linked-item',
  custom_color: '#2563eb', custom_icon: 'wallet', display_order: i, is_hidden: false,
})
const definition = { name: 'Weekly groceries', conditions: [{ field: 'merchant_name', operator: 'contains', value: 'Market' }], actions: [{ type: 'set_category', value: 'FOOD_AND_DRINK_GROCERIES' }], priority: 10, enabled: true }

export interface SizeFixture { operationId: string; payload: unknown; collection?: string; projected: boolean; pathParams?: Record<string, string> }

export const sizeFixtures: SizeFixture[] = [
  { operationId: 'getTransactions', projected: true, collection: 'transactions', payload: {
    $schema: 'https://example.com/schemas/GetTransactionsOutputBody.json', limit: 20, next_cursor: 'synthetic-page-two', total_count: 43, total_amount: 1487.65,
    transactions: rows(20, (i) => i === 19 ? {
      id: 'investment-transaction-19', account_id: 'investment-account', user_account_id: 'user-investment-account', user_id: owner,
      name: 'Synthetic Index Fund dividend', amount: -24.50, date: '2026-08-20', source: 'investment', data_source: 'plaid',
      activity_type: 'dividend', currency_code: 'USD', security_id: 'synthetic-security', security_name: 'Synthetic Index Fund',
      ticker_symbol: 'SYNTH', investment_fees: 0, investment_price: 24.50, investment_quantity: 1, investment_subtype: 'dividend',
    } : ({
      id: `transaction-${i}`, account_id: 'account-0', user_account_id: 'user-account-0', user_id: owner,
      name: ['Corner Market', 'City Transit', 'Neighborhood Cafe', 'Internet Service'][i % 4], amount: 8.75 + i * 2,
      date: `2026-08-${String(i + 1).padStart(2, '0')}`, pending: false, reviewed: i % 2 === 0, source: 'banking', data_source: 'plaid',
      category_override_kind: 'none', pfc_primary: 'FOOD_AND_DRINK', pfc_primary_display: 'Food and Drink',
      pfc_detailed: 'FOOD_AND_DRINK_GROCERIES', pfc_detailed_display: 'Groceries', pfc_confidence_level: 'VERY_HIGH',
      pfc_category_icon_url: 'https://example.com/icons/groceries.svg', external_transaction_id: `synthetic-provider-txn-${i}`,
      linked_item_id: 'synthetic-linked-item', account: account(0), tags: ['monthly-review'],
    })),
  } },
  { operationId: 'getAccounts', projected: true, collection: 'accounts', payload: { $schema: 'https://example.com/schemas/GetAccountsOutputBody.json', total_count: 6, accounts: rows(6, account) } },
  { operationId: 'getUserRules', projected: true, payload: rows(5, (i) => ({ ...metadata, id: `rule-${i}`, name: definition.name, conditions: definition.conditions, priority: 10, enabled: true, target_kind: 'pfc', target_pfc_primary: 'FOOD_AND_DRINK', match_count: i * 5, override_count: 0 })) },
  { operationId: 'getUserRuleExecutions', projected: true, collection: 'executions', payload: { $schema: 'https://example.com/schemas/RuleExecutionsResponse.json', limit: 10, offset: 0, total: 25, executions: rows(10, (i) => ({
    id: `rule-execution-${i}`, rule_id: 'rule-0', rule_name: 'Weekly groceries', transaction_id: `transaction-${i}`, action_type: 'categorize',
    result: { category: 'Groceries' }, executed_at: stamp, user_id: owner, definition_snapshot: definition,
  })) } },
  { operationId: 'getBoostExecutions', pathParams: { id: 'boost-0' }, projected: true, collection: 'executions', payload: { $schema: 'https://example.com/schemas/BoostExecutionsResponse.json', limit: 10, offset: 0, total: 18, executions: rows(10, (i) => ({
    id: `boost-execution-${i}`, boost_id: 'boost-0', boost_name: 'Weekly savings', status: 'completed', started_at: stamp, completed_at: stamp,
    execution_duration_ms: 32, retry_count: 0, max_retries: 3, output_data: { suggested_amount: 15 }, user_id: owner,
    definition_snapshot: { ...definition, actions: [{ type: 'transfer_suggestion', amount: 15 }] },
    input_data: { transaction_id: `transaction-${i}`, amount: 52.34 }, trigger_context: { source: 'schedule', interval: 'weekly' },
  })) } },
  { operationId: 'getBudgets', projected: true, payload: rows(3, (i) => ({
    ...metadata, id: `budget-${i}`, name: ['Essentials', 'Travel', 'Hobbies'][i], amount: 1200, currency_code: 'USD', period: 'monthly',
    start_date: '2026-08-01', end_date: '2026-08-31', is_active: true,
    categories: rows(3, (j) => ({ id: `category-${i}-${j}`, budget_id: `budget-${i}`, allocated_amount: 400, spent_amount: 120 + j * 20, category_kind: 'pfc', pfc_primary: 'FOOD_AND_DRINK', created_at: stamp, updated_at: stamp })),
  })) },
  { operationId: 'getSavingsGoals', projected: true, payload: rows(4, (i) => ({
    ...metadata, id: `goal-${i}`, name: ['Emergency Fund', 'Vacation', 'Laptop', 'Home Repairs'][i], target_amount: 5000,
    current_amount: 1500 + i * 200, currency_code: 'USD', is_completed: false, account_id: `account-${i}`, account: account(i),
  })) },
  { operationId: 'getNotifications', projected: true, payload: rows(8, (i) => ({
    ...metadata, id: `notification-${i}`, title: 'Weekly savings review', message: 'Your weekly savings suggestion is ready to review.',
    type: 'transfer_suggestion', read: i % 2 === 0, action_url: '/boosts', action_label: 'Review suggestion',
    sent_email: true, email_sent_at: stamp,
  })) },
  { operationId: 'getCashFlowSummary', projected: false, payload: { income: 5200, expenses: 3100.25, net_cash_flow: 2099.75, start_date: '2026-08-01', end_date: '2026-08-31', currency_code: 'USD' } },
]
