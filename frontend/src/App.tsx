import { useState, useEffect } from 'react'
import './App.css'

interface HealthCheck {
  status: string
  version: string
  timestamp: string
  checks: {
    database: boolean
    redis: boolean
  }
}

function App() {
  const [health, setHealth] = useState<HealthCheck | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    fetch('/health')
      .then(res => res.json())
      .then(data => {
        setHealth(data)
        setLoading(false)
      })
      .catch(err => {
        console.error('Failed to fetch health:', err)
        setLoading(false)
      })
  }, [])

  return (
    <div className="app">
      <header className="app-header">
        <h1>🚀 BoostedBudget</h1>
        <p>Supercharge your finances with intelligent automation</p>
      </header>

      <main className="app-main">
        <section className="health-check">
          <h2>System Status</h2>
          {loading ? (
            <p>Loading...</p>
          ) : health ? (
            <div className="health-status">
              <p className={`status status-${health.status}`}>
                Status: {health.status.toUpperCase()}
              </p>
              <p>Version: {health.version}</p>
              <p>Region: us-east-1 (MVP)</p>
              <div className="checks">
                <p>Database: {health.checks.database ? '✅' : '❌'}</p>
                <p>Redis: {health.checks.redis ? '✅' : '❌'}</p>
              </div>
            </div>
          ) : (
            <p className="error">Failed to load system status</p>
          )}
        </section>

        <section className="info">
          <h2>Phase 0: Foundation</h2>
          <p>✅ Backend API with health endpoint</p>
          <p>✅ OpenAPI specification</p>
          <p>✅ Frontend skeleton (React + TypeScript + Vite)</p>
          <p>⏳ Auth0 integration (coming next)</p>
          <p>⏳ Database & migrations (coming next)</p>
          <p>⏳ Docker development environment (coming next)</p>
        </section>
      </main>

      <footer className="app-footer">
        <p>BoostedBudget © 2025 | GDPR Compliant | Single Region MVP (us-east-1)</p>
      </footer>
    </div>
  )
}

export default App
