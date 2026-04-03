/**
 * TriggerSelector Component Tests
 * Issue #39: Rule Builder UI
 *
 * Tests for the trigger type selector (schedule vs event).
 * TDD RED: Writing tests before implementation.
 */

import { describe, it, expect, mock } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { TriggerSelector } from './TriggerSelector'
import type { TriggerFormState } from '../types'

const defaultTriggerConfig: TriggerFormState = {
  type: 'schedule',
  cron: '0 9 * * *',
  timezone: 'America/New_York',
} as const

describe('TriggerSelector', () => {
  describe('Rendering', () => {
    it('should render schedule and event options', () => {
      render(
        <TriggerSelector
          config={defaultTriggerConfig}
          onChange={() => {}}
        />
      )

      expect(screen.getByRole('radio', { name: /scheduled/i })).toBeInTheDocument()
      expect(screen.getByRole('radio', { name: /event/i })).toBeInTheDocument()
    })

    it('should show schedule as selected by default', () => {
      render(
        <TriggerSelector
          config={defaultTriggerConfig}
          onChange={() => {}}
        />
      )

      expect(screen.getByRole('radio', { name: /scheduled/i })).toBeChecked()
      expect(screen.getByRole('radio', { name: /event/i })).not.toBeChecked()
    })

    it('should show event as selected when config type is event', () => {
      const eventConfig: TriggerFormState = {
        type: 'event',
        event: 'budget_updated',
      }

      render(
        <TriggerSelector
          config={eventConfig}
          onChange={() => {}}
        />
      )

      expect(screen.getByRole('radio', { name: /event/i })).toBeChecked()
      expect(screen.getByRole('radio', { name: /scheduled/i })).not.toBeChecked()
    })

    it('should show CronBuilder when schedule is selected', () => {
      render(
        <TriggerSelector
          config={defaultTriggerConfig}
          onChange={() => {}}
        />
      )

      // CronBuilder renders a cron input field
      expect(screen.getByRole('group')).toBeInTheDocument()
    })

    it('should show event select when event type is selected', () => {
      const eventConfig: TriggerFormState = {
        type: 'event',
        event: 'budget_updated',
      }

      render(
        <TriggerSelector
          config={eventConfig}
          onChange={() => {}}
        />
      )

      expect(screen.getByRole('combobox')).toBeInTheDocument()
    })
  })

  describe('Event Options', () => {
    it('should render all event type options', () => {
      const eventConfig: TriggerFormState = {
        type: 'event',
        event: 'budget_updated',
      }

      render(
        <TriggerSelector
          config={eventConfig}
          onChange={() => {}}
        />
      )

      const select = screen.getByRole('combobox')
      expect(select).toContainHTML('budget_updated')
      expect(select).toContainHTML('transaction_created')
      expect(select).toContainHTML('balance_changed')
      expect(select).toContainHTML('budget_period_end')
    })
  })

  describe('Type Switching', () => {
    it('should call onChange with schedule type when schedule is selected', () => {
      const onChange = mock(() => {})
      const eventConfig: TriggerFormState = {
        type: 'event',
        event: 'budget_updated',
      }

      render(
        <TriggerSelector
          config={eventConfig}
          onChange={onChange}
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
      const onChange = mock(() => {})

      render(
        <TriggerSelector
          config={defaultTriggerConfig}
          onChange={onChange}
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
        />
      )

      expect(screen.getByRole('radiogroup', { name: /trigger type/i })).toBeInTheDocument()
    })
  })
})
