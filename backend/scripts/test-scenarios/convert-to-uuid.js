#!/usr/bin/env node
/**
 * UUID Conversion Script for Backend Test Scenarios
 *
 * Converts all string IDs in YAML scenario files to UUID format
 * Uses deterministic UUIDv5 generation (same as frontend ensureUUID)
 *
 * Issue #331: E2E Test Scenarios Framework - Phase 3
 */

const fs = require('fs');
const path = require('path');
const { v5: uuidv5 } = require('uuid');

// Same namespace as frontend uuid-utils.ts (DNS namespace)
const UUID_NAMESPACE = '6ba7b810-9dad-11d1-80b4-00c04fd430c8';

/**
 * Convert string ID to UUID format (deterministic)
 * Matches frontend ensureUUID() behavior
 */
function ensureUUID(id) {
  if (!id) {
    throw new Error('ID cannot be empty');
  }

  // If already UUID format, return as-is
  if (/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(id)) {
    return id;
  }

  // Generate deterministic UUID from string using UUIDv5
  return uuidv5(id, UUID_NAMESPACE);
}

/**
 * Convert all IDs in YAML content to UUID format
 * Preserves YAML structure and comments
 */
function convertYAMLToUUID(content) {
  // Track conversions for logging
  const conversions = new Map();

  // Replace all ID fields with UUID equivalents
  // Pattern: id: "string-value" or id: 'string-value'
  let result = content.replace(
    /(\s+(?:id|user_id|linked_item_id|account_id|item_id):\s+)["']([^"']+)["']/g,
    (match, prefix, idValue) => {
      // Skip if already UUID format
      if (/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(idValue)) {
        return match;
      }

      // Convert to UUID
      const uuid = ensureUUID(idValue);

      // Track conversion
      if (!conversions.has(idValue)) {
        conversions.set(idValue, uuid);
      }

      return `${prefix}"${uuid}"`;
    }
  );

  return { result, conversions };
}

/**
 * Process a single scenario file
 */
function processFile(filePath) {
  console.log(`\nProcessing: ${path.basename(filePath)}`);

  // Read original content
  const originalContent = fs.readFileSync(filePath, 'utf8');

  // Convert to UUID
  const { result: convertedContent, conversions } = convertYAMLToUUID(originalContent);

  // Write back
  fs.writeFileSync(filePath, convertedContent, 'utf8');

  // Log conversions
  if (conversions.size > 0) {
    console.log(`  Converted ${conversions.size} unique IDs:`);
    for (const [original, uuid] of conversions) {
      console.log(`    ${original} → ${uuid}`);
    }
  } else {
    console.log(`  No IDs to convert (already in UUID format)`);
  }

  return conversions.size;
}

/**
 * Main execution
 */
function main() {
  const scenariosDir = path.join(__dirname, 'scenarios');

  console.log('Backend Test Scenario UUID Conversion');
  console.log('=====================================');
  console.log(`Directory: ${scenariosDir}\n`);

  // Get all YAML files
  const files = fs.readdirSync(scenariosDir)
    .filter(f => f.endsWith('.yaml') || f.endsWith('.yml'))
    .sort()
    .map(f => path.join(scenariosDir, f));

  console.log(`Found ${files.length} scenario files\n`);

  let totalConversions = 0;
  let filesProcessed = 0;

  // Process each file
  for (const filePath of files) {
    try {
      const conversions = processFile(filePath);
      totalConversions += conversions;
      filesProcessed++;
    } catch (error) {
      console.error(`  ERROR: ${error.message}`);
    }
  }

  console.log('\n=====================================');
  console.log(`Processed: ${filesProcessed}/${files.length} files`);
  console.log(`Total unique IDs converted: ${totalConversions}`);
  console.log('=====================================\n');
}

// Run
main();
