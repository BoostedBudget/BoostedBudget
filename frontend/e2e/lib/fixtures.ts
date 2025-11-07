/**
 * Fixture Generator
 *
 * Generates realistic test data using Faker.js
 * Supports templates for DRY test data creation
 *
 * Issue #331: E2E Test Scenarios Framework
 */

import { faker } from '@faker-js/faker';
import type { User, LinkedItem, Account, Transaction } from './types';

/**
 * Template configuration for generating data from templates
 */
export interface TemplateConfig {
  count?: number;
  dateRange?: string; // e.g., "last_30_days", "last_90_days"
  accountId?: string;
  [key: string]: any;
}

/**
 * Fixture generator class
 * Provides methods to generate realistic test data
 */
export class FixtureGenerator {
  private seed: number | undefined;

  /**
   * Create fixture generator
   * @param seed Optional seed for deterministic data generation
   */
  constructor(seed?: number) {
    this.seed = seed;
    if (seed !== undefined) {
      faker.seed(seed);
    }
  }

  /**
   * Generate Auth0 user ID
   */
  generateAuth0UserId(): string {
    return `auth0|test-${faker.string.nanoid(16)}`;
  }

  /**
   * Generate a user
   */
  generateUser(
    tier: 'free' | 'premium_lite' | 'premium' | 'premium_plus' = 'free'
  ): Partial<User> {
    return {
      id: faker.string.uuid(),
      email: `test-${faker.string.nanoid(8)}@test.example.com`,
      tier,
      auth0_user_id: this.generateAuth0UserId(),
      subscription_status: 'active',
      created_at: faker.date.past(),
    };
  }

  /**
   * Generate a linked item (Plaid connection)
   */
  generateLinkedItem(userId: string, institution: string = 'Chase'): Partial<LinkedItem> {
    return {
      id: faker.string.uuid(),
      user_id: userId,
      item_id: `plaid-item-${faker.string.nanoid(16)}`,
      institution_name: institution,
      institution_id: this.getInstitutionId(institution),
      cursor: null,
      status: 'active',
      created_at: faker.date.past(),
    };
  }

  /**
   * Generate an account
   */
  generateAccount(
    linkedItemId: string,
    userId: string,
    type: 'checking' | 'savings' | 'credit card' = 'checking'
  ): Partial<Account> {
    const accountType = this.getAccountType(type);
    const subtype = type;

    return {
      id: faker.string.uuid(),
      linked_item_id: linkedItemId,
      user_id: userId,
      plaid_account_id: `plaid-acc-${faker.string.nanoid(16)}`,
      name: this.getAccountName(type),
      type: accountType,
      subtype,
      current_balance: faker.number.float({ min: 100, max: 10000, fractionDigits: 2 }),
      available_balance: faker.number.float({ min: 100, max: 10000, fractionDigits: 2 }),
      created_at: faker.date.past(),
    };
  }

  /**
   * Generate a transaction
   */
  generateTransaction(accountId: string, daysAgo: number = 0): Partial<Transaction> {
    const merchants = [
      'Amazon',
      'Starbucks',
      'Whole Foods',
      'Shell',
      'Target',
      'Walmart',
      'Costco',
      'Safeway',
      'CVS',
      'Walgreens',
      'Netflix',
      'Spotify',
      'Apple',
      'Google',
      'Microsoft',
    ];

    const categories = [
      'Shopping',
      'Food and Drink',
      'Gas',
      'Entertainment',
      'Groceries',
      'Healthcare',
      'Travel',
      'Bills',
    ];

    const merchant = faker.helpers.arrayElement(merchants);
    const date = new Date();
    date.setDate(date.getDate() - daysAgo);

    return {
      id: faker.string.uuid(),
      account_id: accountId,
      external_transaction_id: `plaid-txn-${faker.string.nanoid(16)}`,
      amount: -faker.number.float({ min: 5, max: 200, fractionDigits: 2 }),
      date: date.toISOString().split('T')[0], // YYYY-MM-DD format
      name: merchant,
      merchant_name: merchant,
      category: faker.helpers.arrayElement(categories),
      pending: false,
      user_notes: null,
      created_at: new Date(),
    };
  }

