/**
 * Cleanup Manager
 *
 * Handles database cleanup between test scenarios
 * Uses TRUNCATE CASCADE for speed (10x faster than DELETE)
 * Respects worker-specific database isolation
 *
 * Issue #331: E2E Test Scenarios Framework
 */

import type { Pool } from 'pg';
import type { CleanupConfig } from './types';

/**
 * Cleanup strategies
 */
export enum CleanupStrategy {
  DELETE_TEST_USERS = 'delete_test_users', // Delete only test users and cascade
  TRUNCATE_ALL = 'truncate_all', // Truncate all tables (fastest)
  NONE = 'none', // No cleanup (for debugging)
}

/**
 * Cleanup result
 */
export interface CleanupResult {
  success: boolean;
  strategy: CleanupStrategy;
  tablesAffected?: string[];
  error?: string;
  duration?: number; // milliseconds
}

/**
 * Cleanup manager class
 * Provides fast, safe database cleanup between scenarios
 */
export class CleanupManager {
  private pool: Pool;

  /**
   * Create cleanup manager
   * @param pool Database connection pool
   */
  constructor(pool: Pool) {
    this.pool = pool;
  }

  /**
   * Clean up database based on strategy
   */
  async cleanup(config?: CleanupConfig): Promise<CleanupResult> {
    const startTime = Date.now();
    const strategy = this.parseStrategy(config?.strategy);

    try {
      switch (strategy) {
        case CleanupStrategy.TRUNCATE_ALL:
          await this.truncateAll();
          return {
            success: true,
            strategy,
            tablesAffected: [
              'transactions',
              'accounts',
              'linked_items',
              'budget_rules',
              'budgets',
              'users',
            ],
            duration: Date.now() - startTime,
          };

        case CleanupStrategy.DELETE_TEST_USERS: {
          await this.deleteTestUsers();
          return {
            success: true,
            strategy,
            tablesAffected: ['users', 'linked_items', 'accounts', 'transactions'],
            duration: Date.now() - startTime,
          };
        }

        case CleanupStrategy.NONE:
          return {
            success: true,
            strategy,
            duration: Date.now() - startTime,
          };

        default:
          throw new Error(`Unknown cleanup strategy: ${strategy}`);
      }
    } catch (error: unknown) {
      const errorMessage = error instanceof Error ? error.message : String(error);
      return {
        success: false,
        strategy,
        error: `Cleanup failed: ${errorMessage}`,
        duration: Date.now() - startTime,
      };
    }
  }

  /**
   * Truncate all tables with CASCADE
   * Fastest cleanup method (10x faster than DELETE)
   */
  private async truncateAll(): Promise<void> {
    await this.pool.query(`
      TRUNCATE TABLE
        transactions,
        accounts,
        linked_items,
        budget_rules,
        budgets,
        users
      RESTART IDENTITY CASCADE
    `);
  }

  /**
   * Delete test users and cascade to related data
   * Preserves production-like users if any exist
   */
  private async deleteTestUsers(): Promise<number> {
    const result = await this.pool.query(`
      DELETE FROM users
      WHERE email LIKE '%@test.example.com'
         OR email LIKE 'test-%@%'
         OR auth0_user_id LIKE 'auth0|test-%'
      RETURNING id
    `);

    return result.rowCount || 0;
  }

  /**
   * Get database snapshot for verification
   * Useful for debugging cleanup issues
   */
  async getSnapshot(): Promise<{
    users: number;
    linked_items: number;
    accounts: number;
    transactions: number;
    budgets: number;
    budget_rules: number;
  }> {
    const [users, linkedItems, accounts, transactions, budgets, budgetRules] =
      await Promise.all([
        this.pool.query('SELECT COUNT(*) as count FROM users'),
        this.pool.query('SELECT COUNT(*) as count FROM linked_items'),
        this.pool.query('SELECT COUNT(*) as count FROM accounts'),
        this.pool.query('SELECT COUNT(*) as count FROM transactions'),
        this.pool.query('SELECT COUNT(*) as count FROM budgets'),
        this.pool.query('SELECT COUNT(*) as count FROM budget_rules'),
      ]);

    return {
      users: parseInt(users.rows[0].count, 10),
      linked_items: parseInt(linkedItems.rows[0].count, 10),
      accounts: parseInt(accounts.rows[0].count, 10),
      transactions: parseInt(transactions.rows[0].count, 10),
      budgets: parseInt(budgets.rows[0].count, 10),
      budget_rules: parseInt(budgetRules.rows[0].count, 10),
    };
  }

  /**
   * Verify cleanup was successful
   */
  async verifyClean(strategy: CleanupStrategy): Promise<boolean> {
    const snapshot = await this.getSnapshot();

    switch (strategy) {
      case CleanupStrategy.TRUNCATE_ALL:
        // All tables should be empty
        return (
          snapshot.users === 0 &&
          snapshot.linked_items === 0 &&
          snapshot.accounts === 0 &&
          snapshot.transactions === 0 &&
          snapshot.budgets === 0 &&
          snapshot.budget_rules === 0
        );

      case CleanupStrategy.DELETE_TEST_USERS: {
        // Test users should be gone (check via email pattern)
        const testUserCheck = await this.pool.query(`
          SELECT COUNT(*) as count FROM users
          WHERE email LIKE '%@test.example.com'
             OR email LIKE 'test-%@%'
             OR auth0_user_id LIKE 'auth0|test-%'
        `);
        return parseInt(testUserCheck.rows[0].count, 10) === 0;
      }

      case CleanupStrategy.NONE:
        // No verification needed
        return true;

      default:
        return false;
    }
  }

  /**
   * Parse cleanup strategy from string or CleanupConfig
   */
  private parseStrategy(strategy?: string): CleanupStrategy {
    if (!strategy) {
      return CleanupStrategy.TRUNCATE_ALL; // Default
    }

    switch (strategy.toLowerCase()) {
      case 'delete_test_users':
        return CleanupStrategy.DELETE_TEST_USERS;
      case 'truncate_all':
        return CleanupStrategy.TRUNCATE_ALL;
      case 'none':
        return CleanupStrategy.NONE;
      default:
        console.warn(`Unknown cleanup strategy "${strategy}", using truncate_all`);
        return CleanupStrategy.TRUNCATE_ALL;
    }
  }

  /**
   * Reset sequences for all tables
   * Useful when you want clean IDs starting from 1
   */
  async resetSequences(): Promise<void> {
    await this.pool.query(`
      ALTER SEQUENCE users_id_seq RESTART WITH 1;
      ALTER SEQUENCE linked_items_id_seq RESTART WITH 1;
      ALTER SEQUENCE accounts_id_seq RESTART WITH 1;
      ALTER SEQUENCE transactions_id_seq RESTART WITH 1;
      ALTER SEQUENCE budgets_id_seq RESTART WITH 1;
      ALTER SEQUENCE budget_rules_id_seq RESTART WITH 1;
    `);
  }
}
