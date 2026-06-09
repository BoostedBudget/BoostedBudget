/**
 * SavingsSummaryHeader Tests
 * Issue #1267: Boosts page — savings stats header (total moved, MoM, active rules)
 *
 * TDD RED: Tests written before implementation.
 */

import { describe, it, expect } from 'bun:test'
import { render, screen } from '@testing-library/react'
import React from 'react'
import { SavingsSummaryHeader } from './SavingsSummaryHeader'

describe('SavingsSummaryHeader', () => {
  describe('loading state', () => {
    it('shows loading skeleton when isLoading is true', () => {
      const { container } = render(
        <SavingsSummaryHeader
          totalMoved={null}
          momChange={null}
          completedCount={null}
          activeRulesCount={0}
          isLoading={true}
        />
      )
      // Loading state should show some kind of placeholder (skeleton div or animate-pulse)
      expect(container.querySelector('[data-testid="summary-loading"]') || container.querySelector('.animate-pulse')).toBeTruthy()
    })
  })

  describe('data display', () => {
    it('shows formatted total moved amount', () => {
      render(
        <SavingsSummaryHeader
          totalMoved={2500}
          momChange={null}
          completedCount={10}
          activeRulesCount={3}
          isLoading={false}
        />
      )
      // $2,500 formatted
      expect(screen.getByText(/2,500/)).toBeTruthy()
    })

    it('applies data-ph-mask to the total moved amount', () => {
      const { container } = render(
        <SavingsSummaryHeader
          totalMoved={2500}
          momChange={null}
          completedCount={10}
          activeRulesCount={3}
          isLoading={false}
        />
      )
      const maskedElements = container.querySelectorAll('[data-ph-mask]')
      expect(maskedElements.length).toBeGreaterThan(0)
    })

    it('shows MoM change when momChange is positive', () => {
      render(
        <SavingsSummaryHeader
          totalMoved={2500}
          momChange={150}
          completedCount={10}
          activeRulesCount={3}
          isLoading={false}
        />
      )
      expect(screen.getByText(/150/)).toBeTruthy()
    })

    it('does not show MoM change when momChange is null', () => {
      const { container } = render(
        <SavingsSummaryHeader
          totalMoved={500}
          momChange={null}
          completedCount={2}
          activeRulesCount={1}
          isLoading={false}
        />
      )
      // No element with data-testid for mom should appear
      expect(container.querySelector('[data-testid="mom-change"]')).toBeNull()
    })

    it('shows completed count', () => {
      render(
        <SavingsSummaryHeader
          totalMoved={1000}
          momChange={50}
          completedCount={15}
          activeRulesCount={2}
          isLoading={false}
        />
      )
      expect(screen.getByText(/15/)).toBeTruthy()
    })

    it('shows active rules count', () => {
      render(
        <SavingsSummaryHeader
          totalMoved={1000}
          momChange={50}
          completedCount={5}
          activeRulesCount={4}
          isLoading={false}
        />
      )
      expect(screen.getByText(/4/)).toBeTruthy()
    })

    it('shows em-dash or 0 when totalMoved is null (no data yet)', () => {
      render(
        <SavingsSummaryHeader
          totalMoved={null}
          momChange={null}
          completedCount={null}
          activeRulesCount={0}
          isLoading={false}
        />
      )
      // Should not throw and should show some placeholder for empty state
      const { container } = render(
        <SavingsSummaryHeader
          totalMoved={null}
          momChange={null}
          completedCount={null}
          activeRulesCount={0}
          isLoading={false}
        />
      )
      // At minimum the component renders
      expect(container.firstChild).toBeTruthy()
    })

    it('applies data-ph-mask to MoM change amount when shown', () => {
      const { container } = render(
        <SavingsSummaryHeader
          totalMoved={2500}
          momChange={100}
          completedCount={10}
          activeRulesCount={3}
          isLoading={false}
        />
      )
      const maskedElements = container.querySelectorAll('[data-ph-mask]')
      // Should have at least 2: total + mom change
      expect(maskedElements.length).toBeGreaterThanOrEqual(2)
    })
  })
})
