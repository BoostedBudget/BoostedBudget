/**
 * SummaryCard Component Tests
 * Issue #643: Admin dashboard landing page
 */

import { describe, it, expect } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { Users } from 'lucide-react'
import { SummaryCard } from './SummaryCard'

describe('SummaryCard', () => {
  it('should render title and value', () => {
    render(<SummaryCard title="Total Users" value={150} icon={<Users />} />)

    expect(screen.getByText('Total Users')).toBeInTheDocument()
    expect(screen.getByText('150')).toBeInTheDocument()
  })

  it('should render string value', () => {
    render(<SummaryCard title="Status" value="Healthy" icon={<Users />} />)

    expect(screen.getByText('Healthy')).toBeInTheDocument()
  })

  it('should render icon', () => {
    render(
      <SummaryCard
        title="Users"
        value={100}
        icon={<Users data-testid="card-icon" />}
      />
    )

    expect(screen.getByTestId('card-icon')).toBeInTheDocument()
  })

  it('should render upward trend indicator', () => {
    render(
      <SummaryCard
        title="Users"
        value={100}
        icon={<Users />}
        trend={{ value: 12, direction: 'up' }}
      />
    )

    expect(screen.getByText(/\+12/)).toBeInTheDocument()
  })

  it('should render downward trend indicator', () => {
    render(
      <SummaryCard
        title="Users"
        value={100}
        icon={<Users />}
        trend={{ value: 5, direction: 'down' }}
      />
    )

    expect(screen.getByText(/-5/)).toBeInTheDocument()
  })

  it('should apply success status styling', () => {
    render(
      <SummaryCard title="Status" value="OK" icon={<Users />} status="success" />
    )

    const card = document.querySelector('.summary-card')
    expect(card).toHaveClass('summary-card--success')
  })

  it('should apply warning status styling', () => {
    render(
      <SummaryCard
        title="Status"
        value="Warning"
        icon={<Users />}
        status="warning"
      />
    )

    const card = document.querySelector('.summary-card')
    expect(card).toHaveClass('summary-card--warning')
  })

  it('should apply error status styling', () => {
    render(
      <SummaryCard title="Status" value="Error" icon={<Users />} status="error" />
    )

    const card = document.querySelector('.summary-card')
    expect(card).toHaveClass('summary-card--error')
  })
})
