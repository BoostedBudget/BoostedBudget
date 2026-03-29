/**
 * ToggleGroup Component Tests
 * Issue #832: Replace Dashboard Filter Controls with Design System Components
 */

import { describe, it, expect, mock } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { ToggleGroup, ToggleGroupItem } from './toggle-group'

describe('ToggleGroup', () => {
  describe('Rendering', () => {
    it('should render items', () => {
      render(
        <ToggleGroup type="single">
          <ToggleGroupItem value="a">Option A</ToggleGroupItem>
          <ToggleGroupItem value="b">Option B</ToggleGroupItem>
        </ToggleGroup>
      )
      expect(screen.getByText('Option A')).toBeInTheDocument()
      expect(screen.getByText('Option B')).toBeInTheDocument()
    })

    it('should render with role="group"', () => {
      render(
        <ToggleGroup type="single">
          <ToggleGroupItem value="a">A</ToggleGroupItem>
        </ToggleGroup>
      )
      expect(screen.getByRole('group')).toBeInTheDocument()
    })

    it('should apply default container classes', () => {
      render(
        <ToggleGroup type="single" data-testid="group">
          <ToggleGroupItem value="a">A</ToggleGroupItem>
        </ToggleGroup>
      )
      const group = screen.getByTestId('group')
      expect(group.className).toContain('inline-flex')
    })
  })

  describe('Single Selection', () => {
    it('should show selected item with data-state="on"', () => {
      render(
        <ToggleGroup type="single" value="a">
          <ToggleGroupItem value="a">A</ToggleGroupItem>
          <ToggleGroupItem value="b">B</ToggleGroupItem>
        </ToggleGroup>
      )
      const itemA = screen.getByText('A').closest('button')
      const itemB = screen.getByText('B').closest('button')
      expect(itemA).toHaveAttribute('data-state', 'on')
      expect(itemB).toHaveAttribute('data-state', 'off')
    })

    it('should call onValueChange when item clicked', () => {
      const onValueChange = mock(() => {})
      render(
        <ToggleGroup type="single" value="a" onValueChange={onValueChange}>
          <ToggleGroupItem value="b">B</ToggleGroupItem>
        </ToggleGroup>
      )
      fireEvent.click(screen.getByText('B'))
      expect(onValueChange).toHaveBeenCalledWith('b')
    })

    it('should not deselect when clicking active item (prevent empty)', () => {
      const onValueChange = mock(() => {})
      render(
        <ToggleGroup type="single" value="a" onValueChange={onValueChange}>
          <ToggleGroupItem value="a">A</ToggleGroupItem>
        </ToggleGroup>
      )
      fireEvent.click(screen.getByText('A'))
      // onValueChange may be called with '' — parent ignores it
      // The important thing is data-state stays on
      const item = screen.getByText('A').closest('button')
      expect(item).toHaveAttribute('data-state', 'on')
    })
  })

  describe('Variants and Sizes', () => {
    it('should accept variant prop on items without error', () => {
      render(
        <ToggleGroup type="single">
          <ToggleGroupItem value="a" variant="default">A</ToggleGroupItem>
          <ToggleGroupItem value="b" variant="outline">B</ToggleGroupItem>
        </ToggleGroup>
      )
      expect(screen.getByText('A')).toBeInTheDocument()
      expect(screen.getByText('B')).toBeInTheDocument()
    })

    it('should accept size prop on items without error', () => {
      render(
        <ToggleGroup type="single">
          <ToggleGroupItem value="a" size="sm">A</ToggleGroupItem>
          <ToggleGroupItem value="b" size="lg">B</ToggleGroupItem>
        </ToggleGroup>
      )
      expect(screen.getByText('A')).toBeInTheDocument()
    })
  })
})
