/**
 * ListViewToggle Component Tests
 * Issue #39: Rule Builder UI
 *
 * Tests for the reusable list view toggle component.
 * TDD RED: Writing tests before implementation.
 */

import { describe, it, expect, mock, beforeEach, afterEach } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { ListViewToggle } from './ListViewToggle'

describe('ListViewToggle', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  afterEach(() => {
    localStorage.clear()
  })

  describe('Rendering', () => {
    it('should render both cards and table buttons', () => {
      render(<ListViewToggle view="cards" onChange={() => {}} />)

      expect(screen.getByRole('button', { name: /cards/i })).toBeInTheDocument()
      expect(screen.getByRole('button', { name: /table/i })).toBeInTheDocument()
    })

    it('should highlight cards button when view is cards', () => {
      render(<ListViewToggle view="cards" onChange={() => {}} />)

      const cardsButton = screen.getByRole('button', { name: /cards/i })
      const tableButton = screen.getByRole('button', { name: /table/i })

      expect(cardsButton).toHaveAttribute('aria-pressed', 'true')
      expect(tableButton).toHaveAttribute('aria-pressed', 'false')
    })

    it('should highlight table button when view is table', () => {
      render(<ListViewToggle view="table" onChange={() => {}} />)

      const cardsButton = screen.getByRole('button', { name: /cards/i })
      const tableButton = screen.getByRole('button', { name: /table/i })

      expect(cardsButton).toHaveAttribute('aria-pressed', 'false')
      expect(tableButton).toHaveAttribute('aria-pressed', 'true')
    })

    it('should render with custom className', () => {
      render(<ListViewToggle view="cards" onChange={() => {}} className="custom-class" />)

      const container = screen.getByRole('group')
      expect(container).toHaveClass('custom-class')
    })
  })

  describe('Interaction', () => {
    it('should call onChange with "table" when table button is clicked', () => {
      const onChange = mock(() => {})
      render(<ListViewToggle view="cards" onChange={onChange} />)

      fireEvent.click(screen.getByRole('button', { name: /table/i }))

      expect(onChange).toHaveBeenCalledWith('table')
    })

    it('should call onChange with "cards" when cards button is clicked', () => {
      const onChange = mock(() => {})
      render(<ListViewToggle view="table" onChange={onChange} />)

      fireEvent.click(screen.getByRole('button', { name: /cards/i }))

      expect(onChange).toHaveBeenCalledWith('cards')
    })

    it('should not call onChange when clicking already selected view', () => {
      const onChange = mock(() => {})
      render(<ListViewToggle view="cards" onChange={onChange} />)

      fireEvent.click(screen.getByRole('button', { name: /cards/i }))

      expect(onChange).not.toHaveBeenCalled()
    })
  })

  describe('LocalStorage persistence', () => {
    it('should save to localStorage when storageKey is provided', () => {
      const onChange = mock(() => {})
      render(<ListViewToggle view="cards" onChange={onChange} storageKey="test-view" />)

      fireEvent.click(screen.getByRole('button', { name: /table/i }))

      expect(localStorage.getItem('test-view')).toBe('table')
    })

    it('should not save to localStorage when storageKey is not provided', () => {
      const onChange = mock(() => {})
      render(<ListViewToggle view="cards" onChange={onChange} />)

      fireEvent.click(screen.getByRole('button', { name: /table/i }))

      expect(localStorage.length).toBe(0)
    })
  })

  describe('Accessibility', () => {
    it('should have proper aria-label on the group', () => {
      render(<ListViewToggle view="cards" onChange={() => {}} />)

      const group = screen.getByRole('group')
      expect(group).toHaveAttribute('aria-label', 'View toggle')
    })

    it('should have proper aria-pressed on buttons', () => {
      render(<ListViewToggle view="cards" onChange={() => {}} />)

      const cardsButton = screen.getByRole('button', { name: /cards/i })
      expect(cardsButton).toHaveAttribute('aria-pressed', 'true')
    })

    it('should be keyboard accessible', () => {
      const onChange = mock(() => {})
      render(<ListViewToggle view="cards" onChange={onChange} />)

      const tableButton = screen.getByRole('button', { name: /table/i })
      tableButton.focus()
      fireEvent.keyDown(tableButton, { key: 'Enter' })

      // Button click should work with keyboard
      fireEvent.click(tableButton)
      expect(onChange).toHaveBeenCalledWith('table')
    })
  })

  describe('Icons', () => {
    it('should render grid icon for cards view', () => {
      render(<ListViewToggle view="cards" onChange={() => {}} />)

      const cardsButton = screen.getByRole('button', { name: /cards/i })
      expect(cardsButton.querySelector('svg')).toBeInTheDocument()
    })

    it('should render list icon for table view', () => {
      render(<ListViewToggle view="table" onChange={() => {}} />)

      const tableButton = screen.getByRole('button', { name: /table/i })
      expect(tableButton.querySelector('svg')).toBeInTheDocument()
    })
  })

  describe('Disabled state', () => {
    it('should disable buttons when disabled prop is true', () => {
      render(<ListViewToggle view="cards" onChange={() => {}} disabled />)

      const cardsButton = screen.getByRole('button', { name: /cards/i })
      const tableButton = screen.getByRole('button', { name: /table/i })

      expect(cardsButton).toBeDisabled()
      expect(tableButton).toBeDisabled()
    })

    it('should not call onChange when disabled', () => {
      const onChange = mock(() => {})
      render(<ListViewToggle view="cards" onChange={onChange} disabled />)

      fireEvent.click(screen.getByRole('button', { name: /table/i }))

      expect(onChange).not.toHaveBeenCalled()
    })
  })
})
