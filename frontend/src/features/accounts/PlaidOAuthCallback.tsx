/**
 * PlaidOAuthCallback Component
 *
 * Handles OAuth redirects from banks during Plaid Link flow.
 * After user authorizes at their bank, the bank redirects back to this page
 * with OAuth parameters in the URL. This component reinitializes Plaid Link
 * with the receivedRedirectUri to complete the OAuth flow.
 *
 * Related: Issue #198 - Fix Plaid OAuth callback and webhook integration
 */

import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { usePlaidLink } from 'react-plaid-link'
import { useCreateLinkToken } from '@/api/generated/endpoints/plaid/plaid'

export function PlaidOAuthCallback() {
  const navigate = useNavigate()
  const [linkToken, setLinkToken] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  // Use generated mutation hook
  const createLinkTokenMutation = useCreateLinkToken()

  // Get the full redirect URI including OAuth parameters
  const receivedRedirectUri = window.location.href

  // Fetch link token on mount
  useEffect(() => {
    createLinkTokenMutation
      .mutateAsync()
      .then(response => {
        setLinkToken(response.link_token)
      })
      .catch(err => {
        console.error('Failed to fetch link token for OAuth callback:', err)
        setError('Failed to continue bank connection. Please try again.')
      })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // Initialize Plaid Link with receivedRedirectUri to continue OAuth flow
  const { open, ready } = usePlaidLink({
    token: linkToken,
    receivedRedirectUri, // Critical: tells Plaid Link to continue OAuth flow
    onSuccess: (_publicToken, metadata) => {
      console.log('OAuth flow completed successfully:', metadata)
      // Public token exchange will be handled by the main PlaidLink component
      // For now, redirect back to accounts page
      navigate('/accounts')
    },
    onExit: (err, metadata) => {
      console.log('OAuth flow exited:', err, metadata)
      // Redirect back to accounts page
      navigate('/accounts')
    },
  })

  // Open Plaid Link automatically when ready
  useEffect(() => {
    if (ready && linkToken) {
      open()
    }
  }, [ready, linkToken, open])

  return (
    <div className="plaid-oauth-callback">
      <div className="loading-container">
        {error ? (
          <>
            <h2>Connection Error</h2>
            <p>{error}</p>
            <button onClick={() => navigate('/accounts')}>
              Return to Accounts
            </button>
          </>
        ) : (
          <>
            <h2>Continuing Bank Connection...</h2>
            <p>Please wait while we complete your bank authorization.</p>
            <div className="spinner"></div>
          </>
        )}
      </div>
    </div>
  )
}
