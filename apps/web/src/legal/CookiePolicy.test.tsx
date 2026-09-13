import { describe, expect, it } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { CookiePolicy } from './CookiePolicy'

describe('CookiePolicy analytics disclosure', () => {
  it('documents pseudonymous fail-closed analytics, disabled replay, and browser-only DNT', () => {
    render(<MemoryRouter><CookiePolicy /></MemoryRouter>)

    expect(screen.getByText(/pseudonymous analytics data/i)).toBeInTheDocument()
    expect(screen.getByText(/no posthog analytics persistence or analytics event capture occurs before/i)).toBeInTheDocument()
    expect(screen.getByText(/authenticated support chat is a separate, user-requested service/i)).toBeInTheDocument()
    expect(screen.getByText(/production session replay remains disabled/i)).toBeInTheDocument()
    expect(screen.getByText(/do not track.*browser-only/i)).toBeInTheDocument()
    expect(screen.getByText(/signed-out visitors can reopen Cookie Settings/i)).toBeInTheDocument()
    expect(screen.getByText(/locally persisted PostHog SDK state.*current tab session/i)).toBeInTheDocument()
    expect(screen.queryByText(/remove the consent cookie there to withdraw/i)).not.toBeInTheDocument()
    expect(screen.getByText(/no longer than the current tab session.*cleared sooner/i)).toBeInTheDocument()
    expect(screen.queryByText(/withdraw consent as easily as you gave it/i)).not.toBeInTheDocument()
    expect(screen.queryByText(/error and performance data/i)).not.toBeInTheDocument()
    expect(screen.getByText(/session replay remains disabled/i)).toBeInTheDocument()
    expect(screen.getByText(/if session replay is separately enabled/i)).toBeInTheDocument()
    expect(screen.getByText(/locally persisted PostHog SDK state.*tab session/i)).toBeInTheDocument()
    expect(screen.getByText(/signed-in tabs identify as the same account/i)).toBeInTheDocument()
    expect(screen.getByText(/duplicating a tab may copy sessionStorage/i)).toBeInTheDocument()
    expect(screen.queryByText(/identity and event state do not carry between tabs/i)).not.toBeInTheDocument()
  })
})
