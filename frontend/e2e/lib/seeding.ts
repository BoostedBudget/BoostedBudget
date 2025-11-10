/**
 * Database Seeding Utilities
 *
 * Provides methods to seed test data into PostgreSQL database
 * Uses connection pooling for performance
 * Supports batch operations for speed
 *
 * Issue #331: E2E Test Scenarios Framework
 */

import { Pool, QueryResult } from 'pg';
import { v4 as uuidv4, v5 as uuidv5 } from 'uuid';
import type {
  User,
  LinkedItem,
  Account,
  Transaction,
  UserSetup,
  LinkedItemSetup,
  AccountSetup,
  TransactionSetup,
} from './types';
import { FixtureGenerator } from './fixtures';

/**
 * Database seeder class
 * Manages database connections and seeding operations
 */
export class DatabaseSeeder {
  private pool: Pool;
  private fixtureGenerator: FixtureGenerator;
  private static pools: Map<string, Pool> = new Map();

  /**
   * Create database seeder
   * @param databaseUrl PostgreSQL connection URL
   * @param seed Optional seed for deterministic data
   */
  constructor(databaseUrl: string, seed?: number) {
    this.pool = DatabaseSeeder.getPool(databaseUrl);
    this.fixtureGenerator = new FixtureGenerator(seed);
  }

  /**
   * Get or create connection pool (singleton per database URL)
   */
  private static getPool(databaseUrl: string): Pool {
    if (!this.pools.has(databaseUrl)) {
      this.pools.set(
        databaseUrl,
        new Pool({
          connectionString: databaseUrl,
          max: 10, // Maximum 10 connections per worker
          idleTimeoutMillis: 30000,
          connectionTimeoutMillis: 10000,
        })
      );
    }
    return this.pools.get(databaseUrl)!;
  }

  /**
   * Execute a SQL query
   */
  async query<T = unknown>(sql: string, params?: unknown[]): Promise<QueryResult<T>> {
    return this.pool.query(sql, params);
  }

  /**
   * Convert string ID to UUID format
   * Accepts either UUID format or string, returns valid UUID
   * Uses deterministic generation (UUIDv5) for string inputs
   */
  public ensureUUID(id: string | undefined): string {
    if (!id) {
      return uuidv4(); // Generate random UUID if not provided
    }

    // If already UUID format, return as-is
    if (/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(id)) {
      return id;
    }

    // Generate deterministic UUID from string using UUIDv5
    // Using DNS namespace for consistency
    const namespace = '6ba7b810-9dad-11d1-80b4-00c04fd430c8';
    return uuidv5(id, namespace);
  }

  /**
   * Create a user
   */
  async createUser(setup: UserSetup): Promise<User> {
    // Generate or convert ID to UUID format
    const id = this.ensureUUID(setup.id || this.fixtureGenerator.generateUser().id!);

    // Generate Auth0 user ID if set to auto-generate
    const auth0UserId =
      setup.auth0_user_id === 'auto-generate'
        ? this.fixtureGenerator.generateAuth0UserId()
        : setup.auth0_user_id;

    const result = await this.query<User>(
      `
      INSERT INTO users (
        id, email, subscription_tier, auth0_user_id, subscription_status, primary_region,
        email_verified, created_at, updated_at
      )
      VALUES ($1, $2, $3, $4, $5, $6, $7, NOW(), NOW())
      ON CONFLICT (id) DO NOTHING
      RETURNING *
      `,
      [
        id,
        setup.email,
        setup.tier,
        auth0UserId,
        setup.subscription_status || 'active',
        'iad', // Default region for MVP (Fly.io IATA code for us-east-1)
        true, // Email verified for test users
      ]
    );

    if (result.rows.length === 0) {
      throw new Error(`Failed to create user: ${setup.email} (may already exist)`);
    }

    return result.rows[0];
  }

