/**
 * ConditionBuilder Component Tests
 * Issue #39: Rule Builder UI
 *
 * Tests for the dynamic condition builder with add/remove rows.
 * TDD RED: Writing tests before implementation.
 */

import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { ConditionBuilder } from './ConditionBuilder'
import type { RuleCondition } from '../types'

describe('ConditionBuilder', () => {
  describe('Empty State', () => {
    it('should show empty state message when no conditions', () => {
      render(
        <ConditionBuilder
          conditions={[]}
          logic="AND"
          onConditionsChange={() => {}}
          onLogicChange={() => {}}
        />
      )

      expect(screen.getByText(/no conditions/i)).toBeInTheDocument()
    })

    it('should show add condition button', () => {
      render(
        <ConditionBuilder
          conditions={[]}
          logic="AND"
          onConditionsChange={() => {}}
          onLogicChange={() => {}}
        />
      )

      expect(screen.getByRole('button', { name: /add condition/i })).toBeInTheDocument()
    })
  })

  describe('Adding Conditions', () => {
    it('should call onConditionsChange when add button is clicked', () => {
      const onConditionsChange = vi.fn()

      render(
        <ConditionBuilder
          conditions={[]}
          logic="AND"
          onConditionsChange={onConditionsChange}
          onLogicChange={() => {}}
        />
      )

      fireEvent.click(screen.getByRole('button', { name: /add condition/i }))

      expect(onConditionsChange).toHaveBeenCalledWith([
        expect.objectContaining({
          field: expect.any(String),
          operator: expect.any(String),
          value: expect.anything(),
        }),
      ])
    })
  })

  describe('Displaying Conditions', () => {
    const sampleConditions: RuleCondition[] = [
      { field: 'budget.percent_used', operator: 'gte', value: 80 },
      { field: 'account.balance', operator: 'lt', value: 100 },
    ]

    it('should display all conditions', () => {
      render(
        <ConditionBuilder
          conditions={sampleConditions}
          logic="AND"
          onConditionsChange={() => {}}
          onLogicChange={() => {}}
        />
      )

      // Should have field selects
      const fieldSelects = screen.getAllByLabelText(/field/i)
      expect(fieldSelects).toHaveLength(2)
    })

    it('should show logic selector when multiple conditions exist', () => {
      render(
        <ConditionBuilder
          conditions={sampleConditions}
          logic="AND"
          onConditionsChange={() => {}}
          onLogicChange={() => {}}
        />
      )

      expect(screen.getByRole('button', { name: /and/i })).toBeInTheDocument()
      expect(screen.getByRole('button', { name: /or/i })).toBeInTheDocument()
    })

    it('should not show logic selector for single condition', () => {
      render(
        <ConditionBuilder
          conditions={[sampleConditions[0]]}
          logic="AND"
          onConditionsChange={() => {}}
          onLogicChange={() => {}}
        />
      )

      expect(screen.queryByRole('button', { name: /^and$/i })).not.toBeInTheDocument()
    })
  })

  describe('Editing Conditions', () => {
    const sampleCondition: RuleCondition = {
      field: 'budget.percent_used',
      operator: 'gte',
      value: 80,
    }

    it('should call onConditionsChange when field is changed', () => {
      const onConditionsChange = vi.fn()

      render(
        <ConditionBuilder
          conditions={[sampleCondition]}
          logic="AND"
          onConditionsChange={onConditionsChange}
          onLogicChange={() => {}}
        />
      )

      fireEvent.change(screen.getByLabelText(/field/i), {
        target: { value: 'account.balance' },
      })

      expect(onConditionsChange).toHaveBeenCalledWith([
        expect.objectContaining({
          field: 'account.balance',
        }),
      ])
    })

    it('should call onConditionsChange when operator is changed', () => {
      const onConditionsChange = vi.fn()

      render(
        <ConditionBuilder
          conditions={[sampleCondition]}
          logic="AND"
          onConditionsChange={onConditionsChange}
          onLogicChange={() => {}}
        />
      )

      fireEvent.change(screen.getByLabelText(/operator/i), {
        target: { value: 'lt' },
      })

      expect(onConditionsChange).toHaveBeenCalledWith([
        expect.objectContaining({
          operator: 'lt',
        }),
      ])
    })

    it('should call onConditionsChange when value is changed', () => {
      const onConditionsChange = vi.fn()

      render(
        <ConditionBuilder
          conditions={[sampleCondition]}
          logic="AND"
          onConditionsChange={onConditionsChange}
          onLogicChange={() => {}}
        />
      )

      fireEvent.change(screen.getByLabelText(/value/i), {
        target: { value: '90' },
      })

      expect(onConditionsChange).toHaveBeenCalledWith([
        expect.objectContaining({
          value: 90,
        }),
      ])
    })
  })

  describe('Removing Conditions', () => {
    const sampleConditions: RuleCondition[] = [
      { field: 'budget.percent_used', operator: 'gte', value: 80 },
      { field: 'account.balance', operator: 'lt', value: 100 },
    ]

    it('should show remove button for each condition', () => {
      render(
        <ConditionBuilder
          conditions={sampleConditions}
          logic="AND"
          onConditionsChange={() => {}}
          onLogicChange={() => {}}
        />
      )

      const removeButtons = screen.getAllByRole('button', { name: /remove/i })
      expect(removeButtons).toHaveLength(2)
    })

    it('should call onConditionsChange when remove is clicked', () => {
      const onConditionsChange = vi.fn()

      render(
        <ConditionBuilder
          conditions={sampleConditions}
          logic="AND"
          onConditionsChange={onConditionsChange}
          onLogicChange={() => {}}
        />
      )

      const removeButtons = screen.getAllByRole('button', { name: /remove/i })
      fireEvent.click(removeButtons[0])

      expect(onConditionsChange).toHaveBeenCalledWith([sampleConditions[1]])
    })
  })

  describe('Logic Toggle', () => {
    const sampleConditions: RuleCondition[] = [
      { field: 'budget.percent_used', operator: 'gte', value: 80 },
      { field: 'account.balance', operator: 'lt', value: 100 },
    ]

    it('should highlight AND when logic is AND', () => {
      render(
        <ConditionBuilder
          conditions={sampleConditions}
          logic="AND"
          onConditionsChange={() => {}}
          onLogicChange={() => {}}
        />
      )

      expect(screen.getByRole('button', { name: /and/i })).toHaveAttribute('aria-pressed', 'true')
      expect(screen.getByRole('button', { name: /or/i })).toHaveAttribute('aria-pressed', 'false')
    })

    it('should highlight OR when logic is OR', () => {
      render(
        <ConditionBuilder
          conditions={sampleConditions}
          logic="OR"
          onConditionsChange={() => {}}
          onLogicChange={() => {}}
        />
      )

      expect(screen.getByRole('button', { name: /and/i })).toHaveAttribute('aria-pressed', 'false')
      expect(screen.getByRole('button', { name: /or/i })).toHaveAttribute('aria-pressed', 'true')
    })

    it('should call onLogicChange when logic is toggled', () => {
      const onLogicChange = vi.fn()

      render(
        <ConditionBuilder
          conditions={sampleConditions}
          logic="AND"
          onConditionsChange={() => {}}
          onLogicChange={onLogicChange}
        />
      )

      fireEvent.click(screen.getByRole('button', { name: /or/i }))

      expect(onLogicChange).toHaveBeenCalledWith('OR')
    })
  })

  describe('Disabled State', () => {
    it('should disable all controls when disabled prop is true', () => {
      render(
        <ConditionBuilder
          conditions={[{ field: 'budget.percent_used', operator: 'gte', value: 80 }]}
          logic="AND"
          onConditionsChange={() => {}}
          onLogicChange={() => {}}
          disabled
        />
      )

      expect(screen.getByLabelText(/field/i)).toBeDisabled()
      expect(screen.getByLabelText(/operator/i)).toBeDisabled()
      expect(screen.getByLabelText(/value/i)).toBeDisabled()
    })
  })

  describe('Accessibility', () => {
    it('should have accessible labels for all controls', () => {
      render(
        <ConditionBuilder
          conditions={[{ field: 'budget.percent_used', operator: 'gte', value: 80 }]}
          logic="AND"
          onConditionsChange={() => {}}
          onLogicChange={() => {}}
        />
      )

      expect(screen.getByLabelText(/field/i)).toBeInTheDocument()
      expect(screen.getByLabelText(/operator/i)).toBeInTheDocument()
      expect(screen.getByLabelText(/value/i)).toBeInTheDocument()
    })
  })
})
