/**
 * E2E Test Data Helpers
 *
 * Simplified test data generation for E2E tests.
 * Provides realistic test data without complex YAML configuration.
 */

export interface TestUser {
  id: string;
  email: string;
  subscription_tier: 'free' | 'premium_lite' | 'premium' | 'premium_plus';
}

export interface TestLinkedItem {
  id: string;
  user_id: string;
  item_id: string;
  institution_id: string;
  institution_name: string;
  status: string;
}

export interface TestAccount {
  id: string;
  linked_item_id: string;
  user_id: string;
  plaid_account_id: string;
  name: string;
  type: string;
  subtype?: string;
  current_balance?: number;
  available_balance?: number;
}

export interface TestTransaction {
  id: string;
  user_id: string;
  account_id: string;
  linked_item_id: string;
  source: string;
  external_transaction_id?: string;
  amount: number;
  date: string;
  name: string;
  pending: boolean;
  user_notes?: string;
  merchant_name?: string;
}

/**
 * Generate a test user with realistic data
 */
export function generateTestUser(tier: TestUser['subscription_tier'] = 'free'): TestUser {
  const timestamp = Date.now();
  return {
    id: `test-user-${timestamp}`,
    email: `test-${timestamp}@example.com`,
    subscription_tier: tier,
  };
}

/**
 * Generate a test linked item
 */
export function generateTestLinkedItem(userId: string): TestLinkedItem {
  const timestamp = Date.now();
  return {
    id: `test-item-${timestamp}`,
    user_id: userId,
    item_id: `plaid-item-${timestamp}`,
    institution_id: 'ins_3',
    institution_name: 'Chase',
    status: 'active',
  };
}

/**
 * Generate a test account
 */
export function generateTestAccount(
  userId: string,
  linkedItemId: string,
  type: 'depository' | 'credit' = 'depository'
): TestAccount {
  const timestamp = Date.now();
  const isCredit = type === 'credit';

  return {
    id: `test-account-${timestamp}`,
    linked_item_id: linkedItemId,
    user_id: userId,
    plaid_account_id: `plaid-acc-${timestamp}`,
    name: isCredit ? 'Test Credit Card' : 'Test Checking Account',
    type,
    subtype: isCredit ? 'credit_card' : 'checking',
    current_balance: isCredit ? -500 : 5000,
    available_balance: isCredit ? 9500 : 4800,
  };
}

/**
 * Generate a test transaction
 */
export function generateTestTransaction(
  userId: string,
  accountId: string,
  linkedItemId: string,
  overrides?: Partial<TestTransaction>
): TestTransaction {
  const timestamp = Date.now();
  const today = new Date().toISOString().split('T')[0];

  return {
    id: `test-txn-${timestamp}`,
    user_id: userId,
    account_id: accountId,
    linked_item_id: linkedItemId,
    source: 'plaid',
    external_transaction_id: `plaid-txn-${timestamp}`,
    amount: -50.00,
    date: today,
    name: 'Test Transaction',
    pending: false,
    ...overrides,
  };
}

/**
 * Generate a pending transaction
 */
export function generatePendingTransaction(
  userId: string,
  accountId: string,
  linkedItemId: string
): TestTransaction {
  return generateTestTransaction(userId, accountId, linkedItemId, {
    pending: true,
    name: 'Pending Test Transaction',
    amount: -45.00,
  });
}

/**
 * Generate a transaction with user notes
 */
export function generateTransactionWithNotes(
  userId: string,
  accountId: string,
  linkedItemId: string,
  notes: string
): TestTransaction {
  return generateTestTransaction(userId, accountId, linkedItemId, {
    user_notes: notes,
    name: 'Transaction with Notes',
  });
}

/**
 * Generate multiple test transactions
 */
export function generateTestTransactions(
  userId: string,
  accountId: string,
  linkedItemId: string,
  count: number
): TestTransaction[] {
  return Array.from({ length: count }, (_, i) =>
    generateTestTransaction(userId, accountId, linkedItemId, {
      name: `Test Transaction ${i + 1}`,
      amount: -(i + 1) * 10,
    })
  );
}