  /**
   * Create a linked item (Plaid connection)
   */
  async createLinkedItem(
    setup: LinkedItemSetup,
    userId: string
  ): Promise<LinkedItem> {
    const id = this.ensureUUID(setup.id || this.fixtureGenerator.generateLinkedItem(userId).id!);
    // Issue #331: Convert item_id to UUID format for admin webhook API validation
    // Plaid item_id must be UUID format - use deterministic UUIDv5 from readable names
    const itemId = this.ensureUUID(setup.item_id || `plaid-item-test-${id}`);

    // Get institution ID if not provided
    const institutionId = setup.institution_id || this.getInstitutionId(setup.institution);

    const result = await this.query<LinkedItem>(
      `
      INSERT INTO linked_items (
        id, user_id, item_id, institution_name, institution_id,
        access_token_encrypted, cursor, status, created_at, updated_at
      )
      VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW(), NOW())
      ON CONFLICT (id) DO NOTHING
      RETURNING *
      `,
      [
        id,
        userId,
        itemId,
        setup.institution,
        institutionId,
        'test-encrypted-token', // Placeholder for test
        setup.cursor || null,
        setup.status || 'active',
      ]
    );

    if (result.rows.length === 0) {
      throw new Error(`Failed to create linked item for user ${userId}`);
    }

    return result.rows[0];
  }

  /**
   * Create an account
   */
  async createAccount(
    setup: AccountSetup,
    userId: string,
    linkedItemId: string
  ): Promise<Account> {
    const id = this.ensureUUID(setup.id || this.fixtureGenerator.generateAccount(linkedItemId, userId).id!);
    const plaidAccountId =
      setup.plaid_account_id || `plaid-acc-test-${id}`;

    const result = await this.query<Account>(
      `
      INSERT INTO accounts (
        id, user_id, linked_item_id, plaid_account_id, name, type, subtype,
        current_balance, available_balance, currency_code, created_at, updated_at
      )
      VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW(), NOW())
      ON CONFLICT (id) DO NOTHING
      RETURNING *
      `,
      [
        id,
        userId,
        linkedItemId,
        plaidAccountId,
        setup.name,
        setup.type,
        setup.subtype,
        setup.current_balance,
        setup.available_balance || setup.current_balance,
        'USD', // Default currency
      ]
    );

    if (result.rows.length === 0) {
      throw new Error(`Failed to create account ${setup.name} for user ${userId}`);
    }

    return result.rows[0];
  }

  /**
   * Create a single transaction
   */
  async createTransaction(
    setup: TransactionSetup,
    accountId: string
  ): Promise<Transaction> {
    const id = this.ensureUUID(setup.id || this.fixtureGenerator.generateTransaction(accountId).id!);
    const externalTxnId =
      setup.external_transaction_id || `plaid-txn-test-${id}`;

    // Query account to get user_id and linked_item_id for foreign key constraints
    const accountResult = await this.query<Account>(
      'SELECT user_id, linked_item_id FROM accounts WHERE id = $1',
      [accountId]
    );

    if (accountResult.rows.length === 0) {
      throw new Error(`Account not found: ${accountId}`);
    }

    const account = accountResult.rows[0];

    const result = await this.query<Transaction>(
      `
      INSERT INTO transactions (
        id, user_id, linked_item_id, account_id, source,
        external_transaction_id, amount, date, name,
        merchant_name, category, pending, user_notes, created_at, updated_at
      )
      VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, NOW(), NOW())
      ON CONFLICT (id) DO NOTHING
      RETURNING *
      `,
      [
        id,
        account.user_id,
        account.linked_item_id,
        accountId,
        'plaid', // Default source for test transactions
        externalTxnId,
        setup.amount,
        setup.date,
        setup.name,
        setup.merchant_name || setup.name,
        JSON.stringify([setup.category || 'Other']), // Convert to JSONB array format
        setup.pending || false,
        setup.user_notes || null,
      ]
    );

    if (result.rows.length === 0) {
      throw new Error(`Failed to create transaction for account ${accountId}`);
    }

    return result.rows[0];
  }