  /**
   * Generate multiple transactions
   */
  generateTransactions(accountId: string, count: number, dateRange?: string): Array<Partial<Transaction>> {
    const transactions: Array<Partial<Transaction>> = [];
    const days = this.parseDateRange(dateRange);

    for (let i = 0; i < count; i++) {
      const daysAgo = faker.number.int({ min: 0, max: days });
      transactions.push(this.generateTransaction(accountId, daysAgo));
    }

    return transactions;
  }

  /**
   * Generate transaction from template
   */
  generateFromTemplate(templateName: string, config: TemplateConfig): Array<Partial<Transaction>> {
    // This will be enhanced when we load actual templates from templates/*.yaml
    // For now, provide some built-in templates

    const count = config.count || 1;
    const accountId = config.accountId || '';
    const dateRange = config.dateRange || 'last_30_days';

    switch (templateName) {
      case 'monthly_salary':
        return this.generateSalaryTransactions(accountId, count, dateRange);

      case 'grocery_shopping':
        return this.generateGroceryTransactions(accountId, count, dateRange);

      case 'gas_purchases':
        return this.generateGasTransactions(accountId, count, dateRange);

      case 'utility_payments':
        return this.generateUtilityTransactions(accountId, count, dateRange);

      case 'subscription_payments':
        return this.generateSubscriptionTransactions(accountId, count, dateRange);

      default:
        throw new Error(`Unknown template: ${templateName}`);
    }
  }

  /**
   * Generate salary/paycheck transactions
   */
  private generateSalaryTransactions(
    accountId: string,
    count: number,
    dateRange: string
  ): Array<Partial<Transaction>> {
    const transactions: Array<Partial<Transaction>> = [];
    const days = this.parseDateRange(dateRange);
    const interval = Math.floor(days / count);

    for (let i = 0; i < count; i++) {
      const daysAgo = i * interval;
      const date = new Date();
      date.setDate(date.getDate() - daysAgo);

      transactions.push({
        id: faker.string.uuid(),
        account_id: accountId,
        external_transaction_id: `plaid-txn-${faker.string.nanoid(16)}`,
        amount: faker.number.float({ min: 2000, max: 5000, fractionDigits: 2 }),
        date: date.toISOString().split('T')[0],
        name: 'Payroll Deposit',
        merchant_name: 'Employer Inc',
        category: 'Income',
        pending: false,
        user_notes: null,
        created_at: new Date(),
      });
    }

    return transactions;
  }

  /**
   * Generate grocery transactions
   */
  private generateGroceryTransactions(
    accountId: string,
    count: number,
    dateRange: string
  ): Array<Partial<Transaction>> {
    const transactions: Array<Partial<Transaction>> = [];
    const days = this.parseDateRange(dateRange);
    const groceryStores = ['Whole Foods', 'Safeway', 'Trader Joe\'s', 'Costco', 'Kroger'];

    for (let i = 0; i < count; i++) {
      const daysAgo = faker.number.int({ min: 0, max: days });
      const date = new Date();
      date.setDate(date.getDate() - daysAgo);
      const store = faker.helpers.arrayElement(groceryStores);

      transactions.push({
        id: faker.string.uuid(),
        account_id: accountId,
        external_transaction_id: `plaid-txn-${faker.string.nanoid(16)}`,
        amount: -faker.number.float({ min: 30, max: 150, fractionDigits: 2 }),
        date: date.toISOString().split('T')[0],
        name: store,
        merchant_name: store,
        category: 'Groceries',
        pending: false,
        user_notes: null,
        created_at: new Date(),
      });
    }

    return transactions;
  }

  /**
   * Generate gas purchase transactions
   */
  private generateGasTransactions(
    accountId: string,
    count: number,
    dateRange: string
  ): Array<Partial<Transaction>> {
    const transactions: Array<Partial<Transaction>> = [];
    const days = this.parseDateRange(dateRange);
    const gasStations = ['Shell', 'Chevron', '76', 'Exxon', 'BP'];

    for (let i = 0; i < count; i++) {
      const daysAgo = faker.number.int({ min: 0, max: days });
      const date = new Date();
      date.setDate(date.getDate() - daysAgo);
      const station = faker.helpers.arrayElement(gasStations);

      transactions.push({
        id: faker.string.uuid(),
        account_id: accountId,
        external_transaction_id: `plaid-txn-${faker.string.nanoid(16)}`,
        amount: -faker.number.float({ min: 30, max: 80, fractionDigits: 2 }),
        date: date.toISOString().split('T')[0],
        name: station,
        merchant_name: station,
        category: 'Gas',
        pending: false,
        user_notes: null,
        created_at: new Date(),
      });
    }

    return transactions;
  }

