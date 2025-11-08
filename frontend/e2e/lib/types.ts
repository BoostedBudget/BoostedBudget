/**
 * TypeScript Types and Zod Schemas for E2E Test Scenarios
 *
 * Defines the complete structure for YAML scenario files with runtime validation.
 * Issue #331: E2E Test Scenarios Framework
 */

import { z } from 'zod';

// ============================================================================
// Setup Schemas
// ============================================================================

/**
 * User setup configuration
 */
export const UserSetupSchema = z.object({
  id: z.string().optional(), // Auto-generated if not provided
  email: z.string().email(),
  tier: z.enum(['free', 'premium_lite', 'premium', 'premium_plus']),
  auth0_user_id: z.string(), // Can be "auto-generate" to trigger generation
  subscription_status: z.enum(['active', 'cancelled', 'trialing', 'past_due']).default('active'),
  password: z.string().optional(), // For test user login
});

/**
 * Linked item (Plaid connection) setup
 */
export const LinkedItemSetupSchema = z.object({
  id: z.string().optional(),
  institution: z.string(),
  institution_id: z.string().optional(),
  status: z.enum(['active', 'error', 'pending']).default('active'),
  cursor: z.string().nullable().optional(),
  item_id: z.string().optional(), // Plaid item ID
});

/**
 * Account setup configuration
 */
export const AccountSetupSchema = z.object({
  id: z.string().optional(),
  linked_item_id: z.string(),
  name: z.string(),
  type: z.enum(['depository', 'credit', 'loan', 'investment']),
  subtype: z.string(),
  current_balance: z.number(),
  available_balance: z.number().optional(),
  plaid_account_id: z.string().optional(),
});

/**
 * Transaction setup configuration
 */
export const TransactionSetupSchema = z.object({
  id: z.string().optional(),
  account_id: z.string(),
  external_transaction_id: z.string().optional(),
  amount: z.number(),
  date: z.string(), // ISO date string
  name: z.string(),
  merchant_name: z.string().optional(),
  category: z.string().optional(),
  pending: z.boolean().default(false),
  user_notes: z.string().nullable().optional(),
  template: z.string().optional(), // Template name to use
  count: z.number().optional(), // Number of transactions to generate from template
  date_range: z.string().optional(), // Date range for template generation
});

/**
 * Generate transactions configuration (bulk transaction generation)
 */
export const GenerateTransactionsSchema = z.object({
  template: z.string(), // Template name from transaction templates
  count: z.number().optional(), // Number of transactions to generate
  date_range: z.string().optional(), // Date range for generation
  amount_range: z.string().optional(), // Amount range for generation
});

/**
 * Complete setup configuration
 */
export const SetupSchema = z.object({
  user: UserSetupSchema.optional(),
  linked_items: z.array(LinkedItemSetupSchema).optional(),
  accounts: z.array(AccountSetupSchema).optional(),
  seed_transactions: z.array(TransactionSetupSchema).optional(),
  existing_transactions: z.array(TransactionSetupSchema).optional(), // Transactions that already exist (for update scenarios)
  generate_transactions: GenerateTransactionsSchema.optional(), // Bulk transaction generation from templates
});

// ============================================================================
// Step Schemas
// ============================================================================

/**
 * Login step
 */
export const LoginStepSchema = z.object({
  action: z.literal('login'),
  description: z.string().optional(),
  email: z.string(),
  password: z.string(),
  expect_success: z.boolean().default(true),
});

/**
 * Navigate step
 */
export const NavigateStepSchema = z.object({
  action: z.literal('navigate'),
  description: z.string().optional(),
  path: z.string(),
  wait_for_load: z.boolean().default(true),
});

/**
 * API call step
 */
export const ApiCallStepSchema = z.object({
  action: z.literal('api_call'),
  description: z.string().optional(),
  method: z.enum(['GET', 'POST', 'PUT', 'DELETE', 'PATCH']),
  endpoint: z.string(),
  headers: z.record(z.string()).optional(),
  body: z.unknown().optional(),
  expect_status: z.number().optional(),
});

/**
 * Wait for condition step
 */
export const WaitForStepSchema = z.object({
  action: z.literal('wait_for'),
  description: z.string().optional(),
  condition: z.object({
    type: z.enum(['element_visible', 'element_hidden', 'url', 'timeout']),
    selector: z.string().optional(), // For element conditions
    url: z.string().optional(), // For URL condition
    timeout_ms: z.number().optional(),
  }),
});

