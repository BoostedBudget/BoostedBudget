/**
 * P2 Scenario: Removed Transactions
 * Issue #331: E2E Test Scenarios Framework
 *
 * Tests that TRANSACTIONS_REMOVED webhook correctly deletes transactions
 * that were retroactively removed by the financial institution.
 *
 * MEDIUM PRIORITY: Important edge case for data consistency
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

test.describe('P2: Removed Transactions', () => {
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
      path.join(import.meta.dirname, '..'),  // scenarios dir
      path.join(import.meta.dirname, '../../templates')  // templates dir
    );

    // Create scenario runner
    const config: RunnerConfig = {
      databaseUrl,
      apiBaseUrl,
      seed: 88888,  // Different seed
    };
    runner = new ScenarioRunner(config, dbPool);
  });

  test.afterAll(async () => {
    // Close database connections
    await runner.close();
    await dbPool.end();
  });

  test('should correctly handle TRANSACTIONS_REMOVED webhook', async ({ page }) => {
    // Load scenario from YAML
    const scenarioPath = path.join(import.meta.dirname, '07-removed-transactions.yaml');
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

    // Log error if validation or execution failed
    if (result.error) {
      console.error(`\n❌ Error: ${result.error}`);
    }

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

  test('should delete exactly 3 transactions', async ({ page: _page }) => {
    // This test validates the correct count of deletions

    // Query: Count total transactions
    const totalResult = await dbPool.query(`
      SELECT COUNT(*) as count
      FROM transactions t
      JOIN accounts a ON t.account_id = a.id
      JOIN users u ON a.user_id = u.id
      WHERE u.email = 'removed-txn@test.example.com'
    `);

    expect(
      parseInt(totalResult.rows[0]?.count || '0', 10),
      'Should have 2 transactions remaining (5 initial - 3 removed)'
    ).toBe(2);
  });

  test('should verify removed transactions no longer exist', async ({ page: _page }) => {
    // This test validates that specific removed transaction IDs are gone

    // Query: Check for removed transaction IDs
    const removedResult = await dbPool.query(`
      SELECT external_transaction_id
      FROM transactions t
      JOIN accounts a ON t.account_id = a.id
      JOIN users u ON a.user_id = u.id
      WHERE u.email = 'removed-txn@test.example.com'
      AND external_transaction_id IN (
        'plaid-txn-removed-001',
        'plaid-txn-removed-002',
        'plaid-txn-removed-003'
      )
    `);

    expect(
      removedResult.rows.length,
      'Removed transactions should not exist in database'
    ).toBe(0);
  });

  test('should verify remaining transactions are intact', async ({ page: _page }) => {
    // This test validates that non-removed transactions are still present

    // Query: Check for kept transaction IDs
    const keptResult = await dbPool.query(`
      SELECT
        external_transaction_id,
        amount,
        merchant_name,
        user_notes
      FROM transactions t
      JOIN accounts a ON t.account_id = a.id
      JOIN users u ON a.user_id = u.id
      WHERE u.email = 'removed-txn@test.example.com'
      ORDER BY external_transaction_id
    `);

    expect(keptResult.rows.length).toBe(2);

    // Verify grocery transaction
    const groceryTxn = keptResult.rows.find(
      (row) => row.external_transaction_id === 'plaid-txn-keep-001'
    );
    expect(groceryTxn).toBeTruthy();
    expect(parseFloat(groceryTxn?.amount || '0')).toBe(-50.00);
    expect(groceryTxn?.merchant_name).toBe('Whole Foods');
    expect(groceryTxn?.user_notes).toBe('Weekly groceries - important note to preserve');

    // Verify paycheck transaction
    const paycheckTxn = keptResult.rows.find(
      (row) => row.external_transaction_id === 'plaid-txn-keep-002'
    );
    expect(paycheckTxn).toBeTruthy();
    expect(parseFloat(paycheckTxn?.amount || '0')).toBe(3000.00);
    expect(paycheckTxn?.merchant_name).toBe('Employer Inc');
  });

  test('should preserve user notes on non-removed transactions', async ({ page: _page }) => {
    // This test validates critical data preservation during deletion

    // Query: Check user notes on remaining transactions
    const notesResult = await dbPool.query(`
      SELECT
        external_transaction_id,
        user_notes
      FROM transactions t
      JOIN accounts a ON t.account_id = a.id
      JOIN users u ON a.user_id = u.id
      WHERE u.email = 'removed-txn@test.example.com'
      AND user_notes IS NOT NULL
      ORDER BY external_transaction_id
    `);

    expect(notesResult.rows.length).toBeGreaterThanOrEqual(2);

    // Verify notes are preserved
    const groceryNotes = notesResult.rows.find(
      (row) => row.external_transaction_id === 'plaid-txn-keep-001'
    );
    expect(groceryNotes?.user_notes).toBe('Weekly groceries - important note to preserve');

    const paycheckNotes = notesResult.rows.find(
      (row) => row.external_transaction_id === 'plaid-txn-keep-002'
    );
    expect(paycheckNotes?.user_notes).toBe('Biweekly paycheck');
  });

  test('should handle idempotent removal webhook', async ({ page: _page }) => {
    // This test validates that firing the same removal webhook multiple times
    // doesn't cause errors (idempotency)

    // Fire the webhook again with same transaction IDs
    const response = await fetch(`${apiBaseUrl}/api/v1/plaid/webhook`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        webhook_type: 'TRANSACTIONS',
        webhook_code: 'TRANSACTIONS_REMOVED',
        item_id: 'plaid-sandbox-removed-001',
        removed_transactions: [
          'plaid-txn-removed-001',
          'plaid-txn-removed-002',
          'plaid-txn-removed-003',
        ],
      }),
    });

    // Should still return 200 (idempotent)
    expect(response.status).toBe(200);

    // Query: Verify still 2 transactions (no additional deletions)
    const countResult = await dbPool.query(`
      SELECT COUNT(*) as count
      FROM transactions t
      JOIN accounts a ON t.account_id = a.id
      JOIN users u ON a.user_id = u.id
      WHERE u.email = 'removed-txn@test.example.com'
    `);

    expect(parseInt(countResult.rows[0]?.count || '0', 10)).toBe(2);
  });

  test('should verify no orphaned data after removal', async ({ page: _page }) => {
    // This test validates that no related data is left orphaned

    // Query: Check for any orphaned references (if budgets/rules existed)
    const orphanCheck = await dbPool.query(`
      SELECT COUNT(*) as count
      FROM transactions t
      WHERE NOT EXISTS (
        SELECT 1 FROM accounts a WHERE a.id = t.account_id
      )
    `);

    expect(
      parseInt(orphanCheck.rows[0]?.count || '0', 10),
      'Should have no orphaned transactions'
    ).toBe(0);
  });
});
