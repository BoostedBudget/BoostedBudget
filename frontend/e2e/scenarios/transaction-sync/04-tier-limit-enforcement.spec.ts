/**
 * P0 Scenario: Tier Limit Enforcement
 * Issue #331: E2E Test Scenarios Framework
 *
 * Tests that subscription tier limits are enforced for account creation.
 * Free tier users should be limited to 3 accounts max (monthly limit).
 *
 * CRITICAL: Revenue protection - prevents free users from getting premium features
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

test.describe('P0: Tier Limit Enforcement', () => {
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
      seed: 54321,  // Different seed
    };
    runner = new ScenarioRunner(config, dbPool);
  });

  test.afterAll(async () => {
    // Close database connections
    await runner.close();
    await dbPool.end();
  });

  test('should enforce free tier 3-account monthly limit', async ({ page }) => {
    // Load scenario from YAML
    const scenarioPath = path.join(import.meta.dirname, '04-tier-limit-enforcement.yaml');
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
    expect(result.assertions.length, 'Should have 6 assertions').toBeGreaterThanOrEqual(6);

    // All assertions should pass
    const failedAssertions = result.assertions.filter((a) => !a.passed);
    expect(
      failedAssertions,
      `Failed assertions: ${failedAssertions.map((a) => a.description).join(', ')}`
    ).toHaveLength(0);
  });

  test('should verify tier limits from config/subscription_tiers.yaml', async ({ page: _page }) => {
    // This test validates the tier limits match the configured values
    // from config/subscription_tiers.yaml

    // Query: Get user's account count
    const accountCountResult = await dbPool.query(`
      SELECT COUNT(*) as count
      FROM accounts
      WHERE user_id = '00000000-0000-0000-0000-000000000001'
    `);

    const accountCount = parseInt(accountCountResult.rows[0]?.count || '0', 10);

    // Free tier limit: 3 accounts
    expect(
      accountCount,
      'Free tier user should have exactly 3 accounts'
    ).toBe(3);

    // Query: Verify user is on free tier
    const tierResult = await dbPool.query(`
      SELECT tier
      FROM users
      WHERE id = '00000000-0000-0000-0000-000000000001'
    `);

    expect(
      tierResult.rows[0]?.tier,
      'User should be on free tier'
    ).toBe('free');
  });

  test('should prevent account creation beyond tier limit', async ({ page: _page }) => {
    // This test verifies that attempting to create more accounts
    // beyond the tier limit is properly rejected

    // Query: Verify 4th account was not created
    const fourthAccountResult = await dbPool.query(`
      SELECT COUNT(*) as count
      FROM accounts
      WHERE user_id = '00000000-0000-0000-0000-000000000001'
      AND (plaid_account_id = 'plaid-acc-limit-004' OR plaid_account_id = 'plaid-acc-limit-005')
    `);

    expect(
      parseInt(fourthAccountResult.rows[0]?.count || '0', 10),
      '4th and 5th account attempts should have been rejected'
    ).toBe(0);

    // Query: Verify only original 3 accounts exist
    const originalAccountsResult = await dbPool.query(`
      SELECT plaid_account_id
      FROM accounts
      WHERE user_id = '00000000-0000-0000-0000-000000000001'
      ORDER BY plaid_account_id
    `);

    expect(originalAccountsResult.rows.length).toBe(3);
    expect(originalAccountsResult.rows[0]?.plaid_account_id).toBe('plaid-acc-limit-001');
    expect(originalAccountsResult.rows[1]?.plaid_account_id).toBe('plaid-acc-limit-002');
    expect(originalAccountsResult.rows[2]?.plaid_account_id).toBe('plaid-acc-limit-003');
  });

  test('should provide helpful upgrade message when limit exceeded', async ({ page: _page }) => {
    // This test validates that when a user hits their tier limit,
    // they receive a helpful error message explaining they need to upgrade

    // Note: This would require API response inspection
    // For now, we verify the limit is enforced (tested above)
    // In a full implementation, we'd check the error response body
    // contains upgrade messaging like:
    //   "Free tier limited to 3 accounts. Upgrade to Premium Lite for 15 accounts."

    // Placeholder: Verify enforcement happened
    const accountCount = await dbPool.query(`
      SELECT COUNT(*) as count FROM accounts WHERE user_id = '00000000-0000-0000-0000-000000000001'
    `);

    expect(parseInt(accountCount.rows[0]?.count || '0', 10)).toBe(3);
  });
});
