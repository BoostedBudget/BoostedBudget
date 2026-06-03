import { describe, it, expect, beforeEach, mock } from 'bun:test'
import React from 'react'
import { render, screen, fireEvent } from '@testing-library/react'
import { ResumeButton } from './ResumeButton'

const mockWindowOpen = mock(() => null)

beforeEach(() => {
  mockWindowOpen.mockClear()
  Object.defineProperty(window, 'open', { value: mockWindowOpen, writable: true })
})

describe('ResumeButton', () => {
  it('shows Resume button when portal URL is available', () => {
    render(<ResumeButton billingPortalUrl="https://portal.paddle.com/123" isLoadingPortal={false} />)
    expect(screen.getByRole('button', { name: /Resume/i })).toBeDefined()
  })

  it('opens billing portal in new tab on click', () => {
    render(<ResumeButton billingPortalUrl="https://portal.paddle.com/123" isLoadingPortal={false} />)
    fireEvent.click(screen.getByRole('button', { name: /Resume/i }))
    expect(mockWindowOpen).toHaveBeenCalledWith('https://portal.paddle.com/123', '_blank', 'noopener,noreferrer')
  })

  it('shows loading state when portal is loading', () => {
    render(<ResumeButton billingPortalUrl={undefined} isLoadingPortal={true} />)
    expect(screen.getByRole('button')).toBeDisabled()
  })

  it('shows disabled state when no portal URL and not loading', () => {
    render(<ResumeButton billingPortalUrl={undefined} isLoadingPortal={false} />)
    expect(screen.getByRole('button')).toBeDisabled()
  })
})