  /**
   * Generate utility payment transactions
   */
  private generateUtilityTransactions(
    accountId: string,
    count: number,
    dateRange: string
  ): Array<Partial<Transaction>> {
    const transactions: Array<Partial<Transaction>> = [];
    const days = this.parseDateRange(dateRange);
    const utilities = ['PG&E Electric', 'Comcast Internet', 'Water District', 'AT&T Phone'];
    const interval = Math.floor(days / count);

    for (let i = 0; i < count; i++) {
      const daysAgo = i * interval;
      const date = new Date();
      date.setDate(date.getDate() - daysAgo);
      const utility = faker.helpers.arrayElement(utilities);

      transactions.push({
        id: faker.string.uuid(),
        account_id: accountId,
        external_transaction_id: `plaid-txn-${faker.string.nanoid(16)}`,
        amount: -faker.number.float({ min: 50, max: 200, fractionDigits: 2 }),
        date: date.toISOString().split('T')[0],
        name: utility,
        merchant_name: utility,
        category: 'Bills',
        pending: false,
        user_notes: null,
        created_at: new Date(),
      });
    }

    return transactions;
  }

  /**
   * Generate subscription payment transactions
   */
  private generateSubscriptionTransactions(
    accountId: string,
    count: number,
    dateRange: string
  ): Array<Partial<Transaction>> {
    const transactions: Array<Partial<Transaction>> = [];
    const days = this.parseDateRange(dateRange);
    const subscriptions = ['Netflix', 'Spotify', 'Apple Music', 'Adobe Creative Cloud', 'Amazon Prime'];
    const interval = Math.floor(days / count);

    for (let i = 0; i < count; i++) {
      const daysAgo = i * interval;
      const date = new Date();
      date.setDate(date.getDate() - daysAgo);
      const subscription = faker.helpers.arrayElement(subscriptions);

      transactions.push({
        id: faker.string.uuid(),
        account_id: accountId,
        external_transaction_id: `plaid-txn-${faker.string.nanoid(16)}`,
        amount: -faker.number.float({ min: 9.99, max: 49.99, fractionDigits: 2 }),
        date: date.toISOString().split('T')[0],
        name: subscription,
        merchant_name: subscription,
        category: 'Entertainment',
        pending: false,
        user_notes: null,
        created_at: new Date(),
      });
    }

    return transactions;
  }

  /**
   * Parse date range string to number of days
   */
  private parseDateRange(dateRange?: string): number {
    if (!dateRange) return 30;

    const match = dateRange.match(/last[_\s](\d+)[_\s]days?/i);
    if (match) {
      return parseInt(match[1], 10);
    }

    // Default ranges
    const ranges: Record<string, number> = {
      'last_week': 7,
      'last_month': 30,
      'last_30_days': 30,
      'last_60_days': 60,
      'last_90_days': 90,
      'last_6_months': 180,
      'last_year': 365,
    };

    return ranges[dateRange] || 30;
  }

  /**
   * Get Plaid institution ID for a given institution name
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
   * Get account name for a given account type
   */
  private getAccountName(type: string): string {
    const mapping: Record<string, string> = {
      'checking': 'Everyday Checking',
      'savings': 'Savings Account',
      'credit card': 'Credit Card',
    };
    return mapping[type] || 'Account';
  }

  /**
   * Get Plaid account type for a given subtype
   */
  private getAccountType(subtype: string): string {
    if (['checking', 'savings'].includes(subtype)) return 'depository';
    if (subtype === 'credit card') return 'credit';
    return 'other';
  }

  /**
   * Reset faker seed
   */
  resetSeed(seed?: number): void {
    this.seed = seed;
    if (seed !== undefined) {
      faker.seed(seed);
    }
  }
}
