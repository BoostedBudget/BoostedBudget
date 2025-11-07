/**
 * Scenario Runner
 *
 * Main executor that orchestrates scenario execution
 * Coordinates seeding, steps, assertions, and cleanup
 * Provides detailed execution context and error reporting
 *
 * Issue #331: E2E Test Scenarios Framework
 */

import { Page } from '@playwright/test';
import type { Pool } from 'pg';
import type {
  Scenario,
  SetupConfig,
  Step,
  ScenarioContext,
  AssertionResult,
} from './types';
import { ScenarioLoader } from './scenario-loader';
import { DatabaseSeeder } from './seeding';
import { FixtureGenerator } from './fixtures';
import { ApiClient } from './api-client';
import { AssertionRunner } from './assertions';
import { CleanupManager } from './cleanup';

/**
 * Scenario execution result
 */
export interface ScenarioResult {
  scenario: string;
  success: boolean;
  duration: number; // milliseconds
  setupDuration?: number;
  stepsDuration?: number;
  assertionsDuration?: number;
  cleanupDuration?: number;
  context: ScenarioContext;
  assertions: AssertionResult[];
  error?: string;
  failedStep?: number;
}

/**
 * Runner configuration
 */
export interface RunnerConfig {
  databaseUrl: string;
  apiBaseUrl: string;
  scenariosDir?: string;
  templatesDir?: string;
  seed?: number; // For deterministic fixture generation
  skipCleanup?: boolean; // For debugging
}

/**
 * Scenario runner class
 * Executes scenarios from YAML files
 */
export class ScenarioRunner {
  private loader: ScenarioLoader;
  private seeder: DatabaseSeeder;
  private fixtureGenerator: FixtureGenerator;
  private apiClient: ApiClient;
  private assertionRunner: AssertionRunner;
  private cleanupManager: CleanupManager;
  private dbPool: Pool;
  private config: RunnerConfig;

  /**
   * Create scenario runner
   */
  constructor(config: RunnerConfig, dbPool: Pool) {
    this.config = config;
    this.dbPool = dbPool;

    this.loader = new ScenarioLoader(config.scenariosDir, config.templatesDir);
    this.seeder = new DatabaseSeeder(config.databaseUrl, config.seed);
    this.fixtureGenerator = new FixtureGenerator(config.seed);
    this.apiClient = new ApiClient(config.apiBaseUrl);
    this.assertionRunner = new AssertionRunner(this.apiClient);
    this.cleanupManager = new CleanupManager(dbPool);
  }

  /**
   * Run a scenario from file
   */
  async runFromFile(scenarioPath: string, page: Page): Promise<ScenarioResult> {
    const scenario = await this.loader.load(scenarioPath);
    return this.run(scenario, page);
  }

  /**
   * Run a scenario
   */
  async run(scenario: Scenario, page: Page): Promise<ScenarioResult> {
    const startTime = Date.now();
    const context: ScenarioContext = {};
    let setupDuration = 0;
    let stepsDuration = 0;
    let assertionsDuration = 0;
    let cleanupDuration = 0;

    try {
      // Phase 1: Setup (seed database)
      const setupStart = Date.now();
      await this.executeSetup(scenario.scenario.setup, context);
      setupDuration = Date.now() - setupStart;

      // Phase 2: Execute steps
      const stepsStart = Date.now();
      for (let i = 0; i < scenario.scenario.steps.length; i++) {
        await this.executeStep(scenario.scenario.steps[i], context, page);
      }
      stepsDuration = Date.now() - stepsStart;

      // Phase 3: Run assertions
      const assertionsStart = Date.now();
      const assertionResults = await this.assertionRunner.run(
        scenario.scenario.assertions,
        context,
        page,
        this.dbPool
      );
      assertionsDuration = Date.now() - assertionsStart;

      // Phase 4: Cleanup
      const cleanupStart = Date.now();
      if (!this.config.skipCleanup) {
        await this.cleanupManager.cleanup(scenario.scenario.cleanup);
      }
      cleanupDuration = Date.now() - cleanupStart;

      // Check if all assertions passed
      const allPassed = assertionResults.every((r) => r.passed);

      return {
        scenario: scenario.scenario.name,
        success: allPassed,
        duration: Date.now() - startTime,
        setupDuration,
        stepsDuration,
        assertionsDuration,
        cleanupDuration,
        context,
        assertions: assertionResults,
      };
    } catch (error: unknown) {
      // Attempt cleanup even on failure
      const cleanupStart = Date.now();
      if (!this.config.skipCleanup) {
        try {
          await this.cleanupManager.cleanup(scenario.scenario.cleanup);
        } catch (cleanupError) {
          console.error('Cleanup failed:', cleanupError);
        }
      }
      cleanupDuration = Date.now() - cleanupStart;

      const errorMessage = error instanceof Error ? error.message : String(error);
      return {
        scenario: scenario.scenario.name,
        success: false,
        duration: Date.now() - startTime,
        setupDuration,
        stepsDuration,
        assertionsDuration,
        cleanupDuration,
        context,
        assertions: [],
        error: `Scenario execution failed: ${errorMessage}`,
      };
    }
  }

