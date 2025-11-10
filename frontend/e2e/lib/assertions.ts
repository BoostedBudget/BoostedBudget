/**
 * Assertion Runner
 *
 * Executes and validates assertions for scenarios
 * Supports database, API, and UI assertions
 * Provides detailed error messages for failures
 *
 * Issue #331: E2E Test Scenarios Framework
 */

import { Page } from '@playwright/test';
import type { Pool } from 'pg';
import type {
  DatabaseAssertion,
  ApiAssertion,
  UiAssertion,
  Assertions,
  AssertionResult,
  ScenarioContext,
} from './types';
import { ApiClient } from './api-client';
import { ensureUUID } from './uuid-utils';

/**
 * Assertion runner class
 * Executes different types of assertions and returns results
 */
export class AssertionRunner {
  private apiClient: ApiClient;

  constructor(apiClient: ApiClient) {
    this.apiClient = apiClient;
  }

  /**
   * Run all assertions for a scenario
   */
  async run(
    assertions: Assertions,
    context: ScenarioContext,
    page: Page,
    dbPool: Pool
  ): Promise<AssertionResult[]> {
    const results: AssertionResult[] = [];

    // Run database assertions
    if (assertions.database) {
      for (const assertion of assertions.database) {
        const result = await this.runDatabaseAssertion(assertion, context, dbPool);
        results.push(result);
      }
    }

    // Run API assertions
    if (assertions.api) {
      for (const assertion of assertions.api) {
        const result = await this.runApiAssertion(assertion, context);
        results.push(result);
      }
    }

    // Run UI assertions
    if (assertions.ui) {
      for (const assertion of assertions.ui) {
        const result = await this.runUiAssertion(assertion, page);
        results.push(result);
      }
    }

    return results;
  }

  /**
   * Run a database assertion
   */
  async runDatabaseAssertion(
    assertion: DatabaseAssertion,
    context: ScenarioContext,
    dbPool: Pool
  ): Promise<AssertionResult> {
    try {
      // Resolve variables in query
      const resolvedQuery = this.resolveVariables(assertion.query, context);

      // Execute query
      const result = await dbPool.query(resolvedQuery);

      if (result.rows.length === 0) {
        return {
          description: assertion.description,
          passed: false,
          expected: assertion.expected || 'at least one row',
          actual: 'no rows returned',
          error: 'Query returned no rows',
        };
      }

      const actual = result.rows[0] as Record<string, unknown>;

      // Check exact match
      if (assertion.expected) {
        const match = this.matchesExpected(actual, assertion.expected);
        if (!match.passed) {
          return {
            description: assertion.description,
            passed: false,
            expected: assertion.expected,
            actual,
            error: match.error,
          };
        }
      }

      // Check minimum values
      if (assertion.expected_min) {
        const match = this.matchesMin(actual, assertion.expected_min as Record<string, number>);
        if (!match.passed) {
          return {
            description: assertion.description,
            passed: false,
            expected: `>= ${JSON.stringify(assertion.expected_min)}`,
            actual,
            error: match.error,
          };
        }
      }

      // Check maximum values
      if (assertion.expected_max) {
        const match = this.matchesMax(actual, assertion.expected_max as Record<string, number>);
        if (!match.passed) {
          return {
            description: assertion.description,
            passed: false,
            expected: `<= ${JSON.stringify(assertion.expected_max)}`,
            actual,
            error: match.error,
          };
        }
      }

      // Check not null
      if (assertion.expected_not_null) {
        const match = this.matchesNotNull(actual, assertion.expected_not_null);
        if (!match.passed) {
          return {
            description: assertion.description,
            passed: false,
            expected: 'non-null values',
            actual,
            error: match.error,
          };
        }
      }

      // Check greater than
      if (assertion.expected_greater_than) {
        const match = this.matchesGreaterThan(actual, assertion.expected_greater_than as Record<string, number>);
        if (!match.passed) {
          return {
            description: assertion.description,
            passed: false,
            expected: `> ${JSON.stringify(assertion.expected_greater_than)}`,
            actual,
            error: match.error,
          };
        }
      }

      // Check less than
      if (assertion.expected_less_than) {
        const match = this.matchesLessThan(actual, assertion.expected_less_than as Record<string, number>);
        if (!match.passed) {
          return {
            description: assertion.description,
            passed: false,
            expected: `< ${JSON.stringify(assertion.expected_less_than)}`,
            actual,
            error: match.error,
          };
        }
      }

      return {
        description: assertion.description,
        passed: true,
        expected: assertion.expected,
        actual,
      };
    } catch (error: unknown) {
      const errorMessage = error instanceof Error ? error.message : 'Unknown error';
      return {
        description: assertion.description,
        passed: false,
        error: `Database assertion failed: ${errorMessage}`,
      };
    }
  }

