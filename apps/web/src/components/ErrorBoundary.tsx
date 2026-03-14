import { Component, ErrorInfo, ReactNode } from 'react'
import { Sentry } from '../shared/lib/sentry'

interface Props {
  children: ReactNode
  fallback?: ReactNode
  onError?: (error: Error, errorInfo: ErrorInfo) => void
}

interface State {
  hasError: boolean
  error: Error | null
  errorInfo: ErrorInfo | null
  errorId: string | null
}

export class ErrorBoundary extends Component<Props, State> {
  constructor(props: Props) {
    super(props)
    this.state = {
      hasError: false,
      error: null,
      errorInfo: null,
      errorId: null,
    }
  }

  static getDerivedStateFromError(error: Error): Partial<State> {
    return { hasError: true, error }
  }

  componentDidCatch(error: Error, errorInfo: ErrorInfo) {
    const errorId = Math.random().toString(36).slice(2, 9)
    const isDev = window.ENV?.ENVIRONMENT !== 'production'
    this.setState({ errorInfo, errorId })

    if (isDev) {
      console.error('ErrorBoundary caught error:', error)
      console.error('Error info:', errorInfo)
    }

    Sentry.captureException(error, {
      extra: { componentStack: errorInfo.componentStack ?? '' },
    })

    this.props.onError?.(error, errorInfo)
  }

  handleReload = () => {
    window.location.reload()
  }

  handleReset = () => {
    this.setState({ hasError: false, error: null, errorInfo: null, errorId: null })
  }

  render() {
    if (this.state.hasError) {
      // Custom fallback UI
      if (this.props.fallback) {
        return this.props.fallback
      }

      // Default error UI
      const isDev = window.ENV?.ENVIRONMENT !== 'production'
      return (
        <div className="p-10 max-w-3xl mx-auto font-sans">
          <h1 className="text-red-600 mb-4 text-2xl font-semibold">
            Something went wrong
          </h1>
          <p className="text-lg text-gray-500 dark:text-gray-400 mb-6">
            We encountered an unexpected error. Please try refreshing the page.
          </p>

          {this.state.errorId && (
            <p className="text-sm text-gray-500 mb-6">Error ID: {this.state.errorId}</p>
          )}

          <div className="flex gap-3 mb-8">
            <button
              onClick={this.handleReload}
              className="px-6 py-3 bg-blue-600 text-white border-none rounded-lg text-base cursor-pointer font-medium hover:bg-blue-700 transition-colors"
            >
              Reload Page
            </button>
            <button
              onClick={this.handleReset}
              className="px-6 py-3 bg-gray-500 text-white border-none rounded-lg text-base cursor-pointer font-medium hover:bg-gray-600 transition-colors"
            >
              Try Again
            </button>
          </div>

          {isDev && this.state.error && (
            <details className="p-4 bg-red-50 dark:bg-red-950 border border-red-200 dark:border-red-800 rounded-lg">
              <summary className="cursor-pointer font-bold text-red-600 dark:text-red-400 mb-3">
                Error Details (Development/QA Only)
              </summary>
              <pre className="p-3 bg-white dark:bg-gray-900 border border-gray-200 dark:border-gray-700 rounded overflow-auto text-xs leading-relaxed">
                <code>
                  {this.state.error.toString()}
                  {'\n\n'}
                  {this.state.errorInfo?.componentStack}
                </code>
              </pre>
            </details>
          )}
        </div>
      )
    }

    return this.props.children
  }
}
