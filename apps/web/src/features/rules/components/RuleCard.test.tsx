/**
 * RuleCard Component Tests
 * Issue #39: Rule Builder UI
 *
 * Tests for the rule card display component.
 * TDD RED: Writing tests before implementation.
 */

import { describe, it, expect, mock } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { RuleCard } from './RuleCard'
import type { AutomationRule } from '@boostedbudget/api-client/models'

const mockRule: AutomationRule = {
  id: 'rule-1',
  user_id: 'user-1',
  name: 'Daily Budget Check',
  description: 'Check budget usage daily',
  rule_type: 'budget_alert',
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

describe('RuleCard', () => {

  describe('Rendering', () => {
    it('should render rule name', () => {
      render(<RuleCard rule={mockRule} />)

      expect(screen.getByText('Daily Budget Check')).toBeInTheDocument()
    })

    it('should render rule description when provided', () => {
      render(<RuleCard rule={mockRule} />)

      expect(screen.getByText('Check budget usage daily')).toBeInTheDocument()
    })

    it('should not render description when not provided', () => {
      const ruleWithoutDesc = { ...mockRule, description: undefined }
      render(<RuleCard rule={ruleWithoutDesc} />)

      expect(screen.queryByText('Check budget usage daily')).not.toBeInTheDocument()
    })

    it('should render rule type label', () => {
      render(<RuleCard rule={mockRule} />)

      expect(screen.getByText('Budget Alert')).toBeInTheDocument()
    })

    it('should render enabled status indicator when enabled', () => {
      render(<RuleCard rule={mockRule} />)

      expect(screen.getByText('Enabled')).toBeInTheDocument()
    })

    it('should render disabled status indicator when disabled', () => {
      const disabledRule = { ...mockRule, enabled: false }
      render(<RuleCard rule={disabledRule} />)

      expect(screen.getByText('Disabled')).toBeInTheDocument()
    })

    it('should render execution count', () => {
      render(<RuleCard rule={mockRule} />)

      expect(screen.getByText(/5 executions/i)).toBeInTheDocument()
    })

    it('should render last execution time when available', () => {
      render(<RuleCard rule={mockRule} />)

      expect(screen.getByText(/last run/i)).toBeInTheDocument()
    })

    it('should show "Never run" when no last execution', () => {
      const neverRunRule = { ...mockRule, last_executed_at: undefined }
      render(<RuleCard rule={neverRunRule} />)

      expect(screen.getByText(/never run/i)).toBeInTheDocument()
    })

    it('should render trigger type badge', () => {
      render(<RuleCard rule={mockRule} />)

      expect(screen.getByText(/scheduled/i)).toBeInTheDocument()
    })

    it('should render event trigger badge for event-based rules', () => {
      const eventRule = {
        ...mockRule,
        trigger_config: {
          type: 'event',
          event: 'budget_updated',
        },
      }
      render(<RuleCard rule={eventRule} />)

      expect(screen.getByText(/event/i)).toBeInTheDocument()
    })
  })

  describe('Rule Types', () => {
    it('should show transfer suggestion label', () => {
      const rule = { ...mockRule, rule_type: 'transfer_suggestion' }
      render(<RuleCard rule={rule} />)

      expect(screen.getByText('Transfer Suggestion')).toBeInTheDocument()
    })

    it('should show category rollover label', () => {
      const rule = { ...mockRule, rule_type: 'category_rollover' }
      render(<RuleCard rule={rule} />)

      expect(screen.getByText('Category Rollover')).toBeInTheDocument()
    })

    it('should show savings goal label', () => {
      const rule = { ...mockRule, rule_type: 'savings_goal' }
      render(<RuleCard rule={rule} />)

      expect(screen.getByText('Savings Goal')).toBeInTheDocument()
    })
  })

  describe('Actions', () => {
    it('should call onEdit when edit button is clicked', () => {
      const onEdit = mock(() => {})
      render(<RuleCard rule={mockRule} onEdit={onEdit} />)

      fireEvent.click(screen.getByRole('button', { name: /edit/i }))

      expect(onEdit).toHaveBeenCalledWith(mockRule)
    })

    it('should call onDelete when delete button is clicked', () => {
      const onDelete = mock(() => {})
      render(<RuleCard rule={mockRule} onDelete={onDelete} />)

      fireEvent.click(screen.getByRole('button', { name: /delete/i }))

      expect(onDelete).toHaveBeenCalledWith(mockRule)
    })

    it('should call onToggle when toggle is clicked', () => {
      const onToggle = mock(() => {})
      render(<RuleCard rule={mockRule} onToggle={onToggle} />)

      fireEvent.click(screen.getByRole('switch'))

      expect(onToggle).toHaveBeenCalledWith(mockRule, false)
    })

    it('should call onToggle with true when enabling disabled rule', () => {
      const onToggle = mock(() => {})
      const disabledRule = { ...mockRule, enabled: false }
      render(<RuleCard rule={disabledRule} onToggle={onToggle} />)

      fireEvent.click(screen.getByRole('switch'))

      expect(onToggle).toHaveBeenCalledWith(disabledRule, true)
    })

    it('should call onTest when test button is clicked', () => {
      const onTest = mock(() => {})
      render(<RuleCard rule={mockRule} onTest={onTest} />)

      fireEvent.click(screen.getByRole('button', { name: /test/i }))

      expect(onTest).toHaveBeenCalledWith(mockRule)
    })

    it('should not show edit button when onEdit not provided', () => {
      render(<RuleCard rule={mockRule} />)

      expect(screen.queryByRole('button', { name: /edit/i })).not.toBeInTheDocument()
    })

    it('should not show delete button when onDelete not provided', () => {
      render(<RuleCard rule={mockRule} />)

      expect(screen.queryByRole('button', { name: /delete/i })).not.toBeInTheDocument()
    })

    it('should not show toggle when onToggle not provided', () => {
      render(<RuleCard rule={mockRule} />)

      expect(screen.queryByRole('switch')).not.toBeInTheDocument()
    })
  })

  describe('Loading State', () => {
    it('should disable actions when loading', () => {
      const onEdit = mock(() => {})
      const onDelete = mock(() => {})
      const onToggle = mock(() => {})

      render(
        <RuleCard
          rule={mockRule}
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
      render(<RuleCard rule={mockRule} compact />)

      expect(screen.queryByText('Check budget usage daily')).not.toBeInTheDocument()
    })

    it('should show name in compact mode', () => {
      render(<RuleCard rule={mockRule} compact />)

      expect(screen.getByText('Daily Budget Check')).toBeInTheDocument()
    })
  })

  describe('Accessibility', () => {
    it('should have accessible card role', () => {
      render(<RuleCard rule={mockRule} />)

      expect(screen.getByRole('article')).toBeInTheDocument()
    })

    it('should have accessible toggle switch', () => {
      const onToggle = mock(() => {})
      render(<RuleCard rule={mockRule} onToggle={onToggle} />)

      const toggle = screen.getByRole('switch')
      expect(toggle).toHaveAttribute('aria-checked', 'true')
    })

    it('should have accessible toggle for disabled rule', () => {
      const onToggle = mock(() => {})
      const disabledRule = { ...mockRule, enabled: false }
      render(<RuleCard rule={disabledRule} onToggle={onToggle} />)

      const toggle = screen.getByRole('switch')
      expect(toggle).toHaveAttribute('aria-checked', 'false')
    })
  })

  describe('Priority Display', () => {
    it('should display priority value', () => {
      render(<RuleCard rule={mockRule} showPriority />)

      expect(screen.getByText(/priority.*10/i)).toBeInTheDocument()
    })

    it('should not display priority by default', () => {
      render(<RuleCard rule={mockRule} />)

      expect(screen.queryByText(/priority/i)).not.toBeInTheDocument()
    })
  })
})
