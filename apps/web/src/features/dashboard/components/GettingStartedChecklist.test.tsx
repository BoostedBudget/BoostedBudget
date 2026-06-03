/**
 * GettingStartedChecklist Component Tests
 * Issue #848: Onboarding - Add first-time user experience and guided setup
 */

import { describe, it, expect, mock, beforeEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import userEvent from '@testing-library/user-event'
import { GettingStartedChecklist } from './GettingStartedChecklist'
import type { GettingStartedStep } from '../hooks/useGettingStarted'

const MOCK_STEPS: GettingStartedStep[] = [
  {
    id: 'connect-bank',
    label: 'Connect your first bank',
    description: 'Link a bank account to start tracking your finances',
    complete: false,
    href: '/accounts',
  },
  {
    id: 'set-up-budget',
    label: 'Set up a budget',
    description: 'Create a budget to track your spending',
    complete: true,
    href: '/budgets',
  },
  {
    id: 'create-savings-goal',
    label: 'Create a savings goal',
    description: 'Set a goal for financial targets',
    complete: false,
    href: '/budgets',
  },
  {
    id: 'create-automation-rule',
    label: 'Create an automation rule',
    description: 'Automate budget alerts',
    complete: false,
    href: '/rules',
  },
  {
    id: 'set-up-auto-categorization',
    label: 'Set up auto-categorization',
    description: 'Automatically categorize transactions',
    complete: false,
    href: '/auto-categorization',
  },
]

const mockOnDismiss = mock(() => {})
const mockOnStepClick = mock((_stepId: string) => {})

describe('GettingStartedChecklist', () => {
  beforeEach(() => {
    mockOnDismiss.mockClear()
    mockOnStepClick.mockClear()
  })

  const renderComponent = (overrides: Partial<{
    steps: GettingStartedStep[]
    completedCount: number
    isCelebrating: boolean
  }> = {}) =>
    render(
      <MemoryRouter>
        <GettingStartedChecklist
          steps={MOCK_STEPS}
          completedCount={1}
          totalSteps={5}
          isCelebrating={false}
          onDismiss={mockOnDismiss}
          {...overrides}
        />
      </MemoryRouter>
    )

  describe('Rendering', () => {
    it('renders the "Getting Started" heading', () => {
      renderComponent()
      expect(screen.getByRole('heading', { name: /getting started/i })).toBeInTheDocument()
    })

    it('renders all 5 step labels', () => {
      renderComponent()
      expect(screen.getByText('Connect your first bank')).toBeInTheDocument()
      expect(screen.getByText('Set up a budget')).toBeInTheDocument()
      expect(screen.getByText('Create a savings goal')).toBeInTheDocument()
      expect(screen.getByText('Create an automation rule')).toBeInTheDocument()
      expect(screen.getByText('Set up auto-categorization')).toBeInTheDocument()
    })

    it('renders progress text showing completed count', () => {
      renderComponent({ completedCount: 1 })
      expect(screen.getByText(/1 of 5/i)).toBeInTheDocument()
    })

    it('renders progress text correctly for 0 completed', () => {
      renderComponent({ completedCount: 0 })
      expect(screen.getByText(/0 of 5/i)).toBeInTheDocument()
    })

    it('renders progress text correctly for all completed', () => {
      renderComponent({ completedCount: 5 })
      expect(screen.getByText(/5 of 5/i)).toBeInTheDocument()
    })
  })

  describe('Step completion indicators', () => {
    it('renders a check icon for completed steps', () => {
      renderComponent()
      // The completed step (Set up a budget) should have a check indicator
      const completedStepEl = screen.getByTestId('step-set-up-budget')
      expect(completedStepEl).toHaveAttribute('data-complete', 'true')
    })

    it('renders a circle/pending icon for incomplete steps', () => {
      renderComponent()
      const incompleteStepEl = screen.getByTestId('step-connect-bank')
      expect(incompleteStepEl).toHaveAttribute('data-complete', 'false')
    })
  })

  describe('Step links', () => {
    it('renders incomplete steps as links to their routes', () => {
      renderComponent()
      // connect-bank is incomplete → link to /accounts
      const connectBankLink = screen.getByRole('link', { name: /connect your first bank/i })
      expect(connectBankLink).toBeInTheDocument()
      expect(connectBankLink).toHaveAttribute('href', '/accounts')
    })

    it('renders incomplete steps with different routes correctly', () => {
      renderComponent()
      const rulesLink = screen.getByRole('link', { name: /create an automation rule/i })
      expect(rulesLink).toHaveAttribute('href', '/rules')

      const autoCatLink = screen.getByRole('link', { name: /set up auto-categorization/i })
      expect(autoCatLink).toHaveAttribute('href', '/auto-categorization')
    })

    it('does not render completed steps as links', () => {
      renderComponent()
      // "Set up a budget" is complete → should not be a link
      const budgetLinks = screen.queryAllByRole('link', { name: /set up a budget/i })
      expect(budgetLinks).toHaveLength(0)
    })

    it('renders completed step text as plain text', () => {
      renderComponent()
      expect(screen.getByText('Set up a budget')).toBeInTheDocument()
    })
  })

  describe('Dismiss button', () => {
    it('renders a dismiss button', () => {
      renderComponent()
      expect(screen.getByRole('button', { name: /dismiss/i })).toBeInTheDocument()
    })

    it('calls onDismiss when dismiss button is clicked', async () => {
      const user = userEvent.setup()
      renderComponent()
      await user.click(screen.getByRole('button', { name: /dismiss/i }))
      expect(mockOnDismiss).toHaveBeenCalledTimes(1)
    })
  })

  describe('Celebration state', () => {
    it('shows "You\'re all set!" when isCelebrating is true', () => {
      renderComponent({ isCelebrating: true })
      expect(screen.getByText(/you're all set/i)).toBeInTheDocument()
    })

    it('hides the step list when celebrating', () => {
      renderComponent({ isCelebrating: true })
      expect(screen.queryByText('Connect your first bank')).not.toBeInTheDocument()
    })

    it('still renders the heading during celebration', () => {
      renderComponent({ isCelebrating: true })
      expect(screen.getByRole('heading', { name: /getting started/i })).toBeInTheDocument()
    })
  })

  describe('Step click tracking', () => {
    it('calls onStepClick with the step id when an incomplete step link is clicked', async () => {
      const user = userEvent.setup()
      render(
        <MemoryRouter>
          <GettingStartedChecklist
            steps={MOCK_STEPS}
            completedCount={1}
            totalSteps={5}
            isCelebrating={false}
            onDismiss={mockOnDismiss}
            onStepClick={mockOnStepClick}
          />
        </MemoryRouter>
      )
      await user.click(screen.getByRole('link', { name: /connect your first bank/i }))
      expect(mockOnStepClick).toHaveBeenCalledTimes(1)
      expect(mockOnStepClick).toHaveBeenCalledWith('connect-bank')
    })

    it('does not throw when onStepClick is omitted and a step is clicked', async () => {
      const user = userEvent.setup()
      renderComponent()
      // click an incomplete step — should not throw even without onStepClick prop
      await expect(
        user.click(screen.getByRole('link', { name: /connect your first bank/i }))
      ).resolves.toBeUndefined()
    })
  })

  describe('Accessibility', () => {
    it('renders steps as an accessible list', () => {
      renderComponent()
      expect(screen.getByRole('list')).toBeInTheDocument()
    })

    it('dismiss button has accessible label', () => {
      renderComponent()
      expect(screen.getByRole('button', { name: /dismiss/i })).toBeInTheDocument()
    })

    it('progress bar has accessible label and range', () => {
      renderComponent({ completedCount: 2 })
      const progressBar = screen.getByRole('progressbar')
      expect(progressBar).toHaveAttribute('aria-label', 'Getting started progress')
      expect(progressBar).toHaveAttribute('aria-valuemin', '0')
      expect(progressBar).toHaveAttribute('aria-valuenow', '2')
      expect(progressBar).toHaveAttribute('aria-valuemax', '5')
    })
  })
})
