/**
 * AdminSidebar Component Tests
 * Issue #642: Admin feature foundation
 */

import { describe, it, expect } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { AdminSidebar } from './AdminSidebar'
import { ADMIN_SIDEBAR_ITEMS } from '../constants'

function renderWithRouter(ui: React.ReactElement, initialEntries: string[] = ['/admin']) {
  return render(<MemoryRouter initialEntries={initialEntries}>{ui}</MemoryRouter>)
}

describe('AdminSidebar', () => {
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

  it('should highlight the active link', () => {
    renderWithRouter(<AdminSidebar />, ['/admin'])

    const dashboardLink = screen.getByRole('link', { name: 'Dashboard' })
    expect(dashboardLink).toHaveClass('bg-amber-50')
  })

  it('should have navigation landmark', () => {
    renderWithRouter(<AdminSidebar />)

    expect(screen.getByRole('navigation')).toBeInTheDocument()
  })
})
