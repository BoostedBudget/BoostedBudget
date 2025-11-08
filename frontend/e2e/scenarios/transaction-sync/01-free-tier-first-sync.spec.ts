/**
 * P0 Scenario: Free Tier First Sync
 * Issue #331: E2E Test Scenarios Framework
 *
 * Tests that free tier users receive exactly 30 days of transaction history
 * on their first sync (INITIAL_UPDATE webhook).
 *
 * CRITICAL: Revenue protection - ensures free users don't get premium features
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

test.describe('P0: Free Tier First Sync', () => {
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
      seed: 12345,  // Deterministic seed for reproducibility
    };
    runner = new ScenarioRunner(config, dbPool);
  });

  test.afterAll(async () => {
    // Close database connections
    await runner.close();
    await dbPool.end();
  });

  test('should limit free tier users to 30 days of transaction history', async ({ page }) => {
    // Load scenario from YAML
    const scenarioPath = path.join(import.meta.dirname, '01-free-tier-first-sync.yaml');
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
    expect(result.assertions.length, 'Should have 5 assertions').toBeGreaterThanOrEqual(5);

    // All assertions should pass
    const failedAssertions = result.assertions.filter((a) => !a.passed);
    expect(
      failedAssertions,
      `Failed assertions: ${failedAssertions.map((a) => a.description).join(', ')}`
    ).toHaveLength(0);
  });

  test('should validate free tier limits are enforced', async ({ page: _page }) => {
    // This test validates the specific business logic of free tier limits
    // by querying the database directly after the scenario runs

    // Query: Verify no transactions older than 30 days exist
    const oldTransactionsResult = await dbPool.query(`
      SELECT COUNT(*) as count
      FROM transactions t
      JOIN accounts a ON t.account_id = a.id
      JOIN users u ON a.user_id = u.id
      WHERE u.email = 'free-tier-sync@test.example.com'
      AND t.date < CURRENT_DATE - INTERVAL '30 days'
    `);

    expect(
      parseInt(oldTransactionsResult.rows[0]?.count || '0', 10),
      'Free tier should have zero transactions older than 30 days'
    ).toBe(0);

    // Query: Verify exactly 30 days of transactions exist
    const recentTransactionsResult = await dbPool.query(`
      SELECT COUNT(*) as count
      FROM transactions t
      JOIN accounts a ON t.account_id = a.id
      JOIN users u ON a.user_id = u.id
      WHERE u.email = 'free-tier-sync@test.example.com'
      AND t.date >= CURRENT_DATE - INTERVAL '30 days'
    `);

    const recentCount = parseInt(recentTransactionsResult.rows[0]?.count || '0', 10);
    expect(
      recentCount,
      'Free tier should have transactions from last 30 days'
    ).toBe(30);
  });
});