/**
 * Click step
 */
export const ClickStepSchema = z.object({
  action: z.literal('click'),
  description: z.string().optional(),
  selector: z.string(),
  wait_after_ms: z.number().optional(),
});

/**
 * Fill input step
 */
export const FillStepSchema = z.object({
  action: z.literal('fill'),
  description: z.string().optional(),
  selector: z.string(),
  value: z.string(),
});

/**
 * UI assertion step (inline)
 */
export const AssertUiStepSchema = z.object({
  action: z.literal('assert_ui'),
  description: z.string().optional(),
  selector: z.string(),
  should_be_visible: z.boolean().optional(),
  should_be_hidden: z.boolean().optional(),
  should_contain: z.string().optional(),
  should_match_regex: z.string().optional(),
});

/**
 * Delay step (wait for duration)
 */
export const DelayStepSchema = z.object({
  action: z.literal('delay'),
  description: z.string().optional(),
  duration: z.number(), // Duration in milliseconds
});

/**
 * Wait for selector step (wait for element)
 */
export const WaitForSelectorStepSchema = z.object({
  action: z.literal('wait_for_selector'),
  description: z.string().optional(),
  selector: z.string(), // CSS selector to wait for
  timeout_ms: z.number().optional(), // Timeout in milliseconds
});

/**
 * Union of all step types
 */
export const StepSchema = z.discriminatedUnion('action', [
  LoginStepSchema,
  NavigateStepSchema,
  ApiCallStepSchema,
  WaitForStepSchema,
  ClickStepSchema,
  FillStepSchema,
  AssertUiStepSchema,
  DelayStepSchema,
  WaitForSelectorStepSchema,
]);

// ============================================================================
// Assertion Schemas
// ============================================================================

/**
 * Database assertion
 */
export const DatabaseAssertionSchema = z.object({
  description: z.string(),
  query: z.string(), // SQL query
  expected: z.record(z.unknown()).optional(), // Expected exact match
  expected_min: z.record(z.number()).optional(), // Minimum values
  expected_max: z.record(z.number()).optional(), // Maximum values
  expected_not_null: z.array(z.string()).optional(), // Fields that should not be null
  expected_greater_than: z.record(z.number()).optional(),
  expected_less_than: z.record(z.number()).optional(),
});

/**
 * API assertion
 */
export const ApiAssertionSchema = z.object({
  description: z.string(),
  method: z.enum(['GET', 'POST', 'PUT', 'DELETE', 'PATCH']),
  endpoint: z.string(),
  params: z.record(z.unknown()).optional(),
  headers: z.record(z.string()).optional(),
  body: z.unknown().optional(),
  expect_status: z.number(),
  expect_body_contains: z.array(z.string()).optional(),
  assert_response: z.unknown().optional(), // Complex assertions on response body
});

/**
 * UI assertion
 */
export const UiAssertionSchema = z.object({
  description: z.string(),
  selector: z.string(),
  should_be_visible: z.boolean().optional(),
  should_be_hidden: z.boolean().optional(),
  should_contain: z.string().optional(),
  should_match_regex: z.string().optional(),
  should_have_count: z.number().optional(),
});

/**
 * Assertions configuration
 */
export const AssertionsSchema = z.object({
  database: z.array(DatabaseAssertionSchema).optional(),
  api: z.array(ApiAssertionSchema).optional(),
  ui: z.array(UiAssertionSchema).optional(),
});

// ============================================================================
// Cleanup Schema
// ============================================================================

/**
 * Cleanup configuration
 */
export const CleanupSchema = z.object({
  strategy: z.enum(['delete_test_users', 'truncate_all', 'none']).default('delete_test_users'),
  cascade: z.boolean().default(true),
  reset_sequences: z.boolean().default(false),
});

// ============================================================================
// Main Scenario Schema
// ============================================================================

/**
 * Complete scenario schema
 */
export const ScenarioSchema = z.object({
  version: z.literal('1.0'),
  scenario: z.object({
    name: z.string().min(1),
    description: z.string(),
    category: z.enum(['transaction-sync', 'budgets', 'rules', 'user-journeys', 'auth', 'settings']),
    tags: z.array(z.string()).optional(),
    estimated_runtime_seconds: z.number().optional(),
    requires: z.array(z.string()).optional(), // Prerequisites

    setup: SetupSchema,
    steps: z.array(StepSchema),
    assertions: AssertionsSchema,
    cleanup: CleanupSchema.optional(),
  }),
});

