import { describe, it, expect, mock } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { ConditionBuilder } from './ConditionBuilder'
import type { RuleCondition } from '../types'

const sampleConditions: RuleCondition[] = [
  { field: 'budget.percent_used', operator: 'gte', value: 80 },
  { field: 'account.balance', operator: 'lt', value: 100 },
]

const renderConditionBuilder = (props: Partial<Parameters<typeof ConditionBuilder>[0]> = {}) =>
  render(
    <ConditionBuilder
      conditions={[]}
      logic="AND"
      onConditionsChange={() => {}}
      onLogicChange={() => {}}
      {...props}
    />
  )

describe('ConditionBuilder', () => {
  it('should show empty state when no conditions', () => {
    renderConditionBuilder()
    expect(screen.getByText(/no conditions/i)).toBeInTheDocument()
  })

  it('should show add condition button', () => {
    renderConditionBuilder()
    expect(screen.getByRole('button', { name: /add condition/i })).toBeInTheDocument()
  })

  it('should call onConditionsChange when add button is clicked', () => {
    const onConditionsChange = mock(() => {})

    renderConditionBuilder({ onConditionsChange })
    fireEvent.click(screen.getByRole('button', { name: /add condition/i }))

    expect(onConditionsChange).toHaveBeenCalledWith([
      expect.objectContaining({
        field: expect.any(String),
        operator: expect.any(String),
        value: expect.anything(),
      }),
    ])
  })

  it('should display all conditions', () => {
    renderConditionBuilder({ conditions: sampleConditions })

    const fieldSelects = screen.getAllByLabelText(/field/i)
    expect(fieldSelects).toHaveLength(2)
  })

  it('should show logic selector when multiple conditions exist', () => {
    renderConditionBuilder({ conditions: sampleConditions })

    expect(screen.getByRole('button', { name: /and/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /or/i })).toBeInTheDocument()
  })

  it('should not show logic selector for single condition', () => {
    renderConditionBuilder({ conditions: [sampleConditions[0]] })

    expect(screen.queryByRole('button', { name: /^and$/i })).not.toBeInTheDocument()
  })

  it('should call onConditionsChange when field is changed', () => {
    const onConditionsChange = mock(() => {})

    renderConditionBuilder({
      conditions: [sampleConditions[0]],
      onConditionsChange,
    })

    fireEvent.change(screen.getByLabelText(/field/i), {
      target: { value: 'account.balance' },
    })

    expect(onConditionsChange).toHaveBeenCalledWith([
      expect.objectContaining({ field: 'account.balance' }),
    ])
  })

  it('should call onConditionsChange when operator is changed', () => {
    const onConditionsChange = mock(() => {})

    renderConditionBuilder({
      conditions: [sampleConditions[0]],
      onConditionsChange,
    })

    fireEvent.change(screen.getByLabelText(/operator/i), {
      target: { value: 'lt' },
    })

    expect(onConditionsChange).toHaveBeenCalledWith([
      expect.objectContaining({ operator: 'lt' }),
    ])
  })

  it('should call onConditionsChange when value is changed', () => {
    const onConditionsChange = mock(() => {})

    renderConditionBuilder({
      conditions: [sampleConditions[0]],
      onConditionsChange,
    })

    fireEvent.change(screen.getByLabelText(/value/i), {
      target: { value: '90' },
    })

    expect(onConditionsChange).toHaveBeenCalledWith([
      expect.objectContaining({ value: 90 }),
    ])
  })

  it('should show remove button for each condition', () => {
    renderConditionBuilder({ conditions: sampleConditions })

    const removeButtons = screen.getAllByRole('button', { name: /remove/i })
    expect(removeButtons).toHaveLength(2)
  })

  it('should call onConditionsChange when remove is clicked', () => {
    const onConditionsChange = mock(() => {})

    renderConditionBuilder({
      conditions: sampleConditions,
      onConditionsChange,
    })

    const removeButtons = screen.getAllByRole('button', { name: /remove/i })
    fireEvent.click(removeButtons[0])

    expect(onConditionsChange).toHaveBeenCalledWith([sampleConditions[1]])
  })

  it('should highlight AND when logic is AND', () => {
    renderConditionBuilder({ conditions: sampleConditions, logic: 'AND' })

    expect(screen.getByRole('button', { name: /and/i })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: /or/i })).toHaveAttribute('aria-pressed', 'false')
  })

  it('should highlight OR when logic is OR', () => {
    renderConditionBuilder({ conditions: sampleConditions, logic: 'OR' })

    expect(screen.getByRole('button', { name: /and/i })).toHaveAttribute('aria-pressed', 'false')
    expect(screen.getByRole('button', { name: /or/i })).toHaveAttribute('aria-pressed', 'true')
  })

  it('should call onLogicChange when logic is toggled', () => {
    const onLogicChange = mock(() => {})

    renderConditionBuilder({
      conditions: sampleConditions,
      onLogicChange,
    })

    fireEvent.click(screen.getByRole('button', { name: /or/i }))

    expect(onLogicChange).toHaveBeenCalledWith('OR')
  })

  it('should disable all controls when disabled prop is true', () => {
    renderConditionBuilder({
      conditions: [sampleConditions[0]],
      disabled: true,
    })

    expect(screen.getByLabelText(/field/i)).toBeDisabled()
    expect(screen.getByLabelText(/operator/i)).toBeDisabled()
    expect(screen.getByLabelText(/value/i)).toBeDisabled()
  })

  it('should have accessible labels for all controls', () => {
    renderConditionBuilder({ conditions: [sampleConditions[0]] })

    expect(screen.getByLabelText(/field/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/operator/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/value/i)).toBeInTheDocument()
  })
})
