/**
 * EmojiInput Component Tests
 *
 * Tests for the emoji input component that allows users to select
 * custom emojis for their accounts.
 *
 * Issue #307: Account Customization
 * Issue #544: Add unit tests for account customization components
 */

import { describe, it, expect, mock } from 'bun:test'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { EmojiInput } from './EmojiInput'

describe('EmojiInput', () => {
  const mockOnChange = mock(() => {})

  describe('Initialization', () => {
    it('renders with no initial emoji', () => {
      render(<EmojiInput onChange={mockOnChange} />)

      const input = screen.getByLabelText('Emoji input')
      expect(input).toBeInTheDocument()
      expect(input).toHaveValue('')
    })

    it('renders with initial emoji value', () => {
      render(<EmojiInput value="💰" onChange={mockOnChange} />)

      const input = screen.getByLabelText('Emoji input')
      expect(input).toHaveValue('💰')
    })

    it('uses default placeholder when not provided', () => {
      render(<EmojiInput onChange={mockOnChange} />)

      const input = screen.getByLabelText('Emoji input')
      expect(input).toHaveAttribute('placeholder', 'Enter emoji')
    })

    it('uses custom placeholder when provided', () => {
      render(<EmojiInput onChange={mockOnChange} placeholder="Pick an emoji" />)

      const input = screen.getByLabelText('Emoji input')
      expect(input).toHaveAttribute('placeholder', 'Pick an emoji')
    })
  })

  describe('Input Behavior', () => {
    it('accepts emoji input', async () => {
      const user = userEvent.setup()
      render(<EmojiInput onChange={mockOnChange} />)

      const input = screen.getByLabelText('Emoji input')
      await user.type(input, '🏠')

      expect(mockOnChange).toHaveBeenCalledWith('🏠')
    })

    it('updates input value when typing', async () => {
      const user = userEvent.setup()
      render(<EmojiInput onChange={mockOnChange} />)

      const input = screen.getByLabelText('Emoji input')
      await user.type(input, '💵')

      expect(input).toHaveValue('💵')
    })

    it('converts empty string to null', async () => {
      const user = userEvent.setup()
      render(<EmojiInput value="💰" onChange={mockOnChange} />)

      const input = screen.getByLabelText('Emoji input')
      await user.clear(input)

      expect(mockOnChange).toHaveBeenCalledWith(null)
    })

    it('allows multiple characters up to 10', async () => {
      const user = userEvent.setup()
      render(<EmojiInput onChange={mockOnChange} />)

      const input = screen.getByLabelText('Emoji input')
      await user.type(input, '1234567890')

      expect(input).toHaveValue('1234567890')
    })

    it('is a text input', () => {
      render(<EmojiInput onChange={mockOnChange} />)

      const input = screen.getByLabelText('Emoji input')
      expect(input).toHaveAttribute('type', 'text')
    })
  })

  describe('Clear Button', () => {
    it('shows clear button when emoji is set', () => {
      render(<EmojiInput value="💰" onChange={mockOnChange} />)

      expect(screen.getByLabelText('Clear emoji')).toBeInTheDocument()
    })

    it('hides clear button when no emoji is set', () => {
      render(<EmojiInput value={null} onChange={mockOnChange} />)

      expect(screen.queryByLabelText('Clear emoji')).not.toBeInTheDocument()
    })

    it('clears emoji when clear button is clicked', async () => {
      const user = userEvent.setup()
      render(<EmojiInput value="💰" onChange={mockOnChange} />)

      const input = screen.getByLabelText('Emoji input')
      await user.click(screen.getByLabelText('Clear emoji'))

      expect(mockOnChange).toHaveBeenCalledWith(null)
      expect(input).toHaveValue('')
    })
  })

  describe('Suggested Emojis', () => {
    it('renders all 20 suggested emojis', () => {
      render(<EmojiInput onChange={mockOnChange} />)

      const suggestedButtons = screen.getAllByRole('button', { name: /Select emoji/ })
      expect(suggestedButtons).toHaveLength(20)
    })

    it('renders Suggestions label', () => {
      render(<EmojiInput onChange={mockOnChange} />)

      expect(screen.getByText('Suggestions')).toBeInTheDocument()
    })

    it('selects emoji when suggestion is clicked', async () => {
      const user = userEvent.setup()
      render(<EmojiInput onChange={mockOnChange} />)

      const moneyBagButton = screen.getByLabelText('Select emoji 💰')
      await user.click(moneyBagButton)

      expect(mockOnChange).toHaveBeenCalledWith('💰')
    })

    it('updates input value when suggestion is clicked', async () => {
      const user = userEvent.setup()
      render(<EmojiInput onChange={mockOnChange} />)

      const input = screen.getByLabelText('Emoji input')
      await user.click(screen.getByLabelText('Select emoji 🏦'))

      expect(input).toHaveValue('🏦')
    })

    it('highlights selected suggested emoji', () => {
      render(<EmojiInput value="💰" onChange={mockOnChange} />)

      const selectedButton = screen.getByLabelText('Select emoji 💰')
      expect(selectedButton).toHaveAttribute('aria-pressed', 'true')

      const unselectedButton = screen.getByLabelText('Select emoji 💵')
      expect(unselectedButton).toHaveAttribute('aria-pressed', 'false')
    })

    it('renders specific suggested emojis', () => {
      render(<EmojiInput onChange={mockOnChange} />)

      const expectedEmojis = [
        '💰', '💵', '💳', '🏦', '🏠',
        '🚗', '✈️', '🛒', '🎁', '☕',
        '🍽️', '🎓', '💼', '📈', '📉',
        '⭐', '❤️', '🔒', '🔓', '🐷',
      ]

      expectedEmojis.forEach((emoji) => {
        expect(screen.getByLabelText(`Select emoji ${emoji}`)).toBeInTheDocument()
      })
    })
  })

  describe('Preview', () => {
    it('shows emoji preview when value is set', () => {
      render(<EmojiInput value="💰" onChange={mockOnChange} />)

      const preview = screen.getByLabelText('Emoji preview')
      expect(preview).toBeInTheDocument()
      expect(preview).toHaveTextContent('💰')
    })

    it('hides preview when no value is set', () => {
      render(<EmojiInput value={null} onChange={mockOnChange} />)

      expect(screen.queryByLabelText('Emoji preview')).not.toBeInTheDocument()
    })

    it('updates preview when different emoji is selected', () => {
      const { rerender } = render(<EmojiInput value="💰" onChange={mockOnChange} />)

      // Verify initial preview
      expect(screen.getByLabelText('Emoji preview')).toHaveTextContent('💰')

      // Simulate prop change after selection
      rerender(<EmojiInput value="🏠" onChange={mockOnChange} />)

      expect(screen.getByLabelText('Emoji preview')).toHaveTextContent('🏠')
    })
  })

  describe('className prop', () => {
    it('applies additional className to container', () => {
      const { container } = render(
        <EmojiInput onChange={mockOnChange} className="custom-class" />
      )

      const containerDiv = container.firstChild
      expect(containerDiv).toHaveClass('custom-class')
    })
  })

  describe('Accessibility', () => {
    it('has aria-label for input', () => {
      render(<EmojiInput onChange={mockOnChange} />)

      expect(screen.getByLabelText('Emoji input')).toBeInTheDocument()
    })

    it('has aria-label for clear button', () => {
      render(<EmojiInput value="💰" onChange={mockOnChange} />)

      expect(screen.getByLabelText('Clear emoji')).toBeInTheDocument()
    })

    it('has aria-label for preview', () => {
      render(<EmojiInput value="💰" onChange={mockOnChange} />)

      expect(screen.getByLabelText('Emoji preview')).toBeInTheDocument()
    })

    it('has aria-pressed on suggested emoji buttons', () => {
      render(<EmojiInput value="💰" onChange={mockOnChange} />)

      const buttons = screen.getAllByRole('button', { name: /Select emoji/ })
      buttons.forEach((button) => {
        expect(button).toHaveAttribute('aria-pressed')
      })
    })
  })
})
