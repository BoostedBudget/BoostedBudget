/**
 * AdminSidebar Component Tests
 * Issue #642: Admin feature foundation
 */

import { describe, it, expect } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { AdminSidebar } from './AdminSidebar'
import { ADMIN_SIDEBAR_ITEMS } from '../constants'

function renderWithRouter(ui: React.ReactElement, initialEntries: string[] = ['/admin']) {
  return render(<MemoryRouter initialEntries={initialEntries}>{ui}</MemoryRouter>)
}

describe('AdminSidebar', () => {
  it('links to RevenueCat operations', () => {
    renderWithRouter(<AdminSidebar />)
    expect(screen.getByRole('link', {name: 'RevenueCat'})).toHaveAttribute('href', '/admin/revenuecat')
  })
  it('should render all sidebar navigation items', () => {
    renderWithRouter(<AdminSidebar />)

    for (const item of ADMIN_SIDEBAR_ITEMS) {
      expect(screen.getByText(item.label)).toBeInTheDocument()
    }
  })

  it('should render navigation links with correct hrefs', () => {
    renderWithRouter(<AdminSidebar />)

    for (const item of ADMIN_SIDEBAR_ITEMS) {
      const link = screen.getByRole('link', { name: item.label })
      expect(link).toHaveAttribute('href', item.to)
    }
  })

  it('should render an icon for every sidebar item', () => {
    renderWithRouter(<AdminSidebar />)

    // A nav item whose `icon` is missing from AdminSidebar's iconMap renders a bare label.
    for (const item of ADMIN_SIDEBAR_ITEMS) {
      const link = screen.getByRole('link', { name: item.label })
      expect(link.querySelector('svg')).not.toBeNull()
    }
  })

  it('should highlight the active link', () => {
    renderWithRouter(<AdminSidebar />, ['/admin'])

    const dashboardLink = screen.getByRole('link', { name: 'Dashboard' })
    expect(dashboardLink).toHaveClass('bg-warning/10')
  })

  it('should have navigation landmark', () => {
    renderWithRouter(<AdminSidebar />)

    expect(screen.getByRole('navigation')).toBeInTheDocument()
  })
})
