import { Component, ErrorInfo, ReactNode } from 'react'
import { Sentry } from '../shared/lib/sentry'
import { Button } from './ui/button'

interface Props {
  children: ReactNode
  featureName: string
}

interface State {
  hasError: boolean
  errorId: string | null
}

/**
 * Lightweight per-feature error boundary.
 * Shows a contextual error message with a "Try again" button.
 */
export class FeatureErrorBoundary extends Component<Props, State> {
  constructor(props: Props) {
    super(props)
    this.state = { hasError: false, errorId: null }
  }

  static getDerivedStateFromError(): Partial<State> {
    return { hasError: true }
  }

  componentDidCatch(error: Error, errorInfo: ErrorInfo) {
    const errorId = Math.random().toString(36).slice(2, 9)
    this.setState({ errorId })
    Sentry.captureException(error, {
      tags: { feature: this.props.featureName },
      extra: { componentStack: errorInfo.componentStack ?? '' },
    })
  }

  handleReset = () => {
    this.setState({ hasError: false, errorId: null })
  }

  render() {
    if (this.state.hasError) {
      return (
        <div className="flex flex-col items-center justify-center p-8 text-center space-y-4">
          <p className="text-muted-foreground">
            We couldn't load your {this.props.featureName}.
          </p>
          {this.state.errorId && (
            <p className="text-xs text-muted-foreground">Error ID: {this.state.errorId}</p>
          )}
          <Button variant="outline" onClick={this.handleReset}>
            Try again
          </Button>
        </div>
      )
    }

    return this.props.children
  }
}