// ============================================================================
// TypeScript Types (inferred from Zod schemas)
// ============================================================================

export type UserSetup = z.infer<typeof UserSetupSchema>;
export type LinkedItemSetup = z.infer<typeof LinkedItemSetupSchema>;
export type AccountSetup = z.infer<typeof AccountSetupSchema>;
export type TransactionSetup = z.infer<typeof TransactionSetupSchema>;
export type GenerateTransactions = z.infer<typeof GenerateTransactionsSchema>;
export type Setup = z.infer<typeof SetupSchema>;

export type LoginStep = z.infer<typeof LoginStepSchema>;
export type NavigateStep = z.infer<typeof NavigateStepSchema>;
export type ApiCallStep = z.infer<typeof ApiCallStepSchema>;
export type WaitForStep = z.infer<typeof WaitForStepSchema>;
export type ClickStep = z.infer<typeof ClickStepSchema>;
export type FillStep = z.infer<typeof FillStepSchema>;
export type AssertUiStep = z.infer<typeof AssertUiStepSchema>;
export type DelayStep = z.infer<typeof DelayStepSchema>;
export type WaitForSelectorStep = z.infer<typeof WaitForSelectorStepSchema>;
export type Step = z.infer<typeof StepSchema>;

export type DatabaseAssertion = z.infer<typeof DatabaseAssertionSchema>;
export type ApiAssertion = z.infer<typeof ApiAssertionSchema>;
export type UiAssertion = z.infer<typeof UiAssertionSchema>;
export type Assertions = z.infer<typeof AssertionsSchema>;

export type Cleanup = z.infer<typeof CleanupSchema>;
export type Scenario = z.infer<typeof ScenarioSchema>;

// ============================================================================
// Runtime Context Types
// ============================================================================

/**
 * User entity in context (after creation)
 */
export interface User {
  id: string;
  email: string;
  tier: string;
  auth0_user_id: string;
  subscription_status: string;
  created_at: Date;
}

/**
 * Linked item entity in context
 */
export interface LinkedItem {
  id: string;
  user_id: string;
  institution_name: string;
  institution_id: string;
  item_id: string;
  status: string;
  cursor: string | null;
  created_at: Date;
}

/**
 * Account entity in context
 */
export interface Account {
  id: string;
  user_id: string;
  linked_item_id: string;
  plaid_account_id: string;
  name: string;
  type: string;
  subtype: string;
  current_balance: number;
  available_balance: number | null;
  created_at: Date;
}

/**
 * Transaction entity in context
 */
export interface Transaction {
  id: string;
  account_id: string;
  external_transaction_id: string;
  amount: number;
  date: string;
  name: string;
  merchant_name: string | null;
  category: string | null;
  pending: boolean;
  user_notes: string | null;
  created_at: Date;
}

/**
 * Scenario execution context
 * Contains all created entities and runtime state
 */
export interface ScenarioContext {
  user?: User;
  linked_items: LinkedItem[];
  accounts: Account[];
  transactions: Transaction[];
  auth?: {
    token: string;
  };
  api_responses: unknown[];
  ui_snapshots: unknown[];
  [key: string]: unknown; // Allow dynamic properties
}

/**
 * Assertion result
 */
export interface AssertionResult {
  description: string;
  passed: boolean;
  expected?: unknown;
  actual?: unknown;
  error?: string;
}

/**
 * Scenario execution result
 */
export interface ScenarioResult {
  scenario: string;
  status: 'passed' | 'failed' | 'skipped';
  duration: number;
  assertions: AssertionResult[];
  context: ScenarioContext;
  error?: string;
  failureContext?: unknown;
}

/**
 * Failure context for debugging
 */
export interface FailureContext {
  screenshot: Buffer;
  html: string;
  url: string;
  consoleLogs: Array<{ type: string; text: string; timestamp: number }>;
  networkRequests: Array<{ method: string; url: string; timestamp: number }>;
  databaseSnapshot: unknown;
  contextVariables: string;
  error: {
    message: string;
    stack?: string;
    name: string;
  };
  timestamp: string;
}
