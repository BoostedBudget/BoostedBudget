/**
 * CronBuilder Component Tests
 * Issue #39: Rule Builder UI
 *
 * Tests for the cron schedule builder with presets and custom input.
 * TDD RED: Writing tests before implementation.
 */

import { describe, it, expect, mock } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { CronBuilder } from './CronBuilder'
import { CRON_PRESETS, COMMON_TIMEZONES } from '../constants'

describe('CronBuilder', () => {
  describe('Rendering', () => {
    it('should render preset buttons', () => {
      render(<CronBuilder cron="0 9 * * *" onChange={() => {}} />)

      CRON_PRESETS.forEach((preset) => {
        expect(screen.getByRole('button', { name: preset.label })).toBeInTheDocument()
      })
    })

    it('should render timezone selector', () => {
      render(<CronBuilder cron="0 9 * * *" onChange={() => {}} />)

      expect(screen.getByLabelText(/timezone/i)).toBeInTheDocument()
    })

    it('should render custom cron toggle', () => {
      render(<CronBuilder cron="0 9 * * *" onChange={() => {}} />)

      expect(screen.getByRole('button', { name: /custom/i })).toBeInTheDocument()
    })

    it('should show description for the current cron expression', () => {
      render(<CronBuilder cron="0 9 * * *" onChange={() => {}} />)

      expect(screen.getByText(/every day at 9:00 am/i)).toBeInTheDocument()
    })
  })

  describe('Preset Selection', () => {
    it('should highlight the matching preset button', () => {
      render(<CronBuilder cron="0 9 * * *" onChange={() => {}} />)

      const dailyButton = screen.getByRole('button', { name: 'Daily at 9am' })
      expect(dailyButton).toHaveAttribute('aria-pressed', 'true')
    })

    it('should call onChange when a preset is clicked', () => {
      const onChange = mock(() => {})
      render(<CronBuilder cron="0 9 * * *" onChange={onChange} />)

      fireEvent.click(screen.getByRole('button', { name: 'Weekly (Monday)' }))

      expect(onChange).toHaveBeenCalledWith('0 9 * * 1')
    })

    it('should update description when preset changes', () => {
      const { rerender } = render(<CronBuilder cron="0 9 * * *" onChange={() => {}} />)

      expect(screen.getByText(/every day at 9:00 am/i)).toBeInTheDocument()

      rerender(<CronBuilder cron="0 9 * * 1" onChange={() => {}} />)

      expect(screen.getByText(/every monday at 9:00 am/i)).toBeInTheDocument()
    })
  })

  describe('Custom Cron Input', () => {
    it('should show custom input when custom button is clicked', () => {
      render(<CronBuilder cron="0 9 * * *" onChange={() => {}} />)

      fireEvent.click(screen.getByRole('button', { name: /custom/i }))

      expect(screen.getByRole('textbox', { name: /cron expression/i })).toBeInTheDocument()
    })

    it('should display current cron in the input', () => {
      render(<CronBuilder cron="30 14 1,15 * *" onChange={() => {}} />)

      fireEvent.click(screen.getByRole('button', { name: /custom/i }))

      const input = screen.getByRole('textbox', { name: /cron expression/i })
      expect(input).toHaveValue('30 14 1,15 * *')
    })

    it('should call onChange when custom cron is entered', () => {
      const onChange = mock(() => {})
      render(<CronBuilder cron="0 9 * * *" onChange={onChange} />)

      fireEvent.click(screen.getByRole('button', { name: /custom/i }))

      const input = screen.getByRole('textbox', { name: /cron expression/i })
      fireEvent.change(input, { target: { value: '0 12 * * *' } })
      fireEvent.blur(input)

      expect(onChange).toHaveBeenCalledWith('0 12 * * *')
    })

    it('should show error for invalid cron expression', () => {
      render(<CronBuilder cron="invalid" onChange={() => {}} />)

      fireEvent.click(screen.getByRole('button', { name: /custom/i }))

      // Look specifically for the error alert
      expect(screen.getByRole('alert')).toHaveTextContent(/invalid cron expression/i)
    })

    it('should hide custom input when hide button is clicked', () => {
      render(<CronBuilder cron="0 9 * * *" onChange={() => {}} />)

      // Show custom input
      fireEvent.click(screen.getByRole('button', { name: /custom/i }))
      expect(screen.getByRole('textbox', { name: /cron expression/i })).toBeInTheDocument()

      // Hide custom input
      fireEvent.click(screen.getByRole('button', { name: /hide custom/i }))
      expect(screen.queryByRole('textbox', { name: /cron expression/i })).not.toBeInTheDocument()
    })
  })

  describe('Timezone Selection', () => {
    it('should show common timezones in selector', () => {
      render(
        <CronBuilder
          cron="0 9 * * *"
          timezone="America/New_York"
          onChange={() => {}}
          onTimezoneChange={() => {}}
        />
      )

      const select = screen.getByLabelText(/timezone/i)
      fireEvent.click(select)

      COMMON_TIMEZONES.forEach((tz) => {
        expect(screen.getByText(tz.label)).toBeInTheDocument()
      })
    })

    it('should call onTimezoneChange when timezone is selected', () => {
      const onTimezoneChange = mock(() => {})
      render(
        <CronBuilder
          cron="0 9 * * *"
          timezone="America/New_York"
          onChange={() => {}}
          onTimezoneChange={onTimezoneChange}
        />
      )

      const select = screen.getByLabelText(/timezone/i)
      fireEvent.change(select, { target: { value: 'America/Los_Angeles' } })

      expect(onTimezoneChange).toHaveBeenCalledWith('America/Los_Angeles')
    })

    it('should show current timezone as selected', () => {
      render(
        <CronBuilder
          cron="0 9 * * *"
          timezone="America/Los_Angeles"
          onChange={() => {}}
          onTimezoneChange={() => {}}
        />
      )

      const select = screen.getByLabelText(/timezone/i) as HTMLSelectElement
      expect(select.value).toBe('America/Los_Angeles')
    })
  })

  describe('Disabled State', () => {
    it('should disable all controls when disabled prop is true', () => {
      render(<CronBuilder cron="0 9 * * *" onChange={() => {}} disabled />)

      const buttons = screen.getAllByRole('button')
      buttons.forEach((button) => {
        expect(button).toBeDisabled()
      })

      expect(screen.getByLabelText(/timezone/i)).toBeDisabled()
    })
  })

  describe('Accessibility', () => {
    it('should have accessible labels for all controls', () => {
      render(<CronBuilder cron="0 9 * * *" onChange={() => {}} />)

      expect(screen.getByRole('group', { name: /schedule/i })).toBeInTheDocument()
      expect(screen.getByLabelText(/timezone/i)).toBeInTheDocument()
    })
  })
})
