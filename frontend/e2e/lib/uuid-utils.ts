/**
 * UUID Utilities
 *
 * Shared utilities for UUID generation and conversion
 * Used across seeding, assertions, and scenario runner
 *
 * Issue #331: E2E Test Scenarios Framework
 */

import { v4 as uuidv4, v5 as uuidv5 } from 'uuid';

/**
 * Convert string ID to UUID format
 * Accepts either UUID format or string, returns valid UUID
 * Uses deterministic generation (UUIDv5) for string inputs
 *
 * @param id - String ID or UUID
 * @returns Valid UUID string
 */
export function ensureUUID(id: string | undefined): string {
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