  /**
   * Create multiple transactions (batch insert for performance)
   */
  async createTransactions(
    transactions: Array<Partial<Transaction>>
  ): Promise<Transaction[]> {
    if (transactions.length === 0) {
      return [];
    }

    // Query all account IDs to get user_id and linked_item_id mappings
    const accountIds = [...new Set(transactions.map(txn => txn.account_id).filter(Boolean))];
    const accountMappings = new Map<string, { user_id: string; linked_item_id: string }>();

    if (accountIds.length > 0) {
      const accountResult = await this.query<Account>(
        `SELECT id, user_id, linked_item_id FROM accounts WHERE id = ANY($1)`,
        [accountIds]
      );

      for (const account of accountResult.rows) {
        accountMappings.set(account.id, {
          user_id: account.user_id,
          linked_item_id: account.linked_item_id,
        });
      }
    }

    // Build batch insert query
    const values: unknown[] = [];
    const valueStrings: string[] = [];

    transactions.forEach((txn, index) => {
      const offset = index * 14;
      valueStrings.push(
        `($${offset + 1}, $${offset + 2}, $${offset + 3}, $${offset + 4}, $${offset + 5}, ` +
          `$${offset + 6}, $${offset + 7}, $${offset + 8}, $${offset + 9}, $${offset + 10}, ` +
          `$${offset + 11}, $${offset + 12}, $${offset + 13}, $${offset + 14})`
      );

      const id = this.ensureUUID(txn.id || this.fixtureGenerator.generateTransaction(txn.account_id!).id!);
      const externalId = txn.external_transaction_id || `plaid-txn-test-${id}`;
      const accountMapping = accountMappings.get(txn.account_id!);

      if (!accountMapping) {
        throw new Error(`Account mapping not found for account_id: ${txn.account_id}`);
      }

      values.push(
        id,
        accountMapping.user_id,
        accountMapping.linked_item_id,
        txn.account_id,
        'plaid', // Default source for test transactions
        externalId,
        txn.amount,
        txn.date,
        txn.name,
        txn.merchant_name || txn.name,
        JSON.stringify([txn.category || 'Other']), // Convert to JSONB array format
        txn.pending || false,
        txn.user_notes || null,
        new Date() // created_at
      );
    });

    const sql = `
      INSERT INTO transactions (
        id, user_id, linked_item_id, account_id, source,
        external_transaction_id, amount, date, name,
        merchant_name, category, pending, user_notes, created_at
      )
      VALUES ${valueStrings.join(', ')}
      ON CONFLICT (id) DO NOTHING
      RETURNING *
    `;

    const result = await this.query<Transaction>(sql, values);
    return result.rows;
  }

  /**
   * Get database snapshot for debugging
   */
  async getDatabaseSnapshot(userId?: string): Promise<{
    message?: string;
    users?: unknown[];
    linked_items?: unknown[];
    accounts?: unknown[];
    transaction_count?: number | string;
  }> {
    if (!userId) {
      return { message: 'No user ID provided for snapshot' };
    }

    const [users, linkedItems, accounts, transactions] = await Promise.all([
      this.query('SELECT * FROM users WHERE id = $1', [userId]),
      this.query('SELECT * FROM linked_items WHERE user_id = $1', [userId]),
      this.query('SELECT * FROM accounts WHERE user_id = $1', [userId]),
      this.query('SELECT COUNT(*) as count FROM transactions WHERE account_id IN (SELECT id FROM accounts WHERE user_id = $1)', [userId]),
    ]);

    return {
      users: users.rows,
      linked_items: linkedItems.rows,
      accounts: accounts.rows,
      transaction_count: transactions.rows[0].count,
    };
  }

  /**
   * Get institution ID for a given institution name
   */
  private getInstitutionId(name: string): string {
    const mapping: Record<string, string> = {
      'Chase': 'ins_3',
      'Bank of America': 'ins_4',
      'Wells Fargo': 'ins_6',
      'Capital One': 'ins_5',
      'Citibank': 'ins_7',
    };
    return mapping[name] || 'ins_99';
  }

  /**
   * Close all database connections
   */
  static async closeAll(): Promise<void> {
    const closePromises = Array.from(this.pools.values()).map((pool) => pool.end());
    await Promise.all(closePromises);
    this.pools.clear();
  }

  /**
   * Close this seeder's connection pool
   */
  async close(): Promise<void> {
    // Don't close the pool here - it's shared across tests
    // Use closeAll() for cleanup at the end of all tests
  }
}
