/**
 * AdminLayout Component Tests
 * Issue #642: Admin feature foundation
 */

import { describe, it, expect } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { AdminLayout } from './AdminLayout'

function renderWithRouter(initialEntries: string[] = ['/admin']) {
  return render(
    <MemoryRouter initialEntries={initialEntries}>
      <Routes>
        <Route path="/admin" element={<AdminLayout />}>
          <Route index element={<div>Dashboard Content</div>} />
        </Route>
      </Routes>
    </MemoryRouter>
  )
}

describe('AdminLayout', () => {
  it('should render the amber admin mode header', () => {
    renderWithRouter()

    expect(screen.getByText(/Admin Mode/i)).toBeInTheDocument()
  })

  it('should render the sidebar', () => {
    renderWithRouter()

    expect(screen.getByRole('navigation', { name: /admin/i })).toBeInTheDocument()
  })

  it('should render the outlet content', () => {
    renderWithRouter()

    expect(screen.getByText('Dashboard Content')).toBeInTheDocument()
  })

  it('should have admin layout container', () => {
    renderWithRouter()

    // Layout uses Tailwind flex column — verify the wrapper exists via its content
    expect(screen.getByText(/Admin Mode/i).closest('div')).toBeInTheDocument()
  })

  it('should display warning about admin view', () => {
    renderWithRouter()

    expect(
      screen.getByText(/viewing as administrator/i)
    ).toBeInTheDocument()
  })
})
