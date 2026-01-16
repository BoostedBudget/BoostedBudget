/**
 * AdminDashboardPage Component Tests
 * Issue #643: Admin dashboard landing page
 */

import { describe, it, expect, beforeEach, afterEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { AdminDashboardPage } from './AdminDashboardPage'

function renderWithRouter(ui: React.ReactElement) {
  return render(<MemoryRouter>{ui}</MemoryRouter>)
}

describe('AdminDashboardPage', () => {
  const originalLocation = window.location

  beforeEach(() => {
    Object.defineProperty(window, 'location', {
      value: { hostname: 'localhost' },
      writable: true,
    })
  })

  afterEach(() => {
    Object.defineProperty(window, 'location', {
      value: originalLocation,
      writable: true,
    })
  })

  it('should render the page title', () => {
    renderWithRouter(<AdminDashboardPage />)

    expect(screen.getByText('Admin Dashboard')).toBeInTheDocument()
  })

  it('should render environment badge', () => {
    renderWithRouter(<AdminDashboardPage />)

    expect(screen.getByText('DEVELOPMENT')).toBeInTheDocument()
  })

  it('should render summary cards', () => {
    renderWithRouter(<AdminDashboardPage />)

    expect(screen.getByText('Total Users')).toBeInTheDocument()
    expect(screen.getByText('Active Subscriptions')).toBeInTheDocument()
    expect(screen.getByText('System Status')).toBeInTheDocument()
    expect(screen.getByText('Failed Jobs')).toBeInTheDocument()
  })

  it('should render quick actions section', () => {
    renderWithRouter(<AdminDashboardPage />)

    expect(screen.getByText('Quick Actions')).toBeInTheDocument()
  })

  it('should render quick action links', () => {
    renderWithRouter(<AdminDashboardPage />)

    expect(screen.getByText('Manage Users')).toBeInTheDocument()
    expect(screen.getByText('View Subscriptions')).toBeInTheDocument()
    expect(screen.getByText('System Health')).toBeInTheDocument()
  })

  it('should show placeholder values for stats', () => {
    renderWithRouter(<AdminDashboardPage />)

    // Placeholder value for stats that aren't yet implemented
    const placeholders = screen.getAllByText('--')
    expect(placeholders.length).toBeGreaterThan(0)
  })
})
