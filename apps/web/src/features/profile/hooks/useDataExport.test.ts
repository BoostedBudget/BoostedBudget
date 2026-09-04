/**
 * exportFilename tests — Issue #1587.
 *
 * The filename is derived from the export document's own generated_at rather than from the browser
 * clock, so that a retry produces the same name and a skewed clock cannot mislabel someone's
 * financial history. The malformed-input branch exists because generated_at arrives over the wire.
 */
import { describe, it, expect } from 'bun:test'
import { exportFilename } from './useDataExport'

describe('exportFilename', () => {
  it('names the file after the day the export was generated', () => {
    expect(exportFilename('2026-08-25T12:00:00Z')).toBe('boostedbudget-export-2026-08-25.json')
  })

  it('uses the UTC day, so a late-evening export is not labelled tomorrow', () => {
    expect(exportFilename('2026-08-25T23:30:00Z')).toBe('boostedbudget-export-2026-08-25.json')
  })

  it('degrades to a readable name rather than "Invalid Date" when the timestamp is unusable', () => {
    // The alternative — letting toISOString() throw — would turn a cosmetic problem into a failed
    // export, after the document has already been fetched.
    expect(exportFilename('not-a-timestamp')).toBe('boostedbudget-export-unknown-date.json')
    expect(exportFilename('')).toBe('boostedbudget-export-unknown-date.json')
  })
})