  /**
   * Execute setup phase (seed database)
   */
  private async executeSetup(
    setup: SetupConfig,
    context: ScenarioContext
  ): Promise<void> {
    // Create user
    const user = await this.seeder.createUser(setup.user);
    context.user = user;

    // Create linked items
    if (setup.linked_items) {
      context.linked_items = [];
      for (const itemSetup of setup.linked_items) {
        const item = await this.seeder.createLinkedItem(itemSetup, user.id!);
        context.linked_items.push(item);
      }
    }

    // Create accounts
    if (setup.accounts && context.linked_items) {
      context.accounts = [];
      for (const accountSetup of setup.accounts) {
        // Find the linked item for this account
        const linkedItemId =
          accountSetup.linked_item_id || context.linked_items[0]?.id;
        if (!linkedItemId) {
          throw new Error('No linked_item_id available for account');
        }

        const account = await this.seeder.createAccount(
          accountSetup,
          user.id!,
          linkedItemId
        );
        context.accounts.push(account);
      }
    }

    // Create existing transactions (if specified)
    if (setup.existing_transactions && context.accounts) {
      context.transactions = [];
      for (const txnSetup of setup.existing_transactions) {
        // Find the account for this transaction
        const accountId = txnSetup.account_id || context.accounts[0]?.id;
        if (!accountId) {
          throw new Error('No account_id available for transaction');
        }

        const transaction = await this.seeder.createTransaction(txnSetup, accountId);
        context.transactions.push(transaction);
      }
    }

    // Generate transactions from templates
    if (setup.generate_transactions && context.accounts) {
      const accountId = context.accounts[0]?.id;
      if (!accountId) {
        throw new Error('No account available for generated transactions');
      }

      const generated = this.fixtureGenerator.generateFromTemplate(
        setup.generate_transactions.template,
        {
          count: setup.generate_transactions.count,
          dateRange: setup.generate_transactions.date_range,
          accountId,
        }
      );

      // Batch insert for performance
      const insertedTransactions = await this.seeder.createTransactions(generated);
      context.transactions = context.transactions || [];
      context.transactions.push(...insertedTransactions);
    }
  }

  /**
   * Execute a single step
   */
  private async executeStep(
    step: Step,
    context: ScenarioContext,
    page: Page
  ): Promise<void> {
    switch (step.action) {
      case 'api_call':
        await this.executeApiCall(step, context);
        break;

      case 'delay':
        await this.executeDelay(step);
        break;

      case 'navigate':
        await this.executeNavigate(step, page);
        break;

      case 'click':
        await this.executeClick(step, page);
        break;

      case 'fill':
        await this.executeFill(step, page);
        break;

      case 'wait_for_selector':
        await this.executeWaitForSelector(step, page);
        break;

      default:
        throw new Error(`Unknown step action: ${(step as Step).action}`);
    }
  }

  /**
   * Execute API call step
   */
  private async executeApiCall(step: Extract<Step, { action: 'api_call' }>, context: ScenarioContext): Promise<void> {
    const response = await this.apiClient.request({
      method: step.method,
      url: step.endpoint,
      body: step.body,
      headers: step.headers,
    });

    // Store response in context if save_as specified
    if (step.save_as) {
      context[step.save_as] = response.data;
    }

    // Verify status code if specified
    if (step.expect_status && response.status !== step.expect_status) {
      throw new Error(
        `API call failed: expected status ${step.expect_status}, got ${response.status}`
      );
    }
  }

  /**
   * Execute delay step
   */
  private async executeDelay(step: Extract<Step, { action: 'delay' }>): Promise<void> {
    await new Promise((resolve) => setTimeout(resolve, step.duration));
  }

  /**
   * Execute navigate step
   */
  private async executeNavigate(step: Extract<Step, { action: 'navigate' }>, page: Page): Promise<void> {
    await page.goto(step.url);
  }

  /**
   * Execute click step
   */
  private async executeClick(step: Extract<Step, { action: 'click' }>, page: Page): Promise<void> {
    await page.locator(step.selector).click();
  }

  /**
   * Execute fill step
   */
  private async executeFill(step: Extract<Step, { action: 'fill' }>, page: Page): Promise<void> {
    await page.locator(step.selector).fill(step.value);
  }

  /**
   * Execute wait for selector step
   */
  private async executeWaitForSelector(
    step: Extract<Step, { action: 'wait_for_selector' }>,
    page: Page
  ): Promise<void> {
    await page.locator(step.selector).waitFor({
      timeout: step.timeout || 30000,
    });
  }

  /**
   * Get scenario context (for debugging)
   */
  getContext(): ScenarioContext {
    return {};
  }

  /**
   * Get database pool
   */
  getDbPool(): Pool {
    return this.dbPool;
  }

  /**
   * Get API client
   */
  getApiClient(): ApiClient {
    return this.apiClient;
  }

  /**
   * Close connections
   */
  async close(): Promise<void> {
    await this.seeder.close();
  }
}
