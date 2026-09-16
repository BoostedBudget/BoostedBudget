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
})
