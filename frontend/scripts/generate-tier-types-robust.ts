/**
 * Robust file writing utilities for subscription tier type generation
 *
 * Provides guaranteed file writing with fsync and verification to prevent
 * timing issues on macOS APFS and other filesystems with delayed writes.
 *
 * @module generate-tier-types-robust
 */

import { writeFileSync, openSync, fsyncSync, closeSync, readFileSync, existsSync } from 'fs'

/**
 * Custom error for file write failures
 */
export class FileWriteError extends Error {
  constructor(message: string, public cause?: Error) {
    super(message)
    this.name = 'FileWriteError'
  }
}

/**
 * Options for robust file writing
 */
export interface WriteOptions {
  /** Maximum number of verification retry attempts (default: 10) */
  maxRetries?: number
  /** Pattern that must be present in file content for verification */
  verificationPattern?: string | RegExp
  /** Minimum expected file size in bytes (default: 100) */
  minSize?: number
  /** Maximum expected file size in bytes (default: 100000) */
  maxSize?: number
}

/**
 * Write file with guaranteed disk flush and verification
 *
 * This function addresses timing issues where writeFileSync() completes but
 * the OS hasn't actually flushed the data to physical disk yet, causing
 * subsequent reads to fail (especially on macOS APFS).
 *
 * Three-layer reliability strategy:
 * 1. writeFileSync - writes to kernel buffer
 * 2. fsync - forces kernel buffer flush to physical disk
 * 3. Verification - reads back and validates content with retry
 *
 * @param path - Absolute path to file to write
 * @param content - Content to write to file
 * @param options - Configuration options
 * @throws {FileWriteError} If file cannot be written and verified
 *
 * @example
 * ```typescript
 * await writeFileWithVerification(
 *   '/path/to/file.ts',
 *   'export const FOO = "bar"',
 *   {
 *     verificationPattern: /export const FOO/,
 *     minSize: 10,
 *     maxSize: 1000
 *   }
 * )
 * ```
 */
export async function writeFileWithVerification(
  path: string,
  content: string,
  options: WriteOptions = {}
): Promise<void> {
  const {
    maxRetries = 10,
    verificationPattern = /SUBSCRIPTION_TIERS/,
    minSize = 100,
    maxSize = 100000
  } = options

  // Step 1: Write file synchronously
  // This only guarantees write to kernel buffer, not physical disk
  writeFileSync(path, content, 'utf-8')

  // Step 2: Force kernel to flush buffer to physical disk
  // This is THE KEY FIX for macOS APFS timing issues
  const fd = openSync(path, 'r')
  try {
    fsyncSync(fd)  // Blocks until data is physically on disk
  } finally {
    closeSync(fd)  // Always close file descriptor
  }

  // Step 3: Verify file is readable and valid with exponential backoff retry
  for (let attempt = 1; attempt <= maxRetries; attempt++) {
    try {
      // Try to read the file we just wrote
      const readContent = readFileSync(path, 'utf-8')

      // Validation 1: Size check
      if (readContent.length < minSize) {
        throw new Error(
          `File too small: ${readContent.length} bytes (expected >=${minSize})`
        )
      }
      if (readContent.length > maxSize) {
        throw new Error(
          `File too large: ${readContent.length} bytes (expected <=${maxSize})`
        )
      }

      // Validation 2: Pattern check
      if (verificationPattern instanceof RegExp) {
        if (!verificationPattern.test(readContent)) {
          throw new Error(
            `File content doesn't match pattern: ${verificationPattern}`
          )
        }
      } else {
        if (!readContent.includes(verificationPattern)) {
          throw new Error(
            `File content doesn't include required string: ${verificationPattern}`
          )
        }
      }

      // Validation 3: Content integrity check
      if (readContent !== content) {
        throw new Error(
          `File content doesn't match written content (${readContent.length} vs ${content.length} bytes)`
        )
      }

      // Success! File is written, flushed, and verified
      return

    } catch (error) {
      // If this was the last attempt, give up
      if (attempt === maxRetries) {
        throw new FileWriteError(
          `File verification failed after ${maxRetries} attempts: ${path}`,
          error instanceof Error ? error : undefined
        )
      }

      // Exponential backoff: 10ms, 20ms, 40ms, 80ms, 160ms, ...
      // This gives the filesystem time to sync metadata
      const delayMs = 10 * Math.pow(2, attempt - 1)
      await new Promise(resolve => setTimeout(resolve, delayMs))
    }
  }
}

/**
 * Ensure directory exists and is synced to disk
 *
 * Creates directory with recursive: true and attempts to fsync it.
 * Note: Not all filesystems support directory fsync, so this is best-effort.
 *
 * @param dirPath - Absolute path to directory
 */
export function ensureDirectorySync(dirPath: string): void {
  if (existsSync(dirPath)) {
    return
  }

  // Create directory recursively (like mkdir -p)
  import('fs').then(fs => {
    fs.mkdirSync(dirPath, { recursive: true })

    // Attempt to sync directory metadata
    // This is best-effort as not all platforms support it
    try {
      const fd = openSync(dirPath, 'r')
      try {
        fsyncSync(fd)
      } finally {
        closeSync(fd)
      }
    } catch (error) {
      // Non-fatal: directory fsync not supported on this platform
      // Most platforms don't need this anyway
    }
  })
}

/**
 * Verify file exists and is valid
 *
 * Quick verification without retry logic, useful for CI checks
 *
 * @param path - Path to file to verify
 * @param options - Verification options
 * @returns true if valid, false otherwise
 */
export function verifyFile(
  path: string,
  options: Pick<WriteOptions, 'verificationPattern' | 'minSize' | 'maxSize'> = {}
): boolean {
  const {
    verificationPattern = /SUBSCRIPTION_TIERS/,
    minSize = 100,
    maxSize = 100000
  } = options

  try {
    if (!existsSync(path)) {
      return false
    }

    const content = readFileSync(path, 'utf-8')

    if (content.length < minSize || content.length > maxSize) {
      return false
    }

    if (verificationPattern instanceof RegExp) {
      if (!verificationPattern.test(content)) {
        return false
      }
    } else {
      if (!content.includes(verificationPattern)) {
        return false
      }
    }

    return true
  } catch {
    return false
  }
}
