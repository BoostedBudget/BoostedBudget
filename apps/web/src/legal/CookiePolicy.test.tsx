import { describe, expect, it } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { CookiePolicy } from './CookiePolicy'

describe('CookiePolicy analytics disclosure', () => {
  it('documents pseudonymous fail-closed analytics, disabled replay, and browser-only DNT', () => {
    render(<MemoryRouter><CookiePolicy /></MemoryRouter>)

    expect(screen.getByText(/pseudonymous analytics data/i)).toBeInTheDocument()
    expect(screen.getByText(/no posthog persistence or capture occurs before/i)).toBeInTheDocument()
    expect(screen.getByText(/production session replay is disabled/i)).toBeInTheDocument()
    expect(screen.getByText(/do not track.*browser-only/i)).toBeInTheDocument()
    expect(screen.getByText(/signed-out visitors can reopen Cookie Settings/i)).toBeInTheDocument()
    expect(screen.getByText(/tab-scoped.*sessionStorage/i)).toBeInTheDocument()
    expect(screen.queryByText(/remove the consent cookie there to withdraw/i)).not.toBeInTheDocument()
    expect(screen.getByText(/until the browser tab session ends/i)).toBeInTheDocument()
    expect(screen.queryByText(/withdraw consent as easily as you gave it/i)).not.toBeInTheDocument()
  })
})
