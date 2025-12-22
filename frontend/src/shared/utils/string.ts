/**
 * String utility functions for BoostedBudget
 */

/**
 * Abbreviates a UUID for display purposes
 *
 * @param uuid - The full UUID string to abbreviate
 * @param prefixLen - Number of characters to keep at the start (default: 8)
 * @param suffixLen - Number of characters to keep at the end (default: 4)
 * @returns Abbreviated UUID like "ad8d6c49...d77a" or original if too short
 *
 * @example
 * abbreviateUUID('ad8d6c49-4f4a-4756-945a-603fea50d77a')
 * // Returns: 'ad8d6c49...d77a'
 */
export function abbreviateUUID(
  uuid: string,
  prefixLen = 8,
  suffixLen = 4
): string {
  // Handle null, undefined, or empty string
  if (!uuid) {
    return ''
  }

  // If string is too short to abbreviate meaningfully, return as-is
  // Minimum length needed: prefixLen + "..." (3 chars) + suffixLen
  const minLength = prefixLen + 3 + suffixLen
  if (uuid.length < minLength) {
    return uuid
  }

  return `${uuid.slice(0, prefixLen)}...${uuid.slice(-suffixLen)}`
}
