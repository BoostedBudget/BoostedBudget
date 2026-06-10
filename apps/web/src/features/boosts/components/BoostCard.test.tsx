/**
 * BoostCard Component Tests
 * Issue #39: Rule Builder UI
 *
 * Tests for the rule card display component.
 * TDD RED: Writing tests before implementation.
 */

import { describe, it, expect, mock } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { ReadOnlyProvider } from '@/auth/ReadOnlyContext'
import { BoostCard } from './BoostCard'
import type { Boost } from '@boostedbudget/api-client/models'

const mockBoost: Boost = {
  id: 'boost-1',
  user_id: 'user-1',
  name: 'Daily Budget Check',
  description: 'Check budget usage daily',
  boost_type: 'budget_alert',
  enabled: true,
  priority: 10,
  execution_count: 5,
  trigger_config: {
    type: 'schedule',
    cron: '0 9 * * *',
    timezone: 'America/New_York',
  },
  conditions: [],
  condition_logic: 'AND',
  actions: {},
  created_at: '2024-01-01T00:00:00Z',
  updated_at: '2024-01-15T00:00:00Z',
  last_executed_at: '2024-01-15T09:00:00Z',
  last_successful_at: '2024-01-15T09:00:00Z',
} as const

describe('BoostCard', () => {

  describe('Rendering', () => {
    it('should render boost name', () => {
      render(<BoostCard boost={mockBoost} />)

      expect(screen.getByText('Daily Budget Check')).toBeInTheDocument()
    })

    it('should render boost description when provided', () => {
      render(<BoostCard boost={mockBoost} />)

      expect(screen.getByText('Check budget usage daily')).toBeInTheDocument()
    })

    it('should not render description when not provided', () => {
      const ruleWithoutDesc = { ...mockBoost, description: undefined }
      render(<BoostCard boost={ruleWithoutDesc} />)

      expect(screen.queryByText('Check budget usage daily')).not.toBeInTheDocument()
    })

    it('should render boost type label', () => {
      render(<BoostCard boost={mockBoost} />)

      expect(screen.getByText('Budget Alert')).toBeInTheDocument()
    })

    it('should render enabled status indicator when enabled', () => {
      render(<BoostCard boost={mockBoost} />)

      expect(screen.getByText('Enabled')).toBeInTheDocument()
    })

    it('should render disabled status indicator when disabled', () => {
      const disabledBoost = { ...mockBoost, enabled: false }
      render(<BoostCard boost={disabledBoost} />)

      expect(screen.getByText('Disabled')).toBeInTheDocument()
    })

    it('should render execution count', () => {
      render(<BoostCard boost={mockBoost} />)

      expect(screen.getByText(/5 executions/i)).toBeInTheDocument()
    })

    it('should render last execution time when available', () => {
      render(<BoostCard boost={mockBoost} />)

      expect(screen.getByText(/last run/i)).toBeInTheDocument()
    })

    it('should show "Never run" when no last execution', () => {
      const neverRunBoost = { ...mockBoost, last_executed_at: undefined }
      render(<BoostCard boost={neverRunBoost} />)

      expect(screen.getByText(/never run/i)).toBeInTheDocument()
    })

    it('should render trigger type badge', () => {
      render(<BoostCard boost={mockBoost} />)

      expect(screen.getByText(/scheduled/i)).toBeInTheDocument()
    })

    it('should render event trigger badge for event-based rules', () => {
      const eventRule = {
        ...mockBoost,
        trigger_config: {
          type: 'event',
          event: 'budget_updated',
        },
      }
      render(<BoostCard boost={eventRule} />)

      expect(screen.getByText(/event/i)).toBeInTheDocument()
    })
  })

  describe('Boost Types', () => {
    it('should show transfer suggestion label', () => {
      const rule = { ...mockBoost, boost_type: 'transfer_suggestion' }
      render(<BoostCard boost={rule} />)

      expect(screen.getByText('Transfer Suggestion')).toBeInTheDocument()
    })

    it('should show category rollover label', () => {
      const rule = { ...mockBoost, boost_type: 'category_rollover' }
      render(<BoostCard boost={rule} />)

      expect(screen.getByText('Category Rollover')).toBeInTheDocument()
    })

    it('should show savings goal label', () => {
      const rule = { ...mockBoost, boost_type: 'savings_goal' }
      render(<BoostCard boost={rule} />)

      expect(screen.getByText('Savings Goal')).toBeInTheDocument()
    })
  })

  describe('Actions', () => {
    it('should call onEdit when edit button is clicked', () => {
      const onEdit = mock(() => {})
      render(<BoostCard boost={mockBoost} onEdit={onEdit} />)

      fireEvent.click(screen.getByRole('button', { name: /edit/i }))

      expect(onEdit).toHaveBeenCalledWith(mockBoost)
    })

    it('should call onDelete when delete button is clicked', () => {
      const onDelete = mock(() => {})
      render(<BoostCard boost={mockBoost} onDelete={onDelete} />)

      fireEvent.click(screen.getByRole('button', { name: /delete/i }))

      expect(onDelete).toHaveBeenCalledWith(mockBoost)
    })

    it('should call onToggle when toggle is clicked', () => {
      const onToggle = mock(() => {})
      render(<BoostCard boost={mockBoost} onToggle={onToggle} />)

      fireEvent.click(screen.getByRole('switch'))

      expect(onToggle).toHaveBeenCalledWith(mockBoost, false)
    })

    it('should call onToggle with true when enabling disabled boost', () => {
      const onToggle = mock(() => {})
      const disabledBoost = { ...mockBoost, enabled: false }
      render(<BoostCard boost={disabledBoost} onToggle={onToggle} />)

      fireEvent.click(screen.getByRole('switch'))

      expect(onToggle).toHaveBeenCalledWith(disabledBoost, true)
    })

    it('should call onTest when test button is clicked', () => {
      const onTest = mock(() => {})
      render(<BoostCard boost={mockBoost} onTest={onTest} />)

      fireEvent.click(screen.getByRole('button', { name: /test/i }))

      expect(onTest).toHaveBeenCalledWith(mockBoost)
    })

    it('should not show edit button when onEdit not provided', () => {
      render(<BoostCard boost={mockBoost} />)

      expect(screen.queryByRole('button', { name: /edit/i })).not.toBeInTheDocument()
    })

    it('should not show delete button when onDelete not provided', () => {
      render(<BoostCard boost={mockBoost} />)

      expect(screen.queryByRole('button', { name: /delete/i })).not.toBeInTheDocument()
    })

    it('should not show toggle when onToggle not provided', () => {
      render(<BoostCard boost={mockBoost} />)

      expect(screen.queryByRole('switch')).not.toBeInTheDocument()
    })
  })

  describe('Loading State', () => {
    it('should disable actions when loading', () => {
      const onEdit = mock(() => {})
      const onDelete = mock(() => {})
      const onToggle = mock(() => {})

      render(
        <BoostCard
          boost={mockBoost}
          onEdit={onEdit}
          onDelete={onDelete}
          onToggle={onToggle}
          loading
        />
      )

      expect(screen.getByRole('button', { name: /edit/i })).toBeDisabled()
      expect(screen.getByRole('button', { name: /delete/i })).toBeDisabled()
      expect(screen.getByRole('switch')).toBeDisabled()
    })
  })

  describe('Compact Mode', () => {
    it('should hide description in compact mode', () => {
      render(<BoostCard boost={mockBoost} compact />)

      expect(screen.queryByText('Check budget usage daily')).not.toBeInTheDocument()
    })

    it('should show name in compact mode', () => {
      render(<BoostCard boost={mockBoost} compact />)

      expect(screen.getByText('Daily Budget Check')).toBeInTheDocument()
    })
  })

  describe('Accessibility', () => {
    it('should have accessible card role', () => {
      render(<BoostCard boost={mockBoost} />)

      expect(screen.getByRole('article')).toBeInTheDocument()
    })

    it('should have accessible toggle switch', () => {
      const onToggle = mock(() => {})
      render(<BoostCard boost={mockBoost} onToggle={onToggle} />)

      const toggle = screen.getByRole('switch')
      expect(toggle).toHaveAttribute('aria-checked', 'true')
    })

    it('should have accessible toggle for disabled boost', () => {
      const onToggle = mock(() => {})
      const disabledBoost = { ...mockBoost, enabled: false }
      render(<BoostCard boost={disabledBoost} onToggle={onToggle} />)

      const toggle = screen.getByRole('switch')
      expect(toggle).toHaveAttribute('aria-checked', 'false')
    })
  })

  describe('Priority Display', () => {
    it('should display priority value', () => {
      render(<BoostCard boost={mockBoost} showPriority />)

      expect(screen.getByText(/priority.*10/i)).toBeInTheDocument()
    })

    it('should not display priority by default', () => {
      render(<BoostCard boost={mockBoost} />)

      expect(screen.queryByText(/priority/i)).not.toBeInTheDocument()
    })
  })

  describe('Condition summary (W5)', () => {
    const boostWithConditions = {
      ...mockBoost,
      conditions: [{ field: 'budget.percent_used', operator: 'gte', value: 80 }],
    }

    it('shows condition summary when conditions are present', () => {
      render(<BoostCard boost={boostWithConditions} />)
      // Expects "Budget % Used" label and the value "80"
      const bodyText = document.body.textContent ?? ''
      expect(/Budget % Used/i.test(bodyText)).toBe(true)
      expect(/80/.test(bodyText)).toBe(true)
    })

    it('does not show condition summary when conditions is empty', () => {
      render(<BoostCard boost={{ ...mockBoost, conditions: [] }} />)
      // No "When" prefix expected
      const bodyText = document.body.textContent ?? ''
      expect(/^When /m.test(bodyText)).toBe(false)
    })

    it('shows View recent executions link when onViewExecutions is provided', () => {
      render(<BoostCard boost={mockBoost} onViewExecutions={mock(() => {})} />)
      expect(screen.getByRole('button', { name: /view recent executions/i })).toBeTruthy()
    })

    it('calls onViewExecutions with boost when link is clicked', () => {
      const onViewExecutions = mock((_boost: Boost) => {})
      render(<BoostCard boost={mockBoost} onViewExecutions={onViewExecutions} />)
      fireEvent.click(screen.getByRole('button', { name: /view recent executions/i }))
      expect(onViewExecutions).toHaveBeenCalledWith(mockBoost)
    })

    it('does not show View recent executions button when prop is absent', () => {
      render(<BoostCard boost={mockBoost} />)
      expect(screen.queryByRole('button', { name: /view recent executions/i })).toBeNull()
    })
  })

  describe('Action summary (R2-B)', () => {
    it('shows action summary for transfer_suggestion boost with amount', () => {
      const transferBoost = {
        ...mockBoost,
        boost_type: 'transfer_suggestion',
        actions: { amount: 200 },
      }
      render(<BoostCard boost={transferBoost} />)
      const bodyText = document.body.textContent ?? ''
      expect(bodyText).toContain('Suggest transfer $200')
    })

    it('shows action summary for transfer_suggestion boost without amount', () => {
      const transferBoost = {
        ...mockBoost,
        boost_type: 'transfer_suggestion',
        actions: {},
      }
      render(<BoostCard boost={transferBoost} />)
      const bodyText = document.body.textContent ?? ''
      expect(bodyText).toContain('Suggest transfer')
    })

    it('shows action summary for budget_alert rule', () => {
      render(<BoostCard boost={mockBoost} />)
      const bodyText = document.body.textContent ?? ''
      expect(bodyText).toContain('Send budget alert notification')
    })

    it('masks action summary with data-ph-mask (PII — financial amount)', () => {
      const transferBoost = {
        ...mockBoost,
        boost_type: 'transfer_suggestion',
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        actions: { amount: 200 } as any,
      }
      const { container } = render(<BoostCard boost={transferBoost} />)
      // boost.name is always masked; action summary containing "$200" must also be masked
      const masked = container.querySelectorAll('[data-ph-mask]')
      expect(masked.length).toBeGreaterThanOrEqual(2)
    })

    it('masks condition summary with data-ph-mask (PII — condition values can be currency amounts)', () => {
      // R4-1: budget.total_spent is a currency field — condition summary "When Budget Total Spent ≥ 500"
      // must be masked just like the action summary on the line below it.
      const boostWithAmountCondition = {
        ...mockBoost,
        conditions: [{ field: 'budget.total_spent', operator: 'gte', value: 500 }],
      }
      const { container } = render(<BoostCard boost={boostWithAmountCondition} />)
      // At least: boost.name mask + condition summary mask (+ optional action mask)
      const masked = container.querySelectorAll('[data-ph-mask]')
      expect(masked.length).toBeGreaterThanOrEqual(2)
      // Specifically, an element with data-ph-mask must contain "Budget Total Spent"
      const conditionMasked = Array.from(masked).some((el) =>
        el.textContent?.includes('Budget Total Spent')
      )
      expect(conditionMasked).toBe(true)
    })
  })

  describe('Read-only guarding (W6)', () => {
    it('wraps toggle + action buttons in ReadOnlyGuard in read-only mode', () => {
      const { container } = render(
        <ReadOnlyProvider isReadOnly={true}>
          <BoostCard
            boost={mockBoost}
            onEdit={mock(() => {})}
            onDelete={mock(() => {})}
            onToggle={mock(() => {})}
            onTest={mock(() => {})}
          />
        </ReadOnlyProvider>
      )
      expect(container.querySelector('.pointer-events-none')).toBeTruthy()
    })
  })
})
