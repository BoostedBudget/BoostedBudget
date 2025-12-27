/**
 * RuleTestResults Component Tests
 * Issue #39: Rule Builder UI
 *
 * Tests for the rule test results display component.
 * TDD RED: Writing tests before implementation.
 */

import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { RuleTestResults } from './RuleTestResults'
import type { RuleTestResult } from '@/api/generated/models'

const mockTestResult: RuleTestResult = {
  rule_id: 'rule-1',
  rule_name: 'Daily Budget Check',
  rule_type: 'budget_alert',
  conditions_met: true,
  condition_results: [
    {
      field: 'budget.percent_used',
      operator: 'gte',
      value: '80',
      actual: '85',
      met: true,
    },
  ],
  simulated_action: {
    type: 'notification',
    channels: ['in_app', 'email'],
    message: 'Budget is 85% used',
  },
  trigger_data_used: {
    budget_id: 'budget-1',
    percent_used: 85,
  },
}

const mockFailedResult: RuleTestResult = {
  rule_id: 'rule-2',
  rule_name: 'Monthly Savings',
  rule_type: 'savings_goal',
  conditions_met: false,
  condition_results: [
    {
      field: 'account.balance',
      operator: 'gte',
      value: '1000',
      actual: '500',
      met: false,
    },
  ],
  simulated_action: undefined,
  trigger_data_used: {
    account_id: 'acc-1',
  },
}

const mockErrorResult: RuleTestResult = {
  rule_id: 'rule-3',
  rule_name: 'Error Rule',
  rule_type: 'transfer_suggestion',
  conditions_met: false,
  condition_results: null,
  simulated_action: undefined,
  trigger_data_used: {},
  error: 'Failed to evaluate conditions',
}

describe('RuleTestResults', () => {
  describe('Rendering', () => {
    it('should render rule name', () => {
      render(
        <RuleTestResults result={mockTestResult} onClose={() => {}} />
      )

      expect(screen.getByText('Daily Budget Check')).toBeInTheDocument()
    })

    it('should render success state when conditions met', () => {
      render(
        <RuleTestResults result={mockTestResult} onClose={() => {}} />
      )

      expect(screen.getByText(/conditions met/i)).toBeInTheDocument()
    })

    it('should render failure state when conditions not met', () => {
      render(
        <RuleTestResults result={mockFailedResult} onClose={() => {}} />
      )

      expect(screen.getByText(/conditions not met/i)).toBeInTheDocument()
    })

    it('should render error state when test failed', () => {
      render(
        <RuleTestResults result={mockErrorResult} onClose={() => {}} />
      )

      // Check for error status indicator (not rule name which is "Error Rule")
      const errorStatus = screen.getByText('Error')
      expect(errorStatus).toBeInTheDocument()
      expect(screen.getByText(/failed to evaluate/i)).toBeInTheDocument()
    })
  })

  describe('Condition Results', () => {
    it('should display condition field', () => {
      render(
        <RuleTestResults result={mockTestResult} onClose={() => {}} />
      )

      expect(screen.getByText(/budget\.percent_used/)).toBeInTheDocument()
    })

    it('should display expected value', () => {
      render(
        <RuleTestResults result={mockTestResult} onClose={() => {}} />
      )

      // Look for the expected value in the condition comparison text
      expect(screen.getByText(/gte 80/)).toBeInTheDocument()
    })

    it('should display actual value', () => {
      render(
        <RuleTestResults result={mockTestResult} onClose={() => {}} />
      )

      // Look for the actual value in the condition actual text
      expect(screen.getByText(/Actual: 85/)).toBeInTheDocument()
    })

    it('should show passed indicator for passing conditions', () => {
      render(
        <RuleTestResults result={mockTestResult} onClose={() => {}} />
      )

      // Look for a passed indicator (checkmark or similar)
      const passedIndicators = screen.getAllByTestId('condition-passed')
      expect(passedIndicators.length).toBeGreaterThan(0)
    })

    it('should show failed indicator for failing conditions', () => {
      render(
        <RuleTestResults result={mockFailedResult} onClose={() => {}} />
      )

      const failedIndicators = screen.getAllByTestId('condition-failed')
      expect(failedIndicators.length).toBeGreaterThan(0)
    })
  })

  describe('Simulated Action', () => {
    it('should display simulated action when conditions met', () => {
      render(
        <RuleTestResults result={mockTestResult} onClose={() => {}} />
      )

      expect(screen.getByText(/simulated action/i)).toBeInTheDocument()
    })

    it('should show notification channels', () => {
      render(
        <RuleTestResults result={mockTestResult} onClose={() => {}} />
      )

      expect(screen.getByText(/in_app/i)).toBeInTheDocument()
      expect(screen.getByText(/email/i)).toBeInTheDocument()
    })

    it('should not show simulated action when conditions not met', () => {
      render(
        <RuleTestResults result={mockFailedResult} onClose={() => {}} />
      )

      expect(screen.queryByText(/simulated action/i)).not.toBeInTheDocument()
    })
  })

  describe('Close Button', () => {
    it('should call onClose when close button clicked', () => {
      const onClose = vi.fn()

      render(
        <RuleTestResults result={mockTestResult} onClose={onClose} />
      )

      // There are two close buttons (X icon and Close text), use the footer button
      const closeButtons = screen.getAllByRole('button', { name: /close/i })
      fireEvent.click(closeButtons[closeButtons.length - 1])

      expect(onClose).toHaveBeenCalled()
    })
  })

  describe('Loading State', () => {
    it('should show loading state when isLoading is true', () => {
      render(
        <RuleTestResults result={null} isLoading onClose={() => {}} />
      )

      expect(screen.getByText(/testing rule/i)).toBeInTheDocument()
    })
  })

  describe('Accessibility', () => {
    it('should have accessible dialog structure', () => {
      render(
        <RuleTestResults result={mockTestResult} onClose={() => {}} />
      )

      expect(screen.getByRole('dialog')).toBeInTheDocument()
    })

    it('should have accessible heading', () => {
      render(
        <RuleTestResults result={mockTestResult} onClose={() => {}} />
      )

      // Check for the main title heading (h2)
      expect(screen.getByRole('heading', { level: 2 })).toBeInTheDocument()
    })
  })
})
