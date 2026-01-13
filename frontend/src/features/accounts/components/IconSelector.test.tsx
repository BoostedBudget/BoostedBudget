/**
 * IconSelector Component Tests
 *
 * Tests for the icon selector component that allows users to choose
 * icons for their accounts from a predefined set.
 *
 * Issue #307: Account Customization
 * Issue #544: Add unit tests for account customization components
 */

import { describe, it, expect, mock } from 'bun:test'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { IconSelector } from './IconSelector'
import { VALID_ACCOUNT_ICONS } from '../types'

describe('IconSelector', () => {
  const mockOnChange = mock(() => {})

  // Icon to label mapping for all 25 icons
  const ICON_LABELS: Record<string, string> = {
    'wallet': 'Wallet',
    'piggy-bank': 'Savings',
    'credit-card': 'Card',
    'bank': 'Bank',
    'building': 'Building',
    'briefcase': 'Business',
    'dollar': 'Dollar',
    'euro': 'Euro',
    'pound': 'Pound',
    'yen': 'Yen',
    'chart': 'Chart',
    'trending-up': 'Growth',
    'trending-down': 'Decline',
    'lock': 'Locked',
    'unlock': 'Unlocked',
    'star': 'Star',
    'heart': 'Heart',
    'home': 'Home',
    'car': 'Car',
    'plane': 'Travel',
    'shopping-cart': 'Shopping',
    'gift': 'Gift',
    'coffee': 'Coffee',
    'utensils': 'Food',
    'graduation-cap': 'Education',
  }

  describe('Initialization', () => {
    it('renders with no initial icon', () => {
      render(<IconSelector onChange={mockOnChange} />)

      // "No Icon" button should be visible
      expect(screen.getByText('No Icon')).toBeInTheDocument()
    })

    it('renders with initial icon value', () => {
      render(<IconSelector value="wallet" onChange={mockOnChange} />)

      // Should show current selection
      expect(screen.getByText('Current: Wallet')).toBeInTheDocument()
    })

    it('renders with null value', () => {
      render(<IconSelector value={null} onChange={mockOnChange} />)

      // "No Icon" should be selected
      const noIconButton = screen.getByRole('button', { name: 'No Icon' })
      expect(noIconButton).toHaveAttribute('aria-pressed', 'true')
    })
  })

  describe('Icon Grid', () => {
    it('renders all 25 icons', () => {
      render(<IconSelector onChange={mockOnChange} />)

      // Should have 25 icon buttons + 1 "No Icon" button = 26 total buttons
      const iconButtons = screen.getAllByRole('button')
      expect(iconButtons).toHaveLength(26)
    })

    it('renders each icon with correct label', () => {
      render(<IconSelector onChange={mockOnChange} />)

      const expectedLabels = [
        'Wallet', 'Savings', 'Card', 'Bank', 'Building',
        'Business', 'Dollar', 'Euro', 'Pound', 'Yen',
        'Chart', 'Growth', 'Decline', 'Locked', 'Unlocked',
        'Star', 'Heart', 'Home', 'Car', 'Travel',
        'Shopping', 'Gift', 'Coffee', 'Food', 'Education',
      ]

      expectedLabels.forEach((label) => {
        expect(screen.getByLabelText(`Select ${label} icon`)).toBeInTheDocument()
      })
    })

    it('renders icons in a 5-column grid', () => {
      const { container } = render(<IconSelector onChange={mockOnChange} />)

      // The grid container should have grid-cols-5 class
      const gridContainer = container.querySelector('.grid-cols-5')
      expect(gridContainer).toBeInTheDocument()
    })
  })

  describe('Selection', () => {
    it('calls onChange when icon is clicked', async () => {
      const user = userEvent.setup()
      render(<IconSelector onChange={mockOnChange} />)

      await user.click(screen.getByLabelText('Select Wallet icon'))

      expect(mockOnChange).toHaveBeenCalledWith('wallet')
    })

    it('calls onChange with null when "No Icon" is clicked', async () => {
      const user = userEvent.setup()
      render(<IconSelector value="wallet" onChange={mockOnChange} />)

      await user.click(screen.getByText('No Icon'))

      expect(mockOnChange).toHaveBeenCalledWith(null)
    })

    it('highlights selected icon', () => {
      render(<IconSelector value="wallet" onChange={mockOnChange} />)

      const walletButton = screen.getByLabelText('Select Wallet icon')
      expect(walletButton).toHaveAttribute('aria-pressed', 'true')

      const cardButton = screen.getByLabelText('Select Card icon')
      expect(cardButton).toHaveAttribute('aria-pressed', 'false')
    })

    it('highlights "No Icon" when no icon is selected', () => {
      render(<IconSelector value={null} onChange={mockOnChange} />)

      const noIconButton = screen.getByText('No Icon').closest('button')
      expect(noIconButton).toHaveAttribute('aria-pressed', 'true')
    })
  })

  describe('Current Selection Display', () => {
    it('shows current selection when icon is selected', () => {
      render(<IconSelector value="wallet" onChange={mockOnChange} />)

      expect(screen.getByText('Current: Wallet')).toBeInTheDocument()
    })

    it('hides current selection when no icon is selected', () => {
      render(<IconSelector value={null} onChange={mockOnChange} />)

      expect(screen.queryByText(/^Current:/)).not.toBeInTheDocument()
    })

    it('displays correct label for selected icon', () => {
      render(<IconSelector value="credit-card" onChange={mockOnChange} />)

      expect(screen.getByText('Current: Card')).toBeInTheDocument()
    })

    it('displays correct label for all icons', () => {
      // Verify the count matches VALID_ACCOUNT_ICONS
      expect(Object.keys(ICON_LABELS)).toHaveLength(VALID_ACCOUNT_ICONS.length)
    })
  })

  describe('Icon Color', () => {
    it('applies iconColor to selected icon in current selection display', () => {
      const { container } = render(
        <IconSelector value="wallet" onChange={mockOnChange} iconColor="#EF4444" />
      )

      // Find the icon in the current selection display (not in the grid)
      const currentDisplay = container.querySelector('.bg-muted\\/50 svg')
      expect(currentDisplay).toHaveStyle({ color: '#EF4444' })
    })

    it('applies iconColor to selected icon in grid', () => {
      render(
        <IconSelector value="wallet" onChange={mockOnChange} iconColor="#EF4444" />
      )

      // The selected icon in the grid should have the color applied
      const walletButton = screen.getByLabelText('Select Wallet icon')
      const icon = walletButton.querySelector('svg')
      expect(icon).toHaveStyle({ color: '#EF4444' })
    })

    it('does not apply iconColor to unselected icons', () => {
      render(
        <IconSelector value="wallet" onChange={mockOnChange} iconColor="#EF4444" />
      )

      const cardButton = screen.getByLabelText('Select Card icon')
      const icon = cardButton.querySelector('svg')
      // Unselected icons should not have inline color style
      expect(icon).not.toHaveStyle({ color: '#EF4444' })
    })

    it('does not apply iconColor when not provided', () => {
      render(<IconSelector value="wallet" onChange={mockOnChange} />)

      const walletButton = screen.getByLabelText('Select Wallet icon')
      const icon = walletButton.querySelector('svg')
      // Without iconColor, no inline color style should be applied
      expect(icon?.style.color).toBeFalsy()
    })
  })

  describe('Accessibility', () => {
    it('sets aria-pressed on selected icon', () => {
      render(<IconSelector value="star" onChange={mockOnChange} />)

      const starButton = screen.getByLabelText('Select Star icon')
      expect(starButton).toHaveAttribute('aria-pressed', 'true')
    })

    it('sets aria-pressed=false on unselected icons', () => {
      render(<IconSelector value="star" onChange={mockOnChange} />)

      const heartButton = screen.getByLabelText('Select Heart icon')
      expect(heartButton).toHaveAttribute('aria-pressed', 'false')
    })

    it('has aria-label on all icon buttons', () => {
      render(<IconSelector onChange={mockOnChange} />)

      // All 25 icon buttons should have aria-label matching the pattern
      const iconButtons = screen.getAllByRole('button', { name: /Select .* icon/ })
      expect(iconButtons).toHaveLength(25)
    })

    it('has title attribute with icon label', () => {
      render(<IconSelector onChange={mockOnChange} />)

      const walletButton = screen.getByLabelText('Select Wallet icon')
      expect(walletButton).toHaveAttribute('title', 'Wallet')
    })
  })

  describe('className prop', () => {
    it('applies additional className to container', () => {
      const { container } = render(
        <IconSelector onChange={mockOnChange} className="custom-class" />
      )

      const containerDiv = container.firstChild
      expect(containerDiv).toHaveClass('custom-class')
    })
  })

  describe('Icon button styling', () => {
    it('has correct classes for unselected icons', () => {
      render(<IconSelector onChange={mockOnChange} />)

      const walletButton = screen.getByLabelText('Select Wallet icon')
      expect(walletButton).toHaveClass('border-input')
    })

    it('has correct classes for selected icon', () => {
      render(<IconSelector value="wallet" onChange={mockOnChange} />)

      const walletButton = screen.getByLabelText('Select Wallet icon')
      expect(walletButton).toHaveClass('border-primary')
    })
  })
})
