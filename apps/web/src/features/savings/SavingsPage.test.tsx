import { describe, it, expect, beforeEach, mock } from 'bun:test'
import { render, screen, act } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'

const mockTrackEvent = mock(() => {})
mock.module('@/shared/hooks/useTrackEvent', () => ({
  useTrackEvent: () => mockTrackEvent,
}))

const { SavingsPage } = await import('./SavingsPage')

function renderPage() {
  return render(
    <MemoryRouter>
      <SavingsPage />
    </MemoryRouter>
  )
}

describe('SavingsPage', () => {
  beforeEach(() => {
    mockTrackEvent.mockReset()
  })

  it('renders page heading', () => {
    renderPage()
    expect(screen.getByRole('heading', { level: 1 })).toBeDefined()
    expect(screen.getByText('Savings')).toBeDefined()
  })

  it('fires savings_page_viewed on mount', async () => {
    renderPage()
    await act(async () => {})
    expect(mockTrackEvent).toHaveBeenCalledWith('savings_page_viewed')
  })

  it.skip('renders loading state', () => {
    // Will pass in T10 when loading state is implemented
    renderPage()
    expect(screen.getByRole('status')).toBeDefined()
  })

  it.skip('renders error state with retry button', () => {
    // Will pass in T10 when error state is implemented
    renderPage()
    expect(screen.getByRole('button', { name: /retry/i })).toBeDefined()
  })

  it.skip('renders empty state', () => {
    // Will pass in T10 when empty state is implemented
    renderPage()
    expect(screen.getByText(/no savings goals/i)).toBeDefined()
  })
})
