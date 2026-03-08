/**
 * WidgetEmptyState Tests
 * Issue #834: Dashboard empty states with context-aware CTAs
 */

import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router'

import '@/test/mocks/registry'
import { mockUseDashboardContext } from '@/test/mocks/dashboard-context'

// Mock the registry to return controlled widget definitions
const mockGetWidget = mock((type: string) => {
  if (type === 'balance-history') {
    return {
      type: 'balance-history',
      emptyState: {
        icon: <svg data-testid="widget-icon" />,
        title: 'No balance history yet',
        noAccountsDescription: 'Connect a bank account to track your balances over time',
        noDataDescription: 'No balance data for this time period',
      },
    }
  }
  if (type === 'no-empty-state-widget') {
    return { type: 'no-empty-state-widget' }
  }
  return undefined
})

mock.module('./registry', () => ({
  getWidget: mockGetWidget,
  registerWidget: mock(() => {}),
}))
mock.module('@/features/dashboard/widgets/registry', () => ({
  getWidget: mockGetWidget,
  registerWidget: mock(() => {}),
}))

import { WidgetEmptyState } from './WidgetEmptyState'

const renderWithRouter = (ui: React.ReactNode) =>
  render(<MemoryRouter>{ui}</MemoryRouter>)

describe('WidgetEmptyState', () => {
  describe('when no accounts connected (hasAccounts: false)', () => {
    beforeEach(() => {
      mockUseDashboardContext.mockReturnValue({
        filters: { startDate: '2024-01-01', endDate: '2024-01-31' },
        setFilters: mock(() => {}),
        timeframe: 'month' as const,
        setTimeframe: mock(() => {}),
        isEditing: false,
        setIsEditing: mock(() => {}),
        hasAccounts: false,
        accountsLoading: false,
      })
    })

    it('renders noAccountsDescription when hasAccounts is false', () => {
      renderWithRouter(<WidgetEmptyState widgetType="balance-history" />)
      expect(screen.getByText('Connect a bank account to track your balances over time')).toBeInTheDocument()
    })

    it('renders the widget title', () => {
      renderWithRouter(<WidgetEmptyState widgetType="balance-history" />)
      expect(screen.getByText('No balance history yet')).toBeInTheDocument()
    })

    it('renders a Connect Account CTA link to /accounts', () => {
      renderWithRouter(<WidgetEmptyState widgetType="balance-history" />)
      const link = screen.getByRole('link', { name: /connect account/i })
      expect(link).toBeInTheDocument()
      expect(link).toHaveAttribute('href', '/accounts')
    })
  })

  describe('when accounts exist (hasAccounts: true)', () => {
    beforeEach(() => {
      mockUseDashboardContext.mockReturnValue({
        filters: { startDate: '2024-01-01', endDate: '2024-01-31' },
        setFilters: mock(() => {}),
        timeframe: 'month' as const,
        setTimeframe: mock(() => {}),
        isEditing: false,
        setIsEditing: mock(() => {}),
        hasAccounts: true,
        accountsLoading: false,
      })
    })

    it('renders noDataDescription when hasAccounts is true', () => {
      renderWithRouter(<WidgetEmptyState widgetType="balance-history" />)
      expect(screen.getByText('No balance data for this time period')).toBeInTheDocument()
    })

    it('does not render the Connect Account CTA', () => {
      renderWithRouter(<WidgetEmptyState widgetType="balance-history" />)
      expect(screen.queryByRole('link', { name: /connect account/i })).not.toBeInTheDocument()
    })
  })

  describe('accessibility', () => {
    beforeEach(() => {
      mockUseDashboardContext.mockReturnValue({
        filters: { startDate: '2024-01-01', endDate: '2024-01-31' },
        setFilters: mock(() => {}),
        timeframe: 'month' as const,
        setTimeframe: mock(() => {}),
        isEditing: false,
        setIsEditing: mock(() => {}),
        hasAccounts: false,
        accountsLoading: false,
      })
    })

    it('has role="status" for screen readers', () => {
      renderWithRouter(<WidgetEmptyState widgetType="balance-history" />)
      expect(screen.getByRole('status')).toBeInTheDocument()
    })
  })

  describe('fallback when no emptyState config', () => {
    beforeEach(() => {
      mockUseDashboardContext.mockReturnValue({
        filters: { startDate: '2024-01-01', endDate: '2024-01-31' },
        setFilters: mock(() => {}),
        timeframe: 'month' as const,
        setTimeframe: mock(() => {}),
        isEditing: false,
        setIsEditing: mock(() => {}),
        hasAccounts: false,
        accountsLoading: false,
      })
    })

    it('renders fallback text when widget has no emptyState config', () => {
      renderWithRouter(<WidgetEmptyState widgetType="no-empty-state-widget" />)
      expect(screen.getByText('No data available')).toBeInTheDocument()
    })
  })
})
