/**
 * EnvironmentBadge Component Tests
 * Issue #643: Admin dashboard landing page
 */

import { describe, it, expect } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { EnvironmentBadge } from './EnvironmentBadge'

describe('EnvironmentBadge', () => {
  it('should render development badge', () => {
    render(<EnvironmentBadge environment="development" />)

    expect(screen.getByText('DEVELOPMENT')).toBeInTheDocument()
  })

  it('should render qa badge', () => {
    render(<EnvironmentBadge environment="qa" />)

    expect(screen.getByText('QA')).toBeInTheDocument()
  })

  it('should render staging badge', () => {
    render(<EnvironmentBadge environment="staging" />)

    expect(screen.getByText('STAGING')).toBeInTheDocument()
  })

  it('should render production badge', () => {
    render(<EnvironmentBadge environment="production" />)

    expect(screen.getByText('PRODUCTION')).toBeInTheDocument()
  })

  it('should apply correct styling for development', () => {
    render(<EnvironmentBadge environment="development" />)

    const badge = screen.getByText('DEVELOPMENT')
    expect(badge).toHaveClass('environment-badge--development')
  })

  it('should apply correct styling for production', () => {
    render(<EnvironmentBadge environment="production" />)

    const badge = screen.getByText('PRODUCTION')
    expect(badge).toHaveClass('environment-badge--production')
  })
})
