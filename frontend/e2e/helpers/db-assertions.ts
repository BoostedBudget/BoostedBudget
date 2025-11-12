/**
 * E2E Database Assertion Helpers
 *
 * Helpers for asserting database state during E2E tests.
 * Uses API endpoints to query data rather than direct DB access.
 */

import { APIRequestContext, expect } from '@playwright/test';

/**
 * Assert transaction count for an account
 */
export async function assertTransactionCount(
  request: APIRequestContext,
  accountId: string,
  expectedCount: number,
  authToken?: string
): Promise<void> {
  const headers: Record<string, string> = {};
  if (authToken) {
    headers['Authorization'] = `Bearer ${authToken}`;
  }

  const response = await request.get(`/v1/transactions?account_id=${accountId}`, { headers });
  expect(response.ok()).toBeTruthy();

  const data = await response.json();
  expect(data.transactions).toHaveLength(expectedCount);
}

/**
 * Assert transaction exists with specific fields
 */
export async function assertTransactionExists(
  request: APIRequestContext,
  accountId: string,
  externalTransactionId: string,
  expectedFields: Partial<{
    amount: number;
    pending: boolean;
    user_notes: string;
    name: string;
  }>,
  authToken?: string
): Promise<void> {
  const headers: Record<string, string> = {};
  if (authToken) {
    headers['Authorization'] = `Bearer ${authToken}`;
  }

  const response = await request.get(`/v1/transactions?account_id=${accountId}`, { headers });
  expect(response.ok()).toBeTruthy();

  const data = await response.json();
  const transaction = data.transactions.find(
    (t: any) => t.external_transaction_id === externalTransactionId
  );

  expect(transaction).toBeDefined();

  if (expectedFields.amount !== undefined) {
    expect(transaction.amount).toBe(expectedFields.amount);
  }
  if (expectedFields.pending !== undefined) {
    expect(transaction.pending).toBe(expectedFields.pending);
  }
  if (expectedFields.user_notes !== undefined) {
    expect(transaction.user_notes).toBe(expectedFields.user_notes);
  }
  if (expectedFields.name !== undefined) {
    expect(transaction.name).toBe(expectedFields.name);
  }
}

/**
 * Assert transaction does not exist
 */
export async function assertTransactionNotExists(
  request: APIRequestContext,
  accountId: string,
  externalTransactionId: string,
  authToken?: string
): Promise<void> {
  const headers: Record<string, string> = {};
  if (authToken) {
    headers['Authorization'] = `Bearer ${authToken}`;
  }

  const response = await request.get(`/v1/transactions?account_id=${accountId}`, { headers });
  expect(response.ok()).toBeTruthy();

  const data = await response.json();
  const transaction = data.transactions.find(
    (t: any) => t.external_transaction_id === externalTransactionId
  );

  expect(transaction).toBeUndefined();
}

/**
 * Assert user notes are preserved on a transaction
 */
export async function assertUserNotesPreserved(
  request: APIRequestContext,
  accountId: string,
  externalTransactionId: string,
  expectedNotes: string,
  authToken?: string
): Promise<void> {
  await assertTransactionExists(
    request,
    accountId,
    externalTransactionId,
    { user_notes: expectedNotes },
    authToken
  );
}

/**
 * Assert account count for a user
 */
export async function assertAccountCount(
  request: APIRequestContext,
  expectedCount: number,
  authToken?: string
): Promise<void> {
  const headers: Record<string, string> = {};
  if (authToken) {
    headers['Authorization'] = `Bearer ${authToken}`;
  }

  const response = await request.get('/v1/accounts', { headers });
  expect(response.ok()).toBeTruthy();

  const data = await response.json();
  expect(data.accounts).toHaveLength(expectedCount);
}

/**
 * Assert tier limit exceeded error response
 */
export async function assertTierLimitExceeded(
  request: APIRequestContext,
  publicToken: string,
  institutionId: string,
  institutionName: string,
  authToken?: string
): Promise<void> {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
  };
  if (authToken) {
    headers['Authorization'] = `Bearer ${authToken}`;
  }

  const response = await request.post('/v1/plaid/exchange-token', {
    headers,
    data: {
      public_token: publicToken,
      institution_id: institutionId,
      institution_name: institutionName,
    },
  });

  expect(response.status()).toBe(403);
  const body = await response.json();
  expect(body.error).toContain('limit');
}
