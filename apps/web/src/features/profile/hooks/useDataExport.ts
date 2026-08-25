/**
 * useDataExport — requests the user's GDPR Article 20 export and hands it to them as a file.
 *
 * Issue #1587. The privacy policy promises the Right to Data Portability twice; until this hook
 * there was no route, no client method and no UI behind either promise.
 *
 * ⚠ THE IMPERATIVE `getDataExport` IS USED, NOT `useGetDataExport`. The generated query hook fetches
 * on mount, and an export is a materialisation of the user's entire financial history — it must
 * happen when the user asks for it, once, and not every time this settings page renders.
 */
import { useCallback, useState } from 'react'
import { getDataExport } from '@boostedbudget/api-client/endpoints/privacy/privacy'
import { useTrackEvent } from '../../../shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '../../../shared/analytics'

export interface DataExportSummary {
  /** Total rows the export contained, across every section. */
  rowCount: number
  /** Number of sections (source tables) in the document. */
  sectionCount: number
  /** The filename the browser was given. */
  filename: string
}

export interface UseDataExportResult {
  requestExport: () => Promise<void>
  isExporting: boolean
  error: string | null
  lastExport: DataExportSummary | null
}

/**
 * exportFilename mirrors the Content-Disposition the API sets.
 *
 * The header itself is not readable here — the generated client returns the parsed body, not the
 * response — so the name is rebuilt from the export's own generated_at rather than from "now". A
 * name derived from the document is the same name on a retry and after a clock skew; a name derived
 * from the browser clock is not.
 */
export function exportFilename(generatedAt: string): string {
  const date = new Date(generatedAt)
  const stamp = Number.isNaN(date.getTime())
    ? 'unknown-date'
    : date.toISOString().slice(0, 10)
  return `boostedbudget-export-${stamp}.json`
}

export function useDataExport(): UseDataExportResult {
  const [isExporting, setIsExporting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [lastExport, setLastExport] = useState<DataExportSummary | null>(null)
  const trackEvent = useTrackEvent()

  const requestExport = useCallback(async () => {
    setIsExporting(true)
    setError(null)
    try {
      const exportDocument = await getDataExport()
      const filename = exportFilename(exportDocument.generated_at)

      // Two spaces, because a portability export is something a person may well open in a text
      // editor; the machine-readability GDPR asks for is not harmed by being legible.
      const blob = new Blob([JSON.stringify(exportDocument, null, 2)], {
        type: 'application/json',
      })
      const url = URL.createObjectURL(blob)
      try {
        const anchor = document.createElement('a')
        anchor.href = url
        anchor.download = filename
        // Not appended to the document: a detached anchor still downloads, and appending it would
        // briefly put a link carrying the filename into the DOM, where session replay records it.
        anchor.click()
      } finally {
        // Always revoked, including when click() throws — a leaked object URL pins the whole export
        // in memory for the lifetime of the tab.
        URL.revokeObjectURL(url)
      }

      const summary: DataExportSummary = {
        rowCount: exportDocument.row_count,
        sectionCount: Object.keys(exportDocument.sections ?? {}).length,
        filename,
      }
      setLastExport(summary)
      // ⚠ NO FINANCIAL VALUES AND NO TABLE NAMES IN THE EVENT — only counts. row_count is what tells
      // us when the synchronous export has outgrown a request/response cycle, and it says nothing
      // about what the user owns.
      trackEvent(ANALYTICS_EVENTS.DATA_EXPORT_DOWNLOADED, {
        row_count: summary.rowCount,
        section_count: summary.sectionCount,
      })
    } catch {
      setError('We could not build your export just now. Please try again, or email privacy@boostedbudget.com.')
      trackEvent(ANALYTICS_EVENTS.DATA_EXPORT_FAILED)
    } finally {
      setIsExporting(false)
    }
  }, [trackEvent])

  return { requestExport, isExporting, error, lastExport }
}
