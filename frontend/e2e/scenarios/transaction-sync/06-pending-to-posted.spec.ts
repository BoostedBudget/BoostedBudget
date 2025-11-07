/**
 * P2 Scenario: Pending to Posted Transactions
 * Issue #331: E2E Test Scenarios Framework
 *
 * Tests that pending transactions correctly transition to posted status
 * with proper handling of amount/date changes and user note preservation.
 *
 * MEDIUM PRIORITY: Important for accurate transaction tracking
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

test.describe('P2: Pending to Posted Transactions', () => {
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
      seed: 66666,  // Different seed
    };
    runner = new ScenarioRunner(config, dbPool);
  });

  test.afterAll(async () => {
    // Close database connections
    await runner.close();
    await dbPool.end();
  });

  test('should transition pending transactions to posted correctly', async ({ page }) => {
    // Load scenario from YAML
    const scenarioPath = path.join(__dirname, '06-pending-to-posted.yaml');
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
    expect(result.assertions.length, 'Should have 10 assertions').toBeGreaterThanOrEqual(10);

    // All assertions should pass
    const failedAssertions = result.assertions.filter((a) => !a.passed);
    expect(
      failedAssertions,
      `Failed assertions: ${failedAssertions.map((a) => a.description).join(', ')}`
    ).toHaveLength(0);
  });

  test('should not create duplicate transactions during state transition', async ({ page: _page }) => {
    // This test validates that posting a pending transaction updates the existing
    // record rather than creating a duplicate

    // Query: Count transactions
    const countResult = await dbPool.query(`
      SELECT COUNT(*) as count
      FROM transactions t
      JOIN accounts a ON t.account_id = a.id
      JOIN users u ON a.user_id = u.id
      WHERE u.email = 'pending@test.example.com'
    `);

    expect(
      parseInt(countResult.rows[0]?.count || '0', 10),
      'Should have exactly 3 transactions (no duplicates created)'
    ).toBe(3);
  });

  test('should update all pending flags to false', async ({ page: _page }) => {
    // This test validates that all transactions transitioned to posted status

    // Query: Count transactions by pending status
    const statusResult = await dbPool.query(`
      SELECT
        COUNT(*) FILTER (WHERE pending = true) as pending_count,
        COUNT(*) FILTER (WHERE pending = false) as posted_count
      FROM transactions t
      JOIN accounts a ON t.account_id = a.id
      JOIN users u ON a.user_id = u.id
      WHERE u.email = 'pending@test.example.com'
    `);

    expect(
      parseInt(statusResult.rows[0]?.pending_count || '0', 10),
      'Should have 0 pending transactions after posting'
    ).toBe(0);

    expect(
      parseInt(statusResult.rows[0]?.posted_count || '0', 10),
      'Should have 3 posted transactions'
    ).toBe(3);
  });

  test('should correctly update restaurant charge with tip', async ({ page: _page }) => {
    // This test validates that amount changes (like tips) are properly handled

    // Query: Get restaurant transaction
    const restaurantResult = await dbPool.query(`
      SELECT amount, pending, user_notes
      FROM transactions t
      JOIN accounts a ON t.account_id = a.id
      JOIN users u ON a.user_id = u.id
      WHERE u.email = 'pending@test.example.com'
      AND t.external_transaction_id = 'plaid-txn-pending-002'
    `);

    expect(restaurantResult.rows.length).toBe(1);

    const txn = restaurantResult.rows[0];
    expect(parseFloat(txn?.amount || '0')).toBe(-54.50);  // Authorization -$45.00 + $9.50 tip
    expect(txn?.pending).toBe(false);
    expect(txn?.user_notes).toBe('Team lunch - add tip later');
  });

  test('should update hotel checkout date correctly', async ({ page: _page }) => {
    // This test validates that date changes (like checkout date) are handled

    // Query: Get hotel transaction
    const hotelResult = await dbPool.query(`
      SELECT date, pending
      FROM transactions t
      JOIN accounts a ON t.account_id = a.id
      JOIN users u ON a.user_id = u.id
      WHERE u.email = 'pending@test.example.com'
      AND t.external_transaction_id = 'plaid-txn-pending-003'
    `);

    expect(hotelResult.rows.length).toBe(1);

    const txn = hotelResult.rows[0];
    const txnDate = new Date(txn?.date).toISOString().split('T')[0];
    expect(txnDate).toBe('2024-01-16');  // Changed from 01-13 to 01-16 (checkout)
    expect(txn?.pending).toBe(false);
  });

  test('should preserve user notes during state transition', async ({ page: _page }) => {
    // This test validates critical data preservation during pending→posted transition

    // Query: Get all user notes
    const notesResult = await dbPool.query(`
      SELECT external_transaction_id, user_notes
      FROM transactions t
      JOIN accounts a ON t.account_id = a.id
      JOIN users u ON a.user_id = u.id
      WHERE u.email = 'pending@test.example.com'
      AND user_notes IS NOT NULL
      ORDER BY external_transaction_id
    `);

    // Should have at least 2 transactions with notes
    expect(notesResult.rows.length).toBeGreaterThanOrEqual(2);

    // Verify gas purchase note preserved
    const gasNotes = notesResult.rows.find(
      (row) => row.external_transaction_id === 'plaid-txn-pending-001'
    );
    expect(gasNotes?.user_notes).toBe('Fill up - work trip');

    // Verify restaurant note preserved
    const restaurantNotes = notesResult.rows.find(
      (row) => row.external_transaction_id === 'plaid-txn-pending-002'
    );
    expect(restaurantNotes?.user_notes).toBe('Team lunch - add tip later');
  });

  test('should handle gas purchase with unchanged amount', async ({ page: _page }) => {
    // This test validates transactions that post with the same amount as authorization

    // Query: Get gas transaction
    const gasResult = await dbPool.query(`
      SELECT amount, pending
      FROM transactions t
      JOIN accounts a ON t.account_id = a.id
      JOIN users u ON a.user_id = u.id
      WHERE u.email = 'pending@test.example.com'
      AND t.external_transaction_id = 'plaid-txn-pending-001'
    `);

    expect(gasResult.rows.length).toBe(1);

    const txn = gasResult.rows[0];
    expect(parseFloat(txn?.amount || '0')).toBe(-55.00);  // Unchanged from pending
    expect(txn?.pending).toBe(false);
  });

  test('should verify all transactions have complete data after posting', async ({ page: _page }) => {
    // This test validates data integrity after state transition

    // Query: Check for any incomplete data
    const integrityResult = await dbPool.query(`
      SELECT COUNT(*) as count
      FROM transactions t
      JOIN accounts a ON t.account_id = a.id
      JOIN users u ON a.user_id = u.id
      WHERE u.email = 'pending@test.example.com'
      AND (
        t.external_transaction_id IS NULL
        OR t.amount IS NULL
        OR t.date IS NULL
        OR t.name IS NULL
        OR t.pending IS NULL
      )
    `);

    expect(
      parseInt(integrityResult.rows[0]?.count || '0', 10),
      'All transactions should have complete data'
    ).toBe(0);
  });
});
