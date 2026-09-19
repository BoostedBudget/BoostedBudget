import { execFileSync } from 'node:child_process'
import { env, exit, kill, pid } from 'node:process'

type Environment = Record<string, string | undefined>
const fixtureGrant = '[{"key":"comp_premium","expires_at":null}]'
const providerSettings = ['REVENUECAT_PROJECT_ID', 'REVENUECAT_ENVIRONMENT', 'REVENUECAT_SECRET_API_KEY', 'REVENUECAT_GRANT_API_KEY', 'REVENUECAT_WEBHOOK_AUTHORIZATION', 'REVENUECAT_WEBHOOK_SIGNING_SECRET']

// Test-only module, excluded from the production TypeScript program and bundle.
// This is test data, not a provider emulator or a production access fallback.
export function fixtureDatabase(configuration: Environment = env): string {
  if (configuration.E2E_REVENUECAT_FIXTURE_MODE !== 'simulated') throw new Error('Explicit RevenueCat fixture mode is required')
  const value = configuration.DATABASE_URL ?? ''
  const url = new URL(value)
  if (!['postgres:', 'postgresql:'].includes(url.protocol) || !['localhost', '127.0.0.1', '[::1]'].includes(url.hostname)
    || url.pathname !== '/test' || url.username !== 'test'
    || [...url.searchParams].some(([key, value]) => key !== 'sslmode' || value !== 'disable')) {
    throw new Error('RevenueCat fixtures require the harness-owned local test database')
  }
  if (providerSettings.some(key => configuration[key])) throw new Error('Real RevenueCat settings must be absent in fixture mode')
  return value
}

export function fixturePsqlEnvironment(configuration: Environment = env): Environment {
  const database = new URL(fixtureDatabase(configuration))
  const connection: Environment = {...configuration, PGHOST: database.hostname.replace(/^\[|\]$/g, ''), PGPORT: database.port || '5432',
    PGUSER: decodeURIComponent(database.username), PGPASSWORD: decodeURIComponent(database.password),
    PGDATABASE: 'test', PGSSLMODE: 'disable', PGOPTIONS: '', PGCONNECT_TIMEOUT: '5'}
  // An empty PGSERVICE still asks libpq to load a service named "".
  delete connection.PGSERVICE
  return connection
}

function execute(sql: string): void {
  const connection = fixturePsqlEnvironment()
  try {
    execFileSync('psql', ['-X', '-v', 'ON_ERROR_STOP=1', '-q'], {
      input: sql, env: connection,
      stdio: ['pipe', 'pipe', 'pipe'], timeout: 15000,
    })
  } catch {
    // Do not expose connection strings, user identifiers, or SQL in test logs.
    throw new Error('RevenueCat E2E fixture database operation failed; check disposable database state')
  }
}

const registrySQL = `CREATE SCHEMA IF NOT EXISTS e2e_revenuecat_fixture;
CREATE TABLE IF NOT EXISTS e2e_revenuecat_fixture.customers (
  user_id uuid PRIMARY KEY REFERENCES public.users(id) ON DELETE CASCADE,
  expected_grants jsonb NOT NULL
);`