  /**
   * Run an API assertion
   */
  async runApiAssertion(
    assertion: ApiAssertion,
    context: ScenarioContext
  ): Promise<AssertionResult> {
    try {
      // Resolve variables in endpoint and body
      const endpoint = this.resolveVariables(assertion.endpoint, context);
      const params = assertion.params
        ? this.resolveVariablesInObject(assertion.params, context) as Record<string, unknown>
        : undefined;
      const body = assertion.body
        ? this.resolveVariablesInObject(assertion.body, context)
        : undefined;

      // Make API request
      const response = await this.apiClient.request({
        method: assertion.method,
        url: endpoint,
        params,
        body,
        headers: assertion.headers as Record<string, string>,
      });

      // Check status code
      if (response.status !== assertion.expect_status) {
        return {
          description: assertion.description,
          passed: false,
          expected: `status ${assertion.expect_status}`,
          actual: `status ${response.status}`,
          error: `Expected status ${assertion.expect_status}, got ${response.status}`,
        };
      }

      // Check body contains expected fields
      if (assertion.expect_body_contains) {
        const responseBody = JSON.stringify(response.data);
        for (const field of assertion.expect_body_contains) {
          if (!responseBody.includes(field)) {
            return {
              description: assertion.description,
              passed: false,
              expected: `body containing "${field}"`,
              actual: responseBody,
              error: `Response body missing expected field: ${field}`,
            };
          }
        }
      }

      // TODO: Complex response assertions (assert_response)
      // This would evaluate expressions like "every_item.date >= date.subtract(30, 'days')"

      return {
        description: assertion.description,
        passed: true,
        expected: `status ${assertion.expect_status}`,
        actual: response.data,
      };
    } catch (error: unknown) {
      const errorMessage = error instanceof Error ? error.message : 'Unknown error';
      return {
        description: assertion.description,
        passed: false,
        error: `API assertion failed: ${errorMessage}`,
      };
    }
  }

  /**
   * Run a UI assertion
   */
  async runUiAssertion(
    assertion: UiAssertion,
    page: Page
  ): Promise<AssertionResult> {
    try {
      const locator = page.locator(assertion.selector);

      // Check visibility
      if (assertion.should_be_visible) {
        const isVisible = await locator.isVisible();
        if (!isVisible) {
          return {
            description: assertion.description,
            passed: false,
            expected: 'element visible',
            actual: 'element not visible',
            error: `Element not visible: ${assertion.selector}`,
          };
        }
      }

      // Check hidden
      if (assertion.should_be_hidden) {
        const isVisible = await locator.isVisible();
        if (isVisible) {
          return {
            description: assertion.description,
            passed: false,
            expected: 'element hidden',
            actual: 'element visible',
            error: `Element is visible but should be hidden: ${assertion.selector}`,
          };
        }
      }

      // Check text content
      if (assertion.should_contain) {
        const text = await locator.textContent();
        if (!text || !text.includes(assertion.should_contain)) {
          return {
            description: assertion.description,
            passed: false,
            expected: `text containing "${assertion.should_contain}"`,
            actual: text || 'null',
            error: `Element text does not contain "${assertion.should_contain}"`,
          };
        }
      }

      // Check regex match
      if (assertion.should_match_regex) {
        const text = await locator.textContent();
        const regex = new RegExp(assertion.should_match_regex);
        if (!text || !regex.test(text)) {
          return {
            description: assertion.description,
            passed: false,
            expected: `text matching /${assertion.should_match_regex}/`,
            actual: text || 'null',
            error: `Element text does not match regex: ${assertion.should_match_regex}`,
          };
        }
      }

      // Check count
      if (assertion.should_have_count !== undefined) {
        const count = await locator.count();
        if (count !== assertion.should_have_count) {
          return {
            description: assertion.description,
            passed: false,
            expected: `${assertion.should_have_count} elements`,
            actual: `${count} elements`,
            error: `Expected ${assertion.should_have_count} elements, found ${count}`,
          };
        }
      }

      return {
        description: assertion.description,
        passed: true,
      };
    } catch (error: unknown) {
      const errorMessage = error instanceof Error ? error.message : 'Unknown error';
      return {
        description: assertion.description,
        passed: false,
        error: `UI assertion failed: ${errorMessage}`,
      };
    }
  }

  /**
   * Check if actual matches expected (exact match)
   */
  private matchesExpected(
    actual: Record<string, unknown>,
    expected: Record<string, unknown>
  ): { passed: boolean; error?: string } {
    for (const [key, value] of Object.entries(expected)) {
      if (actual[key] !== value) {
        return {
          passed: false,
          error: `Field "${key}": expected ${value}, got ${actual[key]}`,
        };
      }
    }
    return { passed: true };
  }

