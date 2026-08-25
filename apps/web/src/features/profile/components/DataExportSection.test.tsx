/**
 * DataExportSection tests — Issue #1587 (GDPR Article 20).
 *
 * The behaviour that matters here is not "a button renders". It is that clicking the button
 * actually hands the user a file, that a failure says so instead of failing silently, and that
 * nothing about the outcome is communicated by colour alone.
 */
import { describe, it, expect, beforeEach, afterEach } from 'bun:test'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { DataExportSection } from './DataExportSection'
import { mockGetDataExport } from '@/test/mocks/privacy-api'

const sampleExport = {
  export_version: 1,
  generated_at: '2024-03-07T12:00:00Z',
  user_id: 'user-123',
  sections: {
    transactions: [{ id: 't1', amount: '12.34' }],
    budgets: [],
  },
  excluded_tables: { audit_logs: 'security audit trail' },
  row_count: 1,
}

// The download is the point of the feature, so it is observed rather than assumed: the anchor the
// hook builds is captured, and its `download` attribute and object URL are asserted.
let createdObjectURLs: string[]
let revokedObjectURLs: string[]
let clickedAnchors: HTMLAnchorElement[]
let originalCreate: typeof URL.createObjectURL
let originalRevoke: typeof URL.revokeObjectURL
let originalCreateElement: typeof document.createElement

beforeEach(() => {
  mockGetDataExport.mockClear()
  mockGetDataExport.mockResolvedValue(sampleExport)

  createdObjectURLs = []
  revokedObjectURLs = []
  clickedAnchors = []

  originalCreate = URL.createObjectURL
  originalRevoke = URL.revokeObjectURL
  originalCreateElement = document.createElement

  URL.createObjectURL = ((): string => {
    const url = `blob:mock/${createdObjectURLs.length}`
    createdObjectURLs.push(url)
    return url
  }) as typeof URL.createObjectURL
  URL.revokeObjectURL = ((url: string): void => {
    revokedObjectURLs.push(url)
  }) as typeof URL.revokeObjectURL

  document.createElement = ((tagName: string, ...rest: unknown[]) => {
    const element = originalCreateElement.call(document, tagName, ...(rest as []))
    if (tagName === 'a') {
      const anchor = element as HTMLAnchorElement
      anchor.click = () => {
        clickedAnchors.push(anchor)
      }
    }
    return element
  }) as typeof document.createElement
})

afterEach(() => {
  URL.createObjectURL = originalCreate
  URL.revokeObjectURL = originalRevoke
  document.createElement = originalCreateElement
})

describe('DataExportSection', () => {
  it('tells the user what the export is and is not, before they click', () => {
    render(<DataExportSection />)

    expect(screen.getByRole('button', { name: /download my data/i })).toBeTruthy()
    // The two exclusion promises the backend actually keeps. Copy that overstated what the export
    // contains would be worse than no copy: the user cannot check it themselves.
    expect(screen.getByText(/other members' accounts and transactions are not included/i)).toBeTruthy()
    expect(screen.getByText(/bank access tokens are never exported/i)).toBeTruthy()
  })

  it('downloads a file named after the export, not after the browser clock', async () => {
    render(<DataExportSection />)
    fireEvent.click(screen.getByRole('button', { name: /download my data/i }))

    await waitFor(() => expect(clickedAnchors.length).toBe(1))
    expect(mockGetDataExport).toHaveBeenCalledTimes(1)
    // ⚠ A DATE THAT IS DELIBERATELY NOT TODAY. The first version of this fixture used the day
    // the test was written, so a mutant that named the file from the browser clock passed here —
    // the assertion was true by coincidence, not by behaviour.
    expect(clickedAnchors[0].download).toBe('boostedbudget-export-2024-03-07.json')
    expect(createdObjectURLs.length).toBe(1)
  })

  it('revokes the object URL, so a full copy of the export is not pinned in the tab', async () => {
    render(<DataExportSection />)
    fireEvent.click(screen.getByRole('button', { name: /download my data/i }))

    await waitFor(() => expect(revokedObjectURLs.length).toBe(1))
    expect(revokedObjectURLs).toEqual(createdObjectURLs)
  })

  it('reports success in words and masks the record count', async () => {
    render(<DataExportSection />)
    fireEvent.click(screen.getByRole('button', { name: /download my data/i }))

    const status = await screen.findByRole('status')
    await waitFor(() => expect(status.textContent).toContain('Downloaded'))
    expect(status.textContent).toContain('boostedbudget-export-2024-03-07.json')
    expect(status.textContent).toContain('records across')

    // Information is never carried by colour alone: the word "Downloaded" is the signal, and the
    // check mark beside it is hidden from assistive technology rather than being the message.
    expect(status.querySelector('[aria-hidden="true"]')).toBeTruthy()

    // A per-user row count is a weak signal about the size of someone's financial life; it is kept
    // out of session replay.
    expect(status.querySelectorAll('[data-ph-mask]').length).toBe(2)
  })

  it('surfaces a failure instead of silently doing nothing', async () => {
    mockGetDataExport.mockRejectedValue(new Error('boom'))
    render(<DataExportSection />)

    fireEvent.click(screen.getByRole('button', { name: /download my data/i }))

    const alert = await screen.findByRole('alert')
    expect(alert.textContent).toContain('could not build your export')
    // The manual fulfilment channel the privacy policy already publishes.
    expect(alert.textContent).toContain('privacy@boostedbudget.com')
    expect(clickedAnchors.length).toBe(0)
  })

  it('disables the button while the export is being built, so one click is one export', async () => {
    let release: (value: typeof sampleExport) => void = () => {}
    mockGetDataExport.mockReturnValue(
      new Promise<typeof sampleExport>((resolve) => {
        release = resolve
      })
    )

    render(<DataExportSection />)
    const button = screen.getByRole('button', { name: /download my data/i })
    fireEvent.click(button)

    await waitFor(() =>
      expect((screen.getByRole('button') as HTMLButtonElement).disabled).toBe(true)
    )
    expect(screen.getByRole('button').textContent).toContain('Preparing your export')

    release(sampleExport)
    await waitFor(() => expect(clickedAnchors.length).toBe(1))
    expect(mockGetDataExport).toHaveBeenCalledTimes(1)
  })
})