export function fixtureRegistrationSQL(userID: string): string {
  if (!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(userID)) throw new Error('Invalid fixture user ID')
  return `DO $$
DECLARE fixture_id uuid := '${userID}'::uuid;
BEGIN
  PERFORM 1 FROM public.users WHERE id = fixture_id AND NOT is_deleted;
  IF NOT FOUND THEN RAISE EXCEPTION 'Fixture user is unavailable'; END IF;
  INSERT INTO public.revenuecat_customers(user_id) VALUES (fixture_id) ON CONFLICT DO NOTHING;
  PERFORM 1 FROM public.revenuecat_customers WHERE user_id = fixture_id FOR UPDATE;
  IF EXISTS (SELECT 1 FROM e2e_revenuecat_fixture.customers WHERE user_id = fixture_id) THEN
    PERFORM 1 FROM public.revenuecat_customers c
      JOIN e2e_revenuecat_fixture.customers f USING (user_id)
      WHERE c.user_id = fixture_id AND c.grants = f.expected_grants
        AND c.erasure_started_at IS NULL AND c.erasure_confirmed_at IS NULL;
    IF NOT FOUND THEN RAISE EXCEPTION 'Existing fixture changed; refusing to replace it'; END IF;
  ELSE
    UPDATE public.revenuecat_customers SET grants = '${fixtureGrant}'::jsonb,
      access_updated_at = clock_timestamp(), access_error = '',
      billing = '[]', billing_error = '', billing_updated_at = clock_timestamp(), next_refresh_at = clock_timestamp() + interval '1 day'
      WHERE user_id = fixture_id AND access_updated_at IS NULL AND grants = '[]'::jsonb
        AND billing_updated_at IS NULL AND erasure_started_at IS NULL AND erasure_confirmed_at IS NULL;
    IF NOT FOUND THEN RAISE EXCEPTION 'Refusing to replace an observed or fenced customer'; END IF;
    INSERT INTO e2e_revenuecat_fixture.customers VALUES (fixture_id, '${fixtureGrant}'::jsonb);
  END IF;
END $$;`
}

export function registerRevenueCatFixture(userID: string): void {
  execute(fixtureRegistrationSQL(userID))
}

export function fixtureHeartbeatSQL(): string {
  return `DO $$ DECLARE expected bigint; refreshed bigint;
BEGIN
  WITH refreshed_rows AS (UPDATE public.revenuecat_customers c
    SET access_updated_at = clock_timestamp(), billing_updated_at = clock_timestamp(),
        next_refresh_at = clock_timestamp() + interval '1 day'
    FROM e2e_revenuecat_fixture.customers f, public.users u
    WHERE c.user_id = f.user_id AND u.id = c.user_id AND NOT u.is_deleted
      AND c.grants = f.expected_grants AND c.access_error = '' AND c.billing_error = ''
      AND c.erasure_started_at IS NULL AND c.erasure_confirmed_at IS NULL
    RETURNING c.user_id)
  SELECT (SELECT count(*) FROM e2e_revenuecat_fixture.customers),
         (SELECT count(*) FROM refreshed_rows) INTO expected, refreshed;
  IF refreshed <> expected THEN RAISE EXCEPTION 'Fixture changed; refusing to mask an authority failure'; END IF;
END $$;`
}

export function initializeRevenueCatFixture(): void {
  execute(registrySQL)
  execute(fixtureHeartbeatSQL())
}

// Playwright only checks webServer process exit during startup, not mid-suite.
export function assertFixtureProcessAlive(value: string | undefined, probe = (id: number) => { kill(id, 0) }): void {
  if (!value || !/^[1-9][0-9]*$/.test(value) || !Number.isSafeInteger(Number(value))) {
    throw new Error('RevenueCat fixture process was not registered')
  }
  try { probe(Number(value)) } catch {
    throw new Error('RevenueCat fixture process ended before E2E completed')
  }
}

export function verifyRevenueCatFixtureCompletion(
  value: string | undefined,
  probe?: (id: number) => void,
  verifyState = () => { execute(fixtureHeartbeatSQL()) },
): void {
  assertFixtureProcessAlive(value, probe)
  // A mutation after the last periodic heartbeat must still fail the run.
  verifyState()
}

if (import.meta.main) {
  try {
    initializeRevenueCatFixture()
    console.log(`RevenueCat simulated fixture ready ${pid}`)
    setInterval(() => {
      try { execute(fixtureHeartbeatSQL()) } catch {
        console.error('RevenueCat simulated fixture freshness failed')
        exit(1)
      }
    }, 60000)
  } catch {
    console.error('RevenueCat simulated fixture startup failed')
    exit(1)
  }
}
