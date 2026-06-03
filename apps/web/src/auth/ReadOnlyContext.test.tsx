// Issue #930: ReadOnlyContext tests
// Verifies context propagation and useReadOnly hook behavior.

import { describe, it, expect } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { ReadOnlyProvider, useReadOnly } from './ReadOnlyContext'

function TestConsumer() {
  const { isReadOnly, subscriptionStatus } = useReadOnly()
  return (
    <div>
      <span data-testid="is-read-only">{String(isReadOnly)}</span>
      <span data-testid="status">{subscriptionStatus}</span>
    </div>
  )
}

describe('ReadOnlyContext', () => {
  it('defaults to isReadOnly=false when no provider is present', () => {
    render(<TestConsumer />)
    expect(screen.getByTestId('is-read-only').textContent).toBe('false')
  })

  it('defaults to empty subscriptionStatus when no provider is present', () => {
    render(<TestConsumer />)
    expect(screen.getByTestId('status').textContent).toBe('')
  })

  it('propagates isReadOnly=true from provider', () => {
    render(
      <ReadOnlyProvider isReadOnly={true} subscriptionStatus="inactive">
        <TestConsumer />
      </ReadOnlyProvider>
    )
    expect(screen.getByTestId('is-read-only').textContent).toBe('true')
  })

  it('propagates isReadOnly=false from provider', () => {
    render(
      <ReadOnlyProvider isReadOnly={false} subscriptionStatus="active">
        <TestConsumer />
      </ReadOnlyProvider>
    )
    expect(screen.getByTestId('is-read-only').textContent).toBe('false')
  })

  it('propagates subscriptionStatus from provider', () => {
    render(
      <ReadOnlyProvider isReadOnly={true} subscriptionStatus="cancelled">
        <TestConsumer />
      </ReadOnlyProvider>
    )
    expect(screen.getByTestId('status').textContent).toBe('cancelled')
  })

  it('defaults subscriptionStatus to empty string when not provided', () => {
    render(
      <ReadOnlyProvider isReadOnly={true}>
        <TestConsumer />
      </ReadOnlyProvider>
    )
    expect(screen.getByTestId('status').textContent).toBe('')
  })

  it('renders children inside provider', () => {
    render(
      <ReadOnlyProvider isReadOnly={false}>
        <span data-testid="child">hello</span>
      </ReadOnlyProvider>
    )
    expect(screen.getByTestId('child')).toBeInTheDocument()
  })
})
