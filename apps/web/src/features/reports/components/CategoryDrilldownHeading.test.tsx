import { describe, it, expect, mock } from 'bun:test'
import { render, screen } from '@testing-library/react'
import React from 'react'

import { CategoryDrilldownHeading } from './CategoryDrilldownHeading'

describe('CategoryDrilldownHeading', () => {
  it('renders bare heading when no drilldown', () => {
    const { container } = render(
      <CategoryDrilldownHeading drilldown={undefined} onBack={() => {}} />
    )
    expect(container.textContent).toContain('Spending by Category')
    expect(container.textContent).not.toContain('—')
  })

  it('renders humanized display label, never raw enum', () => {
    const { container } = render(
      <CategoryDrilldownHeading
        drilldown={{ category: 'FOOD_AND_DRINK', displayLabel: 'Food And Drink' }}
        onBack={() => {}}
      />
    )
    expect(container.textContent).toContain('Food And Drink')
    expect(container.textContent).not.toContain('FOOD_AND_DRINK')
  })

  it('invokes onBack when back button clicked', () => {
    const onBack = mock(() => {})
    render(
      <CategoryDrilldownHeading
        drilldown={{ category: 'FOOD_AND_DRINK', displayLabel: 'Food And Drink' }}
        onBack={onBack}
      />
    )
    screen.getByText('(back)').click()
    expect(onBack).toHaveBeenCalled()
  })
})
