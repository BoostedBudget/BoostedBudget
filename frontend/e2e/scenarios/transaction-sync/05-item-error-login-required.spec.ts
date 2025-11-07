/**
 * P1 Scenario: Item Error - Login Required
 * Issue #331: E2E Test Scenarios Framework
 *
 * Tests that the system handles Plaid item LOGIN_REQUIRED errors gracefully,
 * preserving data integrity while prompting users to reconnect.
 *
 * HIGH PRIORITY: Ensures graceful error handling without data loss
 */

import { test, expect } from '@playwright/test';
import { Pool } from 'pg';
import { ScenarioRunner } from '../../lib/scenario-runner';
import { ScenarioLoader } from '../../lib/scenario-loader';
import type { RunnerConfig } from '../../lib/scenario-runner';
import path from 'path';

// Get worker-specific database URL
const workerIndex = parseInt(process.env.TEST_PARALLEL_INDEX || '0', 10);
const databaseUrl = process.env[`TEST_DATABASE_URL_${workerIndex}`] || process.env.TEST_DATABASE_URL || '';
const apiBaseUrl = process.env.API_BASE_URL || 'http://localhost:8081';

test.describe('P1: Item Error - Login Required', () => {
  let dbPool: Pool;
  let runner: ScenarioRunner;
  let loader: ScenarioLoader;

  test.beforeAll(async () => {
    // Create database pool for this worker
    dbPool = new Pool({
      connectionString: databaseUrl,
      max: 10,
    });

    // Create scenario loader
    loader = new ScenarioLoader(
      path.join(__dirname, '..'),  // scenarios dir
      path.join(__dirname, '../../templates')  // templates dir
    );

    // Create scenario runner
    const config: RunnerConfig = {
      databaseUrl,
      apiBaseUrl,
      seed: 77777,  // Different seed
    };
    runner = new ScenarioRunner(config, dbPool);
  });

  test.afterAll(async () => {
    // Close database connections
    await runner.close();
    await dbPool.end();
  });

  test('should handle ITEM_LOGIN_REQUIRED error gracefully', async ({ page }) => {
    // Load scenario from YAML
    const scenarioPath = path.join(__dirname, '05-item-error-login-required.yaml');
    const scenario = await loader.load(scenarioPath);

    // Run scenario
    const result = await runner.run(scenario, page);

    // Log result for debugging
    console.log(`\nScenario: ${result.scenario}`);
    console.log(`Duration: ${result.duration}ms`);
    console.log(`  Setup: ${result.setupDuration}ms`);
    console.log(`  Steps: ${result.stepsDuration}ms`);
    console.log(`  Assertions: ${result.assertionsDuration}ms`);
    console.log(`  Cleanup: ${result.cleanupDuration}ms`);

    // Log failed assertions
    if (!result.success) {
      console.error('\n❌ Failed Assertions:');
      for (const assertion of result.assertions) {
        if (!assertion.passed) {
          console.error(`  - ${assertion.description}`);
          console.error(`    Expected: ${JSON.stringify(assertion.expected)}`);
          console.error(`    Actual: ${JSON.stringify(assertion.actual)}`);
          if (assertion.error) {
            console.error(`    Error: ${assertion.error}`);
          }
        }
      }
    } else {
      console.log('\n✅ All Assertions Passed:');
      for (const assertion of result.assertions) {
        console.log(`  ✓ ${assertion.description}`);
      }
    }

    // Assert scenario success
    expect(result.success, 'Scenario should pass all assertions').toBe(true);

    // Verify specific assertions
    expect(result.assertions.length, 'Should have 8 assertions').toBeGreaterThanOrEqual(8);

    // All assertions should pass
    const failedAssertions = result.assertions.filter((a) => !a.passed);
    expect(
      failedAssertions,
      `Failed assertions: ${failedAssertions.map((a) => a.description).join(', ')}`
    ).toHaveLength(0);
  });

  test('should preserve existing transactions during error state', async ({ page }) => {
    // This test validates that no data loss occurs when item enters error state

    // Query: Count transactions that existed before error
    const countResult = await dbPool.query(`
      SELECT COUNT(*) as count
      FROM transactions t
      JOIN accounts a ON t.account_id = a.id
      JOIN linked_items li ON a.linked_item_id = li.id
      JOIN users u ON li.user_id = u.id
      WHERE u.email = 'item-error@test.example.com'
    `);

    expect(
      parseInt(countResult.rows[0]?.count || '0', 10),
      'Should have all original transactions preserved'
    ).toBe(2);

    // Query: Verify user notes were not lost
    const notesResult = await dbPool.query(`
      SELECT user_notes
      FROM transactions t
      JOIN accounts a ON t.account_id = a.id
      JOIN users u ON a.user_id = u.id
      WHERE u.email = 'item-error@test.example.com'
      AND t.external_transaction_id = 'plaid-txn-error-001'
    `);

    expect(
      notesResult.rows[0]?.user_notes,
      'User notes should be preserved during error state'
    ).toBe('Weekly groceries - important note');
  });

  test('should set correct error metadata on linked item', async ({ page }) => {
    // This test validates that error metadata is properly recorded for debugging

    // Query: Get error metadata
    const errorResult = await dbPool.query(`
      SELECT status, error_code, error_timestamp
      FROM linked_items li
      JOIN users u ON li.user_id = u.id
      WHERE u.email = 'item-error@test.example.com'
    `);

    expect(
      errorResult.rows[0]?.status,
      'Status should be set to login_required'
    ).toBe('login_required');

    expect(
      errorResult.rows[0]?.error_code,
      'Error code should be ITEM_LOGIN_REQUIRED'
    ).toBe('ITEM_LOGIN_REQUIRED');

    expect(
      errorResult.rows[0]?.error_timestamp,
      'Error timestamp should be set for tracking'
    ).toBeTruthy();
  });

  test('should not sync new transactions while in error state', async ({ page }) => {
    // This test validates that transaction syncing is paused during error state
    // to prevent partial/inconsistent data

    // Query: Count transactions created after error timestamp
    const newTransactionsResult = await dbPool.query(`
      SELECT COUNT(*) as count
      FROM transactions t
      JOIN accounts a ON t.account_id = a.id
      JOIN linked_items li ON a.linked_item_id = li.id
      JOIN users u ON li.user_id = u.id
      WHERE u.email = 'item-error@test.example.com'
      AND t.created_at > li.error_timestamp
    `);

    expect(
      parseInt(newTransactionsResult.rows[0]?.count || '0', 10),
      'Should have zero new transactions while in error state'
    ).toBe(0);
  });

  test('should preserve cursor value during error state', async ({ page }) => {
    // This test validates that the cursor is not reset when item enters error state
    // This is critical for resuming sync from correct position after reconnection

    // Query: Get cursor value
    const cursorResult = await dbPool.query(`
      SELECT cursor
      FROM linked_items li
      JOIN users u ON li.user_id = u.id
      WHERE u.email = 'item-error@test.example.com'
    `);

    expect(
      cursorResult.rows[0]?.cursor,
      'Cursor should be preserved (not reset) during error state'
    ).toBe('cursor-before-error-123');

    expect(
      cursorResult.rows[0]?.cursor,
      'Cursor should not be null'
    ).not.toBeNull();
  });

  test('should allow fetching existing transactions during error state', async ({ page }) => {
    // This test validates that users can still view their existing transactions
    // even when the linked item is in error state

    // Query: Verify transactions are still accessible
    const transactionsResult = await dbPool.query(`
      SELECT
        t.id,
        t.external_transaction_id,
        t.amount,
        t.name
      FROM transactions t
      JOIN accounts a ON t.account_id = a.id
      JOIN users u ON a.user_id = u.id
      WHERE u.email = 'item-error@test.example.com'
      ORDER BY t.date DESC
    `);

    expect(transactionsResult.rows.length).toBe(2);
    expect(transactionsResult.rows[0]?.external_transaction_id).toBeTruthy();
    expect(transactionsResult.rows[0]?.amount).toBeTruthy();
  });
});
