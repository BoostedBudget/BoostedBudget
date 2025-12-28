/**
 * TriggerSelector Component Tests
 * Issue #39: Rule Builder UI
 *
 * Tests for the trigger type selector (schedule vs event).
 * TDD RED: Writing tests before implementation.
 */

import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { TriggerSelector } from './TriggerSelector'
import type { TriggerFormState } from '../types'

const defaultTriggerConfig: TriggerFormState = {
  type: 'schedule',
  cron: '0 9 * * *',
  timezone: 'America/New_York',
}

describe('TriggerSelector', () => {
  describe('Rendering', () => {
    it('should render schedule and event options', () => {
      render(
        <TriggerSelector
          config={defaultTriggerConfig}
          onChange={() => {}}
          canUseEventTriggers={true}
        />
      )

      expect(screen.getByRole('radio', { name: /scheduled/i })).toBeInTheDocument()
      expect(screen.getByRole('radio', { name: /event/i })).toBeInTheDocument()
    })

    it('should show descriptions for each trigger type', () => {
      render(
        <TriggerSelector
          config={defaultTriggerConfig}
          onChange={() => {}}
          canUseEventTriggers={true}
        />
      )

      expect(screen.getByText(/run on a fixed schedule/i)).toBeInTheDocument()
      expect(screen.getByText(/run when something happens/i)).toBeInTheDocument()
    })

    it('should check schedule option when type is schedule', () => {
      render(
        <TriggerSelector
          config={defaultTriggerConfig}
          onChange={() => {}}
          canUseEventTriggers={true}
        />
      )

      expect(screen.getByRole('radio', { name: /scheduled/i })).toBeChecked()
      expect(screen.getByRole('radio', { name: /event/i })).not.toBeChecked()
    })

    it('should check event option when type is event', () => {
      const eventConfig: TriggerFormState = {
        type: 'event',
        event: 'budget_updated',
      }

      render(
        <TriggerSelector
          config={eventConfig}
          onChange={() => {}}
          canUseEventTriggers={true}
        />
      )

      expect(screen.getByRole('radio', { name: /scheduled/i })).not.toBeChecked()
      expect(screen.getByRole('radio', { name: /event/i })).toBeChecked()
    })
  })

  describe('Schedule Trigger', () => {
    it('should show CronBuilder when schedule is selected', () => {
      render(
        <TriggerSelector
          config={defaultTriggerConfig}
          onChange={() => {}}
          canUseEventTriggers={true}
        />
      )

      // CronBuilder should show preset buttons
      expect(screen.getByRole('button', { name: /daily at 9am/i })).toBeInTheDocument()
    })

    it('should call onChange with updated cron when preset is clicked', () => {
      const onChange = vi.fn()
      render(
        <TriggerSelector
          config={defaultTriggerConfig}
          onChange={onChange}
          canUseEventTriggers={true}
        />
      )

      fireEvent.click(screen.getByRole('button', { name: 'Weekly (Monday)' }))

      expect(onChange).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'schedule',
          cron: '0 9 * * 1',
        })
      )
    })
  })

  describe('Event Trigger', () => {
    it('should show event dropdown when event is selected', () => {
      const eventConfig: TriggerFormState = {
        type: 'event',
        event: 'budget_updated',
      }

      render(
        <TriggerSelector
          config={eventConfig}
          onChange={() => {}}
          canUseEventTriggers={true}
        />
      )

      expect(screen.getByLabelText(/event type/i)).toBeInTheDocument()
    })

    it('should call onChange when event type is changed', () => {
      const onChange = vi.fn()
      const eventConfig: TriggerFormState = {
        type: 'event',
        event: 'budget_updated',
      }

      render(
        <TriggerSelector
          config={eventConfig}
          onChange={onChange}
          canUseEventTriggers={true}
        />
      )

      fireEvent.change(screen.getByLabelText(/event type/i), {
        target: { value: 'transaction_created' },
      })

      expect(onChange).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'event',
          event: 'transaction_created',
        })
      )
    })

    it('should show all event type options', () => {
      const eventConfig: TriggerFormState = {
        type: 'event',
        event: 'budget_updated',
      }

      render(
        <TriggerSelector
          config={eventConfig}
          onChange={() => {}}
          canUseEventTriggers={true}
        />
      )

      const select = screen.getByLabelText(/event type/i)
      expect(select).toContainHTML('budget_updated')
      expect(select).toContainHTML('transaction_created')
      expect(select).toContainHTML('balance_changed')
      expect(select).toContainHTML('budget_period_end')
    })
  })

  describe('Free Tier Restrictions', () => {
    it('should disable event option when canUseEventTriggers is false', () => {
      render(
        <TriggerSelector
          config={defaultTriggerConfig}
          onChange={() => {}}
          canUseEventTriggers={false}
        />
      )

      expect(screen.getByRole('radio', { name: /event/i })).toBeDisabled()
    })

    it('should show upgrade message when event triggers are not available', () => {
      render(
        <TriggerSelector
          config={defaultTriggerConfig}
          onChange={() => {}}
          canUseEventTriggers={false}
        />
      )

      expect(screen.getByText(/premium/i)).toBeInTheDocument()
    })

    it('should not disable event option when canUseEventTriggers is true', () => {
      render(
        <TriggerSelector
          config={defaultTriggerConfig}
          onChange={() => {}}
          canUseEventTriggers={true}
        />
      )

      expect(screen.getByRole('radio', { name: /event/i })).not.toBeDisabled()
    })
  })

  describe('Type Switching', () => {
    it('should call onChange with schedule type when schedule is selected', () => {
      const onChange = vi.fn()
      const eventConfig: TriggerFormState = {
        type: 'event',
        event: 'budget_updated',
      }

      render(
        <TriggerSelector
          config={eventConfig}
          onChange={onChange}
          canUseEventTriggers={true}
        />
      )

      fireEvent.click(screen.getByRole('radio', { name: /scheduled/i }))

      expect(onChange).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'schedule',
        })
      )
    })

    it('should call onChange with event type when event is selected', () => {
      const onChange = vi.fn()

      render(
        <TriggerSelector
          config={defaultTriggerConfig}
          onChange={onChange}
          canUseEventTriggers={true}
        />
      )

      fireEvent.click(screen.getByRole('radio', { name: /event/i }))

      expect(onChange).toHaveBeenCalledWith(
        expect.objectContaining({
          type: 'event',
        })
      )
    })
  })

  describe('Disabled State', () => {
    it('should disable all controls when disabled prop is true', () => {
      render(
        <TriggerSelector
          config={defaultTriggerConfig}
          onChange={() => {}}
          canUseEventTriggers={true}
          disabled
        />
      )

      expect(screen.getByRole('radio', { name: /scheduled/i })).toBeDisabled()
      expect(screen.getByRole('radio', { name: /event/i })).toBeDisabled()
    })
  })

  describe('Accessibility', () => {
    it('should have accessible radio group', () => {
      render(
        <TriggerSelector
          config={defaultTriggerConfig}
          onChange={() => {}}
          canUseEventTriggers={true}
        />
      )

      expect(screen.getByRole('radiogroup', { name: /trigger type/i })).toBeInTheDocument()
    })
  })
})
