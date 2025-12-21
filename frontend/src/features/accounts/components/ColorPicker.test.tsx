/**
 * ColorPicker Component Tests
 *
 * Tests for the color picker component that allows users to select
 * custom colors for their accounts.
 *
 * Issue #307: Account Customization
 * Issue #544: Add unit tests for account customization components
 */

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ColorPicker } from './ColorPicker'

// Mock react-colorful as it uses canvas which isn't available in jsdom
vi.mock('react-colorful', () => ({
  HexColorPicker: ({
    color,
    onChange,
    style,
  }: {
    color: string
    onChange: (color: string) => void
    style?: React.CSSProperties
  }) => (
    <div
      data-testid="hex-color-picker"
      data-color={color}
      style={style}
      onClick={() => onChange('#FF0000')}
    />
  ),
}))

describe('ColorPicker', () => {
  const mockOnChange = vi.fn()

  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('Initialization', () => {
    it('renders with no initial color', () => {
      render(<ColorPicker onChange={mockOnChange} />)

      // Should show the picker
      expect(screen.getByTestId('hex-color-picker')).toBeInTheDocument()
      // Should show the hex input
      expect(screen.getByLabelText('Hex color input')).toBeInTheDocument()
    })

    it('renders with initial color value', () => {
      render(<ColorPicker value="#EF4444" onChange={mockOnChange} />)

      // Hex input should show the color value
      const hexInput = screen.getByLabelText('Hex color input')
      expect(hexInput).toHaveValue('#EF4444')
    })

    it('initializes internal color to default when value is null', () => {
      render(<ColorPicker value={null} onChange={mockOnChange} />)

      // Should use default blue color (#3B82F6) when value is null
      const hexInput = screen.getByLabelText('Hex color input')
      expect(hexInput).toHaveValue('#3B82F6')
    })

    it('shows uppercase hex value', () => {
      render(<ColorPicker value="#ef4444" onChange={mockOnChange} />)

      const hexInput = screen.getByLabelText('Hex color input')
      expect(hexInput).toHaveValue('#EF4444')
    })
  })

  describe('Color Selection', () => {
    it('calls onChange when color is selected via picker', async () => {
      const user = userEvent.setup()
      render(<ColorPicker onChange={mockOnChange} />)

      // Click the mocked color picker (simulates selecting a color)
      await user.click(screen.getByTestId('hex-color-picker'))

      expect(mockOnChange).toHaveBeenCalledWith('#FF0000')
    })

    it('calls onChange when preset color is clicked', async () => {
      const user = userEvent.setup()
      render(<ColorPicker onChange={mockOnChange} />)

      // Click the first preset color (red #EF4444)
      const redPreset = screen.getByLabelText('Select color #EF4444')
      await user.click(redPreset)

      expect(mockOnChange).toHaveBeenCalledWith('#EF4444')
    })

    it('highlights selected preset color', () => {
      render(<ColorPicker value="#EF4444" onChange={mockOnChange} />)

      const redPreset = screen.getByLabelText('Select color #EF4444')
      expect(redPreset).toHaveAttribute('aria-pressed', 'true')

      const bluePreset = screen.getByLabelText('Select color #3B82F6')
      expect(bluePreset).toHaveAttribute('aria-pressed', 'false')
    })
  })

  describe('Hex Input', () => {
    it('displays current color in hex input', () => {
      render(<ColorPicker value="#EF4444" onChange={mockOnChange} />)

      const hexInput = screen.getByLabelText('Hex color input')
      expect(hexInput).toHaveValue('#EF4444')
    })

    it('only accepts valid hex format pattern', () => {
      // The input has a regex check: /^#[0-9A-Fa-f]{6}$/
      render(<ColorPicker value="#3B82F6" onChange={mockOnChange} />)

      const hexInput = screen.getByLabelText('Hex color input')
      // Verify it's a text input for hex colors
      expect(hexInput).toHaveAttribute('type', 'text')
      expect(hexInput).toHaveAttribute('placeholder', '#RRGGBB')
    })

    it('has maxLength of 7 characters', () => {
      render(<ColorPicker onChange={mockOnChange} />)

      const hexInput = screen.getByLabelText('Hex color input')
      expect(hexInput).toHaveAttribute('maxLength', '7')
    })

    it('displays uppercase hex value', () => {
      render(<ColorPicker value="#abcdef" onChange={mockOnChange} />)

      const hexInput = screen.getByLabelText('Hex color input')
      // Component always displays uppercase
      expect(hexInput).toHaveValue('#ABCDEF')
    })

    it('has monospace font for hex input', () => {
      render(<ColorPicker onChange={mockOnChange} />)

      const hexInput = screen.getByLabelText('Hex color input')
      expect(hexInput).toHaveClass('font-mono')
    })

    it('calls onChange when valid hex is entered via fireEvent', () => {
      render(<ColorPicker onChange={mockOnChange} />)

      const hexInput = screen.getByLabelText('Hex color input')
      // Use fireEvent.change to simulate directly entering a valid hex value
      fireEvent.change(hexInput, { target: { value: '#ABCDEF' } })

      expect(mockOnChange).toHaveBeenCalledWith('#ABCDEF')
    })

    it('does not call onChange when invalid hex is entered', () => {
      render(<ColorPicker onChange={mockOnChange} />)

      const hexInput = screen.getByLabelText('Hex color input')
      // Use fireEvent.change with invalid hex
      fireEvent.change(hexInput, { target: { value: '#GGG' } })

      // Should not call onChange for invalid hex
      expect(mockOnChange).not.toHaveBeenCalled()
    })
  })

  describe('Clear Button', () => {
    it('shows clear button when color is set', () => {
      render(<ColorPicker value="#EF4444" onChange={mockOnChange} />)

      expect(screen.getByLabelText('Clear color')).toBeInTheDocument()
    })

    it('hides clear button when no color is set', () => {
      render(<ColorPicker value={null} onChange={mockOnChange} />)

      expect(screen.queryByLabelText('Clear color')).not.toBeInTheDocument()
    })

    it('hides clear button when value is undefined', () => {
      render(<ColorPicker onChange={mockOnChange} />)

      expect(screen.queryByLabelText('Clear color')).not.toBeInTheDocument()
    })

    it('calls onChange with null when cleared', async () => {
      const user = userEvent.setup()
      render(<ColorPicker value="#EF4444" onChange={mockOnChange} />)

      await user.click(screen.getByLabelText('Clear color'))

      expect(mockOnChange).toHaveBeenCalledWith(null)
    })
  })

  describe('Preset Colors', () => {
    it('renders all 18 preset colors', () => {
      render(<ColorPicker onChange={mockOnChange} />)

      // Should have 18 preset buttons
      const presetButtons = screen.getAllByRole('button', { name: /Select color #/ })
      expect(presetButtons).toHaveLength(18)
    })

    it('renders Presets label', () => {
      render(<ColorPicker onChange={mockOnChange} />)

      expect(screen.getByText('Presets')).toBeInTheDocument()
    })

    it('updates internal color when preset is clicked', async () => {
      const user = userEvent.setup()
      render(<ColorPicker onChange={mockOnChange} />)

      const greenPreset = screen.getByLabelText('Select color #22C55E')
      await user.click(greenPreset)

      // Should highlight the clicked preset
      expect(greenPreset).toHaveAttribute('aria-pressed', 'true')
    })
  })

  describe('Color Preview', () => {
    it('shows color preview box', () => {
      render(<ColorPicker value="#EF4444" onChange={mockOnChange} />)

      const preview = screen.getByLabelText('Current color preview')
      expect(preview).toBeInTheDocument()
      expect(preview).toHaveStyle({ backgroundColor: '#EF4444' })
    })

    it('shows default color in preview when value is null', () => {
      render(<ColorPicker value={null} onChange={mockOnChange} />)

      const preview = screen.getByLabelText('Current color preview')
      expect(preview).toHaveStyle({ backgroundColor: '#3B82F6' })
    })
  })

  describe('Accessibility', () => {
    it('uses custom aria-label when provided', () => {
      render(
        <ColorPicker
          onChange={mockOnChange}
          aria-label="Choose account color"
        />
      )

      expect(screen.getByLabelText('Choose account color')).toBeInTheDocument()
    })

    it('uses default aria-label when not provided', () => {
      render(<ColorPicker onChange={mockOnChange} />)

      expect(screen.getByLabelText('Pick a color')).toBeInTheDocument()
    })

    it('has accessible labels for all preset buttons', () => {
      render(<ColorPicker onChange={mockOnChange} />)

      const presets = [
        '#EF4444', '#F97316', '#F59E0B', '#EAB308', '#84CC16', '#22C55E',
        '#10B981', '#14B8A6', '#06B6D4', '#0EA5E9', '#3B82F6', '#6366F1',
        '#8B5CF6', '#A855F7', '#D946EF', '#EC4899', '#F43F5E', '#64748B',
      ]

      presets.forEach((color) => {
        expect(screen.getByLabelText(`Select color ${color}`)).toBeInTheDocument()
      })
    })
  })

  describe('className prop', () => {
    it('applies additional className to container', () => {
      render(
        <ColorPicker
          onChange={mockOnChange}
          className="custom-class"
        />
      )

      const container = screen.getByLabelText('Pick a color')
      expect(container).toHaveClass('custom-class')
    })
  })
})
