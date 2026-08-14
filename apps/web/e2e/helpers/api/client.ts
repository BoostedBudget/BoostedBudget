import type { Page } from '@playwright/test'

const BACKEND_PORT = process.env.E2E_BACKEND_PORT || '8080'
export const API_BASE = `http://localhost:${BACKEND_PORT}`

// Retries up to 5 times with progressive backoff because the first token after sign-in may lack
// custom claims (email) if Clerk hasn't completed its token refresh cycle yet. Lifted from
// auth.setup.ts.
//
// #1531 Task 11e finding: this Clerk test tenant's default session JWT never carries an
// email/primary_email claim at all (confirmed by decoding a real token here — payload has only
// azp/exp/iat/iss/nbf/sid/sts/sub/v), so every one of the 5 retries used to return null and the
// caller got NO Authorization header on every real (non-mocked) apiClient call — a silent 401,
// not a slow-refresh timing issue. Fixed to keep the LAST successfully-obtained token as a
// fallback instead of discarding it: the backend's own admin middleware already tolerates an
// email-less JWT by resolving the address via a live Clerk API call
// (auth.FetchEmailFromClerkAPI, backend/internal/huma/middleware/admin.go) — a valid, unexpired
// token missing only the email claim is still far more useful to send than no token at all.
export async function getAuthToken(page: Page): Promise<string | null> {
  let lastValidToken: string | null = null
  for (let attempt = 0; attempt < 5; attempt++) {
    try {
      const token = await page.evaluate(async () => {
        const clerk = (
          window as unknown as {
            Clerk?: { session?: { getToken: () => Promise<string | null> } }
          }
        ).Clerk
        if (!clerk?.session) return null
        const t = await clerk.session.getToken()
        if (!t) return null
        try {
          const payload = JSON.parse(atob(t.split('.')[1]))
          if (!payload.email && !payload.primary_email) {
            return { withEmail: false, token: t }
          }
        } catch {
          // Accept token if we can't decode; backend will validate
        }
        return { withEmail: true, token: t }
      })
      if (token) {
        if (token.withEmail) return token.token
        lastValidToken = token.token
      }
    } catch {
      // Retry if execution context was destroyed during navigation
    }
    if (attempt < 4) await page.waitForTimeout(1000 * (attempt + 1))
  }
  return lastValidToken
}

// Token is fetched once per call; create a new client per spec if you need freshness.
export async function apiClient(page: Page) {
  const token = await getAuthToken(page)
  const headers = token ? { Authorization: `Bearer ${token}` } : {}

  return {
    get: (path: string) =>
      page.request.get(`${API_BASE}${path}`, { headers }),
    post: (path: string, data?: unknown) =>
      page.request.post(`${API_BASE}${path}`, {
        headers: { ...headers, 'Content-Type': 'application/json' },
        data,
      }),
    put: (path: string, data?: unknown) =>
      page.request.put(`${API_BASE}${path}`, {
        headers: { ...headers, 'Content-Type': 'application/json' },
        data,
      }),
    delete: (path: string) =>
      page.request.delete(`${API_BASE}${path}`, { headers }),
  }
}
