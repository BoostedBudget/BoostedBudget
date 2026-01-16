/**
 * QuickActionLink Component Tests
 * Issue #643: Admin dashboard landing page
 */

import { describe, it, expect } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { Users } from 'lucide-react'
import { QuickActionLink } from './QuickActionLink'

function renderWithRouter(ui: React.ReactElement) {
  return render(<MemoryRouter>{ui}</MemoryRouter>)
}

describe('QuickActionLink', () => {
  it('should render label and description', () => {
    renderWithRouter(
      <QuickActionLink
        to="/admin/users"
        label="Manage Users"
        description="View and manage user accounts"
        icon={<Users />}
      />
    )

    expect(screen.getByText('Manage Users')).toBeInTheDocument()
    expect(screen.getByText('View and manage user accounts')).toBeInTheDocument()
  })

  it('should render as a link with correct href', () => {
    renderWithRouter(
      <QuickActionLink
        to="/admin/users"
        label="Users"
        description="Manage users"
        icon={<Users />}
      />
    )

    const link = screen.getByRole('link')
    expect(link).toHaveAttribute('href', '/admin/users')
  })

  it('should render icon', () => {
    renderWithRouter(
      <QuickActionLink
        to="/admin/users"
        label="Users"
        description="Manage users"
        icon={<Users data-testid="link-icon" />}
      />
    )

    expect(screen.getByTestId('link-icon')).toBeInTheDocument()
  })
})
