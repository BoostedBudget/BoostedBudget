/**
 * Console logging helper for diagnostic output that must never reach
 * production. Several third-party integrations (Paddle, Plaid) emit
 * payloads that can carry financial PII (customer email, institution/account
 * names and masks); those must not be `console.log`'d outside local
 * development, because Sentry's default `Breadcrumbs` integration captures
 * every `console.*` call by default (see `sentry.ts`'s `beforeBreadcrumb`,
 * which drops the whole `console` category as a second line of defense).
 */
export function devLog(...args: unknown[]): void {
  if (import.meta.env.DEV) {
    console.log(...args)
  }
}
