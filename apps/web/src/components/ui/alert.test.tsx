/**
 * Alert Component Tests
 * Issue #832: Replace Dashboard Filter Controls with Design System Components
 */

import { describe, it, expect, mock } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { Alert, AlertTitle, AlertDescription } from './alert'

describe('Alert', () => {
  describe('Rendering', () => {
    it('should render with role="alert"', () => {
      render(<Alert>Alert content</Alert>)
      expect(screen.getByRole('alert')).toBeInTheDocument()
    })

    it('should render children', () => {
      render(<Alert>Hello alert</Alert>)
      expect(screen.getByText('Hello alert')).toBeInTheDocument()
    })

    it('should render AlertTitle', () => {
      render(
        <Alert>
          <AlertTitle>Alert Title</AlertTitle>
        </Alert>
      )
      expect(screen.getByText('Alert Title')).toBeInTheDocument()
    })

    it('should render AlertDescription', () => {
      render(
        <Alert>
          <AlertDescription>Alert description text</AlertDescription>
        </Alert>
      )
      expect(screen.getByText('Alert description text')).toBeInTheDocument()
    })
  })

  describe('Variants', () => {
    it('should render default variant', () => {
      render(<Alert variant="default" data-testid="alert">Content</Alert>)
      const alert = screen.getByTestId('alert')
      expect(alert).toBeInTheDocument()
    })

    it('should render destructive variant', () => {
      render(<Alert variant="destructive" data-testid="alert">Content</Alert>)
      const alert = screen.getByTestId('alert')
      expect(alert.className).toContain('destructive')
    })

    it('should render info variant', () => {
      render(<Alert variant="info" data-testid="alert">Content</Alert>)
      const alert = screen.getByTestId('alert')
      expect(alert.className).toContain('info')
    })

    it('should render warning variant', () => {
      render(<Alert variant="warning" data-testid="alert">Content</Alert>)
      const alert = screen.getByTestId('alert')
      expect(alert.className).toContain('warning')
    })
  })

  describe('Dismissible', () => {
    it('should show dismiss button when dismissible=true', () => {
      render(
        <Alert dismissible onDismiss={() => {}}>
          Content
        </Alert>
      )
      expect(screen.getByLabelText('Dismiss')).toBeInTheDocument()
    })

    it('should not show dismiss button when dismissible=false', () => {
      render(<Alert>Content</Alert>)
      expect(screen.queryByLabelText('Dismiss')).not.toBeInTheDocument()
    })

    it('should call onDismiss when dismiss button clicked', () => {
      const onDismiss = mock(() => {})
      render(
        <Alert dismissible onDismiss={onDismiss}>
          Content
        </Alert>
      )
      fireEvent.click(screen.getByLabelText('Dismiss'))
      expect(onDismiss).toHaveBeenCalledTimes(1)
    })
  })
})
