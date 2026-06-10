/**
 * Switch component tests
 * Issue #1267 R2-A: shared primitive for on/off boolean controls
 * TDD RED: written before implementation
 */
import { describe, it, expect, mock } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { Switch } from './switch'

describe('Switch', () => {
  describe('Rendering', () => {
    it('renders with role="switch"', () => {
      render(<Switch checked={false} onCheckedChange={() => {}} />)
      expect(screen.getByRole('switch')).toBeInTheDocument()
    })

    it('sets aria-checked to true when checked', () => {
      render(<Switch checked={true} onCheckedChange={() => {}} />)
      expect(screen.getByRole('switch')).toHaveAttribute('aria-checked', 'true')
    })

    it('sets aria-checked to false when unchecked', () => {
      render(<Switch checked={false} onCheckedChange={() => {}} />)
      expect(screen.getByRole('switch')).toHaveAttribute('aria-checked', 'false')
    })

    it('applies bg-success class when checked', () => {
      render(<Switch checked={true} onCheckedChange={() => {}} />)
      const track = screen.getByRole('switch')
      expect(track.className).toContain('bg-success')
    })

    it('does not apply bg-success when unchecked', () => {
      render(<Switch checked={false} onCheckedChange={() => {}} />)
      const track = screen.getByRole('switch')
      expect(track.className).not.toContain('bg-success')
    })

    it('merges custom className', () => {
      render(<Switch checked={false} onCheckedChange={() => {}} className="my-custom" />)
      expect(screen.getByRole('switch').className).toContain('my-custom')
    })
  })

  describe('Interaction', () => {
    it('calls onCheckedChange with true when toggled from off', () => {
      const handler = mock((_v: boolean) => {})
      render(<Switch checked={false} onCheckedChange={handler} />)
      fireEvent.click(screen.getByRole('switch'))
      expect(handler).toHaveBeenCalledWith(true)
    })

    it('calls onCheckedChange with false when toggled from on', () => {
      const handler = mock((_v: boolean) => {})
      render(<Switch checked={true} onCheckedChange={handler} />)
      fireEvent.click(screen.getByRole('switch'))
      expect(handler).toHaveBeenCalledWith(false)
    })

    it('does not call onCheckedChange when disabled', () => {
      const handler = mock((_v: boolean) => {})
      render(<Switch checked={false} onCheckedChange={handler} disabled />)
      fireEvent.click(screen.getByRole('switch'))
      expect(handler).not.toHaveBeenCalled()
    })
  })

  describe('Disabled state', () => {
    it('has disabled attribute when disabled=true', () => {
      render(<Switch checked={false} onCheckedChange={() => {}} disabled />)
      expect(screen.getByRole('switch')).toBeDisabled()
    })

    it('applies opacity class when disabled', () => {
      render(<Switch checked={false} onCheckedChange={() => {}} disabled />)
      expect(screen.getByRole('switch').className).toContain('disabled:opacity-50')
    })
  })

  describe('Accessibility', () => {
    it('is keyboard-activatable via Enter key', () => {
      const handler = mock((_v: boolean) => {})
      render(<Switch checked={false} onCheckedChange={handler} />)
      fireEvent.keyDown(screen.getByRole('switch'), { key: 'Enter' })
      expect(handler).toHaveBeenCalledWith(true)
    })

    it('is keyboard-activatable via Space key', () => {
      const handler = mock((_v: boolean) => {})
      render(<Switch checked={false} onCheckedChange={handler} />)
      fireEvent.keyDown(screen.getByRole('switch'), { key: ' ' })
      expect(handler).toHaveBeenCalledWith(true)
    })

    it('renders thumb indicator inside track', () => {
      const { container } = render(<Switch checked={false} onCheckedChange={() => {}} />)
      // A thumb span should be inside the switch button
      expect(container.querySelector('[data-slot="switch-thumb"]')).toBeInTheDocument()
    })
  })
})
