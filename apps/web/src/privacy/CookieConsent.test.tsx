import { beforeEach, describe, expect, it, mock } from 'bun:test'
import { fireEvent, render, screen } from '@testing-library/react'
import Cookies from 'js-cookie'
import { CookieConsent } from './CookieConsent'
import { COOKIE_CONSENT_NAME } from './types'

describe('CookieConsent', () => {
  beforeEach(() => Cookies.remove(COOKIE_CONSENT_NAME))

  it('keeps Cookie Settings accessible and broadcasts an explicit denial after a choice', () => {
    const onConsentChange = mock(() => {})
    render(<CookieConsent onConsentChange={onConsentChange} />)

    fireEvent.click(screen.getByRole('button', { name: /accept all cookies/i }))
    fireEvent.click(screen.getByRole('button', { name: /cookie settings/i }))
    expect(screen.getByLabelText(/analytics cookies/i)).toBeChecked()

    fireEvent.click(screen.getByLabelText(/analytics cookies/i))
    fireEvent.click(screen.getByRole('button', { name: /save cookie preferences/i }))

    expect(onConsentChange).toHaveBeenLastCalledWith(
      expect.objectContaining({ analytics: false }),
      'explicit',
    )
    expect(screen.getByRole('button', { name: /cookie settings/i })).toBeInTheDocument()
  })

  it('states that optional cookies stay off until chosen without implied agreement', () => {
    render(<CookieConsent />)
    expect(screen.getByText(/optional cookies remain off until you choose them/i)).toBeInTheDocument()
    expect(screen.queryByText(/by using BoostedBudget, you agree/i)).not.toBeInTheDocument()
  })

  // Issue #1683: the interaction-capture category is new; a cookie written before it
  // existed must still be honoured (no re-prompt) and normalize to `false`.
  it('applies a stored v2.0 cookie that predates the interaction-capture category without re-asking', () => {
    const onConsentChange = mock(() => {})
    Cookies.set(COOKIE_CONSENT_NAME, JSON.stringify({
      consents: { necessary: true, analytics: true, advertising: false },
      timestamp: '2026-01-01T00:00:00.000Z',
      version: '2.0',
    }))

    render(<CookieConsent onConsentChange={onConsentChange} />)

    expect(screen.getByRole('button', { name: /cookie settings/i })).toBeInTheDocument()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(onConsentChange).toHaveBeenCalledWith(
      expect.objectContaining({ analytics: true, interactionCapture: false }),
      'stored',
    )
  })

  it('renders the reopened detail view with the interaction-capture toggle off for a cookie lacking the key', () => {
    const onConsentChange = mock(() => {})
    Cookies.set(COOKIE_CONSENT_NAME, JSON.stringify({
      consents: { necessary: true, analytics: true, advertising: false },
      timestamp: '2026-01-01T00:00:00.000Z',
      version: '2.0',
    }))

    render(<CookieConsent onConsentChange={onConsentChange} />)
    fireEvent.click(screen.getByRole('button', { name: /cookie settings/i }))

    const toggle = screen.getByLabelText(/session recording/i) as HTMLInputElement
    expect(toggle.checked).toBe(false)

    fireEvent.click(screen.getByRole('button', { name: /save cookie preferences/i }))
    expect(onConsentChange).toHaveBeenLastCalledWith(
      expect.objectContaining({ interactionCapture: false }),
      'explicit',
    )
  })

  it('grants interaction capture from Accept All and labels the button as session recording', () => {
    const onConsentChange = mock(() => {})
    render(<CookieConsent onConsentChange={onConsentChange} />)

    const acceptAll = screen.getByRole('button', { name: /accept all cookies/i })
    expect(acceptAll).toHaveTextContent(/session recording/i)
    fireEvent.click(acceptAll)

    expect(onConsentChange).toHaveBeenLastCalledWith(
      expect.objectContaining({ analytics: true, interactionCapture: true }),
      'explicit',
    )
  })
})
