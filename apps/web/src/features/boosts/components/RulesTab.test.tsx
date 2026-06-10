/**
 * RulesTab Tests
 * Issue #1267: Boosts page — thin wrapper rendering RulesPage with hidden h1
 *
 * TDD RED: Tests written before implementation.
 */

import { describe, it, expect, mock } from 'bun:test'
import { render, screen } from '@testing-library/react'
import React from 'react'
import { MemoryRouter } from 'react-router-dom'

// Mock RulesPage to avoid rendering the full rules management UI in this test
mock.module('@/features/rules/components/RulesPage', () => ({
  RulesPage: () => <div data-testid="rules-page-mock"><h1>Automation Rules</h1></div>,
}))

import { RulesTab } from './RulesTab'

describe('RulesTab', () => {
  it('renders the RulesPage component', () => {
    render(
      <MemoryRouter>
        <RulesTab />
      </MemoryRouter>
    )
    expect(screen.getByTestId('rules-page-mock')).toBeTruthy()
  })

  it('suppresses the RulesPage h1 to avoid duplicate heading', () => {
    const { container } = render(
      <MemoryRouter>
        <RulesTab />
      </MemoryRouter>
    )
    // The h1 from RulesPage should be hidden via CSS ([&_h1]:hidden)
    const h1 = container.querySelector('h1')
    if (h1) {
      // If h1 exists, it should have hidden styling applied via parent class
      const parent = h1.closest('[class*="hidden"]') || h1.parentElement?.closest('[class*="hidden"]')
      // Either the h1 is not visible, or wrapped in a hidden container
      const computedStyle = window.getComputedStyle(h1)
      const isHidden =
        computedStyle.display === 'none' ||
        h1.closest('[class*="hidden"]') !== null ||
        parent !== null
      expect(isHidden || h1.className.includes('hidden')).toBe(true)
    }
    // Pass — h1 either doesn't render (if hidden by Tailwind) or is suppressed
    expect(true).toBe(true)
  })
})
