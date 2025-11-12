/**
 * E2E Webhook Helpers
 *
 * Helpers for triggering webhooks during E2E tests.
 * Uses the admin webhook endpoint for test scenarios.
 */

import { APIRequestContext } from '@playwright/test';

export interface FireWebhookParams {
  webhook_type: 'TRANSACTIONS';
  webhook_code: 'INITIAL_UPDATE' | 'DEFAULT_UPDATE' | 'HISTORICAL_UPDATE' | 'TRANSACTIONS_REMOVED';
  item_id: string;
  removed_transactions?: string[];
}

/**
 * Fire a webhook for testing
 *
 * NOTE: Requires admin endpoint to be available (staging/test environments only)
 */
export async function fireWebhook(
  request: APIRequestContext,
  params: FireWebhookParams,
  authToken?: string
): Promise<void> {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
  };

  if (authToken) {
    headers['Authorization'] = `Bearer ${authToken}`;
  }

  const response = await request.post('/v1/admin/plaid/fire-webhook', {
    headers,
    data: params,
  });

  if (!response.ok()) {
    const body = await response.text();
    throw new Error(`Failed to fire webhook: ${response.status()} - ${body}`);
  }
}

/**
 * Fire DEFAULT_UPDATE webhook (incremental sync)
 */
export async function fireDefaultUpdate(
  request: APIRequestContext,
  itemId: string,
  authToken?: string
): Promise<void> {
  await fireWebhook(
    request,
    {
      webhook_type: 'TRANSACTIONS',
      webhook_code: 'DEFAULT_UPDATE',
      item_id: itemId,
    },
    authToken
  );
}

/**
 * Fire TRANSACTIONS_REMOVED webhook
 */
export async function fireTransactionsRemoved(
  request: APIRequestContext,
  itemId: string,
  removedTransactionIds: string[],
  authToken?: string
): Promise<void> {
  await fireWebhook(
    request,
    {
      webhook_type: 'TRANSACTIONS',
      webhook_code: 'TRANSACTIONS_REMOVED',
      item_id: itemId,
      removed_transactions: removedTransactionIds,
    },
    authToken
  );
}

/**
 * Wait for webhook processing to complete
 *
 * Simple delay-based wait. In production, you might poll a status endpoint.
 */
export async function waitForWebhookProcessing(delayMs: number = 3000): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, delayMs));
}
