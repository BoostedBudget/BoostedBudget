/**
 * WidgetEmptyState Tests
 * Issue #834: Dashboard empty states with context-aware CTAs
 */

import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router'

// Issue #1323: Use centralized registry mock (preloaded via bunfig.toml).
// Inline mock.module('./registry') would clobber the global mock and pollute
// Widget Registry tests that run in the same batch.
import { mockGetWidget } from '@/test/mocks/registry'
import { mockUseDashboardContext } from '@/test/mocks/dashboard-context'

// Widget definitions used by this test file
const balanceHistoryWidget = {
  type: 'balance-history',
  emptyState: {
    icon: <svg data-testid="widget-icon" />,
    title: 'No balance history yet',
    noAccountsDescription: 'Connect a bank account to track your balances over time',
    noDataDescription: 'No balance data for this time period',
  },
}

const noEmptyStateWidget = { type: 'no-empty-state-widget' }

// Default mock context used across all describes
const defaultDashboardContext = {
  filters: { startDate: '2024-01-01', endDate: '2024-01-31' },
  setFilters: mock(() => {}),
  timeframe: 'this_month' as const,
  setTimeframe: mock(() => {}),
  isEditing: false,
  setIsEditing: mock(() => {}),
  hasAccounts: false,
  accountsLoading: false,
}

import { WidgetEmptyState } from './WidgetEmptyState'

const renderWithRouter = (ui: React.ReactNode) =>
  render(<MemoryRouter>{ui}</MemoryRouter>)

describe('WidgetEmptyState', () => {
  describe('when no accounts connected (hasAccounts: false)', () => {
    beforeEach(() => {
      mockGetWidget.mockImplementation((type: string) => {
        if (type === 'balance-history') return balanceHistoryWidget as never
        if (type === 'no-empty-state-widget') return noEmptyStateWidget as never
        return undefined
      })
      mockUseDashboardContext.mockReturnValue({
        ...defaultDashboardContext,
        hasAccounts: false,
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
      mockGetWidget.mockImplementation((type: string) => {
        if (type === 'balance-history') return balanceHistoryWidget as never
        return undefined
      })
      mockUseDashboardContext.mockReturnValue({
        ...defaultDashboardContext,
        hasAccounts: true,
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
      mockGetWidget.mockImplementation((type: string) => {
        if (type === 'balance-history') return balanceHistoryWidget as never
        return undefined
      })
      mockUseDashboardContext.mockReturnValue({
        ...defaultDashboardContext,
        hasAccounts: false,
      })
    })

    it('has role="status" for screen readers', () => {
      renderWithRouter(<WidgetEmptyState widgetType="balance-history" />)
      expect(screen.getByRole('status')).toBeInTheDocument()
    })
  })

  describe('fallback when no emptyState config', () => {
    beforeEach(() => {
      mockGetWidget.mockImplementation((type: string) => {
        if (type === 'no-empty-state-widget') return noEmptyStateWidget as never
        return undefined
      })
      mockUseDashboardContext.mockReturnValue({
        ...defaultDashboardContext,
        hasAccounts: false,
      })
    })

    it('renders fallback text when widget has no emptyState config', () => {
      renderWithRouter(<WidgetEmptyState widgetType="no-empty-state-widget" />)
      expect(screen.getByText('No data available')).toBeInTheDocument()
    })
  })
})
