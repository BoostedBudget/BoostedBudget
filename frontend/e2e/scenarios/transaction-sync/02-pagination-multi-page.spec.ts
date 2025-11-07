/**
 * P1 Scenario: Pagination Multi-Page
 * Issue #331: E2E Test Scenarios Framework
 *
 * Tests that transaction sync handles pagination correctly when Plaid returns
 * more than 500 transactions (typical page size).
 *
 * HIGH PRIORITY: Ensures large transaction volumes are handled correctly
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

test.describe('P1: Pagination Multi-Page', () => {
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
      seed: 99999,  // Different seed
    };
    runner = new ScenarioRunner(config, dbPool);
  });

  test.afterAll(async () => {
    // Close database connections
    await runner.close();
    await dbPool.end();
  });

  test('should handle >500 transactions across multiple pages', async ({ page }) => {
    // Load scenario from YAML
    const scenarioPath = path.join(import.meta.dirname, '02-pagination-multi-page.yaml');
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
    expect(result.assertions.length, 'Should have 7 assertions').toBeGreaterThanOrEqual(7);

    // All assertions should pass
    const failedAssertions = result.assertions.filter((a) => !a.passed);
    expect(
      failedAssertions,
      `Failed assertions: ${failedAssertions.map((a) => a.description).join(', ')}`
    ).toHaveLength(0);
  });

  test('should verify all 600 transactions were synced', async ({ page: _page }) => {
    // This test validates that no transactions were lost during pagination

    // Query: Get total transaction count
    const countResult = await dbPool.query(`
      SELECT COUNT(*) as count
      FROM transactions t
      JOIN accounts a ON t.account_id = a.id
      JOIN users u ON a.user_id = u.id
      WHERE u.email = 'pagination@test.example.com'
    `);

    expect(
      parseInt(countResult.rows[0]?.count || '0', 10),
      'Should have all 600 transactions (no pagination data loss)'
    ).toBe(600);
  });

  test('should verify no duplicate transactions across pages', async ({ page: _page }) => {
    // This test validates the common pagination bug where transactions
    // appear multiple times due to cursor handling issues

    // Query: Find duplicate external transaction IDs
    const duplicatesResult = await dbPool.query(`
      SELECT external_transaction_id, COUNT(*) as duplicate_count
      FROM transactions t
      JOIN accounts a ON t.account_id = a.id
      JOIN users u ON a.user_id = u.id
      WHERE u.email = 'pagination@test.example.com'
      GROUP BY external_transaction_id
      HAVING COUNT(*) > 1
    `);

    expect(
      duplicatesResult.rows.length,
      'Should have zero duplicate transactions across pagination'
    ).toBe(0);
  });

  test('should verify cursor was updated after pagination', async ({ page: _page }) => {
    // This test validates that the pagination cursor was properly updated
    // after fetching all pages

    // Query: Get cursor value
    const cursorResult = await dbPool.query(`
      SELECT cursor
      FROM linked_items li
      JOIN users u ON li.user_id = u.id
      WHERE u.email = 'pagination@test.example.com'
    `);

    expect(
      cursorResult.rows[0]?.cursor,
      'Cursor should be set after pagination completes'
    ).toBeTruthy();

    expect(
      cursorResult.rows[0]?.cursor,
      'Cursor should not be null'
    ).not.toBeNull();
  });

  test('should verify transaction uniqueness', async ({ page: _page }) => {
    // This test validates that all external transaction IDs are unique
    // (no accidental duplicates during multi-page sync)

    // Query: Count total transactions vs unique transactions
    const uniquenessResult = await dbPool.query(`
      SELECT
        COUNT(*) as total_count,
        COUNT(DISTINCT external_transaction_id) as unique_count
      FROM transactions t
      JOIN accounts a ON t.account_id = a.id
      JOIN users u ON a.user_id = u.id
      WHERE u.email = 'pagination@test.example.com'
    `);

    const totalCount = parseInt(uniquenessResult.rows[0]?.total_count || '0', 10);
    const uniqueCount = parseInt(uniquenessResult.rows[0]?.unique_count || '0', 10);

    expect(
      totalCount,
      'Total count should equal unique count (no duplicates)'
    ).toBe(uniqueCount);

    expect(totalCount).toBe(600);
    expect(uniqueCount).toBe(600);
  });
});