  /**
   * Check if actual meets minimum values
   */
  private matchesMin(
    actual: Record<string, unknown>,
    min: Record<string, number>
  ): { passed: boolean; error?: string } {
    for (const [key, minValue] of Object.entries(min)) {
      const actualValue = actual[key];
      if (typeof actualValue === 'number' && actualValue < minValue) {
        return {
          passed: false,
          error: `Field "${key}": expected >= ${minValue}, got ${actualValue}`,
        };
      }
    }
    return { passed: true };
  }

  /**
   * Check if actual meets maximum values
   */
  private matchesMax(
    actual: Record<string, unknown>,
    max: Record<string, number>
  ): { passed: boolean; error?: string } {
    for (const [key, maxValue] of Object.entries(max)) {
      const actualValue = actual[key];
      if (typeof actualValue === 'number' && actualValue > maxValue) {
        return {
          passed: false,
          error: `Field "${key}": expected <= ${maxValue}, got ${actualValue}`,
        };
      }
    }
    return { passed: true };
  }

  /**
   * Check if fields are not null
   */
  private matchesNotNull(
    actual: Record<string, unknown>,
    fields: string[]
  ): { passed: boolean; error?: string } {
    for (const field of fields) {
      if (actual[field] === null || actual[field] === undefined) {
        return {
          passed: false,
          error: `Field "${field}" is null or undefined`,
        };
      }
    }
    return { passed: true };
  }

  /**
   * Check if actual is greater than expected
   */
  private matchesGreaterThan(
    actual: Record<string, unknown>,
    greaterThan: Record<string, number>
  ): { passed: boolean; error?: string } {
    for (const [key, value] of Object.entries(greaterThan)) {
      const actualValue = actual[key];
      if (typeof actualValue === 'number' && actualValue <= value) {
        return {
          passed: false,
          error: `Field "${key}": expected > ${value}, got ${actualValue}`,
        };
      }
    }
    return { passed: true };
  }

  /**
   * Check if actual is less than expected
   */
  private matchesLessThan(
    actual: Record<string, unknown>,
    lessThan: Record<string, number>
  ): { passed: boolean; error?: string } {
    for (const [key, value] of Object.entries(lessThan)) {
      const actualValue = actual[key];
      if (typeof actualValue === 'number' && actualValue >= value) {
        return {
          passed: false,
          error: `Field "${key}": expected < ${value}, got ${actualValue}`,
        };
      }
    }
    return { passed: true };
  }

  /**
   * Resolve variables in a string (e.g., "{{ user.id }}")
   * Also converts string IDs to UUIDs in SQL queries for PostgreSQL UUID column compatibility
   * Issue #331: Fix UUID validation errors in assertion queries
   */
  private resolveVariables(value: string, context: ScenarioContext): string {
    // Step 1: Replace {{ context.variables }} as before
    let resolved = value.replace(/\{\{\s*([^}]+)\s*\}\}/g, (match, path) => {
      const keys = path.trim().split('.');
      let result: unknown = context;

      for (const key of keys) {
        // Handle array notation: accounts[0]
        const arrayMatch = key.match(/^(\w+)\[(\d+)\]$/);
        if (arrayMatch) {
          const arrayName = arrayMatch[1];
          const index = parseInt(arrayMatch[2], 10);
          if (result && typeof result === 'object' && arrayName in result) {
            const arrayValue = (result as Record<string, unknown>)[arrayName];
            if (Array.isArray(arrayValue)) {
              result = arrayValue[index];
            }
          }
        } else {
          if (result && typeof result === 'object' && key in result) {
            result = (result as Record<string, unknown>)[key];
          }
        }
      }

      return result !== undefined && result !== null ? String(result) : match;
    });

    // Step 2: Convert string IDs to UUIDs in SQL WHERE clauses
    // Pattern: WHERE table.id = 'string-id-name' or WHERE id = 'string-id-name'
    // This ensures human-readable IDs from YAML scenarios work with PostgreSQL UUID columns
    resolved = resolved.replace(
      /(\b(?:id|account_id|linked_item_id|user_id|item_id)\s*=\s*')([^']+)(')/gi,
      (match, prefix, id, suffix) => {
        // Convert to UUID if not already in UUID format
        const convertedId = ensureUUID(id);
        return prefix + convertedId + suffix;
      }
    );

    return resolved;
  }

  /**
   * Resolve variables in an object
   */
  private resolveVariablesInObject(obj: unknown, context: ScenarioContext): unknown {
    if (typeof obj === 'string') {
      return this.resolveVariables(obj, context);
    }

    if (Array.isArray(obj)) {
      return obj.map((item) => this.resolveVariablesInObject(item, context));
    }

    if (typeof obj === 'object' && obj !== null) {
      const resolved: Record<string, unknown> = {};
      for (const [key, value] of Object.entries(obj)) {
        resolved[key] = this.resolveVariablesInObject(value, context);
      }
      return resolved;
    }

    return obj;
  }
}
