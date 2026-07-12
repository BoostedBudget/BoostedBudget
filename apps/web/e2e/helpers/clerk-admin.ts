// Clerk Backend API helper for E2E test-user provisioning (issue #1370).
//
// Provisions a second, independently-authenticated Clerk test identity (the "invitee")
// needed for household-invite E2E specs. Uses Node's built-in `fetch` (available in
// Playwright's Node environment) rather than adding a new dependency — this is a thin,
// idempotent wrapper over Clerk's Backend API, not a general-purpose Clerk client.
//
// Verified against Clerk's Backend API docs (Context7 /clerk/clerk-docs):
// - GET /v1/users?email_address={email} — Bearer CLERK_SECRET_KEY — filters users by email.
// - POST /v1/users — Bearer CLERK_SECRET_KEY — body `{email_address: [email]}` creates a user.
//   `password` is optional in CreateUserParams; since this test user is only ever signed
//   into via `clerk.signIn({page, emailAddress})`'s test-mode bypass (no password check),
//   omitting it is safe for a Clerk test-mode instance.
//
// CLERK_SECRET_KEY is already required/sourced for this project's E2E harness (see
// auth.setup.ts and playwright.config.ts's webServer command for the backend process),
// so it is available here via process.env exactly the same way.

const CLERK_API_BASE = 'https://api.clerk.com/v1'

function requireSecretKey(): string {
  const key = process.env.CLERK_SECRET_KEY
  if (!key) {
    throw new Error(
      'ensureTestUserExists: CLERK_SECRET_KEY is not set. Source apps/web/.env.e2e ' +
      '(set -a && source .env.e2e && set +a) before running this spec — see the ' +
      'run-e2e-tests skill for the full local harness setup.'
    )
  }
  return key
}

// The raw Clerk Backend API has historically returned a bare array from GET /v1/users
// (unlike most other list endpoints, which wrap results in `{data: [...]}`). Handle both
// shapes defensively rather than assuming one, since this is unconfirmed at the raw-REST
// level (only the SDK-wrapped shape is documented).
function extractUserList(body: unknown): unknown[] {
  if (Array.isArray(body)) return body
  if (body && typeof body === 'object' && Array.isArray((body as { data?: unknown[] }).data)) {
    return (body as { data: unknown[] }).data
  }
  return []
}

// Idempotently provisions a Clerk test user by email: checks for an existing user first
// (safe to call every test run), only creates one if none is found.
export async function ensureTestUserExists(email: string): Promise<void> {
  const secretKey = requireSecretKey()

  const listResponse = await fetch(
    `${CLERK_API_BASE}/users?email_address=${encodeURIComponent(email)}`,
    {
      headers: { Authorization: `Bearer ${secretKey}` },
    }
  )

  if (!listResponse.ok) {
    const body = await listResponse.text()
    throw new Error(
      `ensureTestUserExists: GET /v1/users failed for "${email}" (${listResponse.status}): ${body}`
    )
  }

  const existingUsers = extractUserList(await listResponse.json())
  if (existingUsers.length > 0) {
    return
  }

  const createResponse = await fetch(`${CLERK_API_BASE}/users`, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${secretKey}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ email_address: [email] }),
  })

  if (!createResponse.ok) {
    const body = await createResponse.text()
    throw new Error(
      `ensureTestUserExists: POST /v1/users failed to create "${email}" (${createResponse.status}): ${body}`
    )
  }
}
