/**
 * P0 Scenario: Preserve User Notes
 * Issue #331: E2E Test Scenarios Framework
 * Related: Issue #332 - Data Integrity Tests
 *
 * Tests that user notes on transactions are preserved during sync operations.
 * This is a CRITICAL data integrity test - losing user notes is a data loss bug.
 *
 * CRITICAL: Data loss prevention - user notes must never be overwritten or deleted
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

test.describe('P0: Preserve User Notes', () => {
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
      seed: 67890,  // Different seed from other scenarios
    };
    runner = new ScenarioRunner(config, dbPool);
  });

  test.afterAll(async () => {
    // Close database connections
    await runner.close();
    await dbPool.end();
  });

  test('should preserve user notes during transaction sync', async ({ page }) => {
    // Load scenario from YAML
    const scenarioPath = path.join(import.meta.dirname, '03-preserve-user-notes.yaml');
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

  test('should verify user notes are never null after sync', async ({ page: _page }) => {
    // This test validates that user notes are never accidentally set to null
    // during sync operations - a common data loss bug pattern

    // Query: Verify all transactions with notes still have non-null values
    const nullNotesResult = await dbPool.query(`
      SELECT COUNT(*) as count
      FROM transactions t
      JOIN accounts a ON t.account_id = a.id
      JOIN users u ON a.user_id = u.id
      WHERE u.email = 'preserve-notes@test.example.com'
      AND t.external_transaction_id IN ('plaid-txn-preserve-001', 'plaid-txn-preserve-002', 'plaid-txn-preserve-003')
      AND t.user_notes IS NULL
    `);

    expect(
      parseInt(nullNotesResult.rows[0]?.count || '0', 10),
      'No transactions should have null user_notes after sync'
    ).toBe(0);
  });

  test('should verify exact note content preservation', async ({ page: _page }) => {
    // This test verifies the exact content of user notes
    // to ensure no truncation, encoding issues, or whitespace corruption

    // Query: Get all three notes
    const notesResult = await dbPool.query(`
      SELECT external_transaction_id, user_notes
      FROM transactions t
      JOIN accounts a ON t.account_id = a.id
      JOIN users u ON a.user_id = u.id
      WHERE u.email = 'preserve-notes@test.example.com'
      AND t.external_transaction_id IN ('plaid-txn-preserve-001', 'plaid-txn-preserve-002', 'plaid-txn-preserve-003')
      ORDER BY external_transaction_id
    `);

    expect(notesResult.rows.length, 'Should have 3 transactions with notes').toBe(3);

    // Verify each note exactly
    const notes = notesResult.rows;

    // Note 1: Single-line note
    expect(notes[0]?.user_notes).toBe('Important note - do not lose this! Project XYZ expense.');

    // Note 2: Single-line note with special characters
    expect(notes[1]?.user_notes).toBe('Coffee with Sarah - discuss Q1 planning');

    // Note 3: Multi-line note (with newlines preserved)
    expect(notes[2]?.user_notes).toBe('Reimbursable expense\nReceipt in folder\nSubmit by 01/31');
  });

  test('should prevent accidental note overwriting', async ({ page: _page }) => {
    // This test simulates the specific bug pattern where sync operations
    // accidentally overwrite user notes with empty values or Plaid data

    // Query: Verify notes don't contain Plaid-provided data (which would indicate overwrite)
    const overwriteCheckResult = await dbPool.query(`
      SELECT external_transaction_id, user_notes
      FROM transactions t
      JOIN accounts a ON t.account_id = a.id
      JOIN users u ON a.user_id = u.id
      WHERE u.email = 'preserve-notes@test.example.com'
      AND t.user_notes LIKE '%Plaid%'
    `);

    expect(
      overwriteCheckResult.rows.length,
      'User notes should not contain Plaid-provided data (indicates accidental overwrite)'
    ).toBe(0);
  });
});
