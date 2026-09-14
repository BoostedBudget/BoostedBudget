import { useState, useEffect } from 'react';
import { useGetSystemHealth } from '@boostedbudget/api-client/endpoints/admin/admin';
import { useTrackEvent } from '../../../shared/hooks/useTrackEvent';
import { ANALYTICS_EVENTS } from '../../../shared/analytics';
import { SummaryCard } from '@/features/admin';
import { Activity, AlertTriangle, Database, Shield, Landmark, RefreshCw, Clock, CreditCard, CheckCircle, Users, Timer, UserCheck, UserX, History, GitFork } from 'lucide-react';
import { cn } from '@/lib/utils';
import { Card } from '@/components/ui/card';
import { RevenueCatHealthSummary } from './RevenueCatHealthSummary';

// Issue #1458: formats a backlog gauge's age (seconds) as a short human string for the Job
// Backlog SummaryCard grid — mirrors the Paddle Status section's "Avg Duration" precedent
// (`${Math.round(ms / 1000)}s`) one level up in granularity, since these ages are backlog-scale
// (potentially hours) rather than single-request-scale (milliseconds).
const formatBacklogAge = (seconds: number): string => {
  if (seconds <= 0) return '0s';
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  if (hours > 0) return `${hours}h ${minutes}m`;
  if (minutes > 0) return `${minutes}m`;
  return `${Math.round(seconds)}s`;
};

// Issue #1497 item 4/5: above this many failed automated removal attempts on a single unresolved
// orphan record, the card turns warning. Mirrors the threshold in
// observability.PlaidOrphanedItemMaxRetryAttempts's intended alert rule (backend), and is derived
// the same way: the retry sweep runs daily and the retained credential expires after 30 days, so no
// single row can ever accumulate much more than ~30 attempts before it becomes
// manual-intervention work anyway. 20 therefore lights up with roughly a third of the retention
// window left — early enough for a human to act at Plaid before the automated path runs out.
//
// ⚠ A HIGH COUNT IS NOT A STATE. The backend's AdminState deliberately ignores retry_attempt_count
// (nothing in this system gives up on an attempt count), so this card answers a question the state
// cards beside it cannot — "is the retrying futile?" — and the two must not be conflated.
const ORPHAN_RETRY_ATTEMPT_WARN_THRESHOLD = 20;

const statusToVariant = (status: string): 'success' | 'warning' | 'error' => {
  if (status === 'up' || status === 'healthy') return 'success';
  if (status === 'degraded') return 'warning';
  return 'error';
};

const statusTextClass = (status: string) => {
  if (status === 'healthy') return 'text-success';
  if (status === 'degraded') return 'text-warning';
  return 'text-destructive';
};

export function HealthDashboardPage() {
  const [autoRefresh, setAutoRefresh] = useState(true);
  const trackEvent = useTrackEvent();

  useEffect(() => {
    trackEvent(ANALYTICS_EVENTS.ADMIN_PAGE_VIEWED, { page: 'health' });
  }, [trackEvent]);

  const { data, isLoading, error, refetch } = useGetSystemHealth({
    query: {
      refetchInterval: autoRefresh ? 30000 : false,
      refetchIntervalInBackground: false,
    },
  });

  if (isLoading) {
    return (
      <div className="p-8 max-w-[1400px] mx-auto">
        <h1 className="text-2xl font-semibold">System Health</h1>
        <p className="text-muted-foreground">Loading system health metrics...</p>
      </div>
    );
  }

  if (error) {
    return (
      <div className="p-8 max-w-[1400px] mx-auto">
        <h1 className="text-2xl font-semibold">System Health</h1>
        <p className="text-destructive bg-destructive/10 border border-destructive/20 rounded-lg p-4">
          Error loading system health: {error.message}
        </p>
      </div>
    );
  }

  if (!data) {
    return null;
  }

  // Issue #1458 follow-up G16: `available` distinguishes "queried successfully" (possibly a real
  // zero backlog) from "the underlying query failed" — both currently read as the identical
  // zero-valued numeric fields, so this flag is the ONLY signal the Job Backlog cards below have
  // for telling them apart. Falsy (false or, for an older cached/mocked response, undefined) reads
  // as unavailable — the fail-closed default, never a falsely-confident green zero.
  //
  // R3-7: `job_backlog` itself (not just `.available`) can be absent — a rolling deploy hitting an
  // older API response, or a partial mock — in which case an unguarded `data.job_backlog.available`
  // throws a TypeError and blanks the ENTIRE dashboard, the opposite of this field's fail-closed
  // intent. Optional-chaining here is the only guard needed: every read of `data.job_backlog.*`
  // below is already gated behind this `jobBacklogAvailable` boolean via `? :` short-circuiting
  // (e.g. `jobBacklogAvailable ? data.job_backlog.x : 'Unavailable'`), so once this flag safely
  // resolves to `undefined` (falsy) for a missing `job_backlog`, none of those reads are reached.
  const jobBacklogAvailable = data.job_backlog?.available;

  // Issue #1497 item 5. Same fail-closed shape, and the same optional-chaining guard, as
  // jobBacklogAvailable above and for the same two reasons: a FAILED derivation and a genuinely
  // EMPTY table both arrive as identical zero-valued fields, so this flag is the only signal that
  // tells them apart; and `plaid_orphaned_items` itself can be absent entirely on an older cached
  // response or a rolling deploy, where an unguarded read would throw and blank the whole
  // dashboard.
  //
  // ⚠ IT MATTERS MORE HERE THAN FOR ANY OTHER GAUGE ON THIS PAGE. A green "0 needs manual removal"
  // produced by a failed read tells an operator that no erased user's Plaid Item is still billing —
  // a compliance claim nothing actually checked.
  const orphanedItemsAvailable = data.plaid_orphaned_items?.available;

  // Issue #1504. Same fail-closed shape and the same optional-chaining guard as the two flags above,
  // for the same two reasons (a failed query and a genuine zero arrive as identical zero-valued
  // fields; the section itself can be absent on an older cached response or mid-rolling-deploy).
  const deletionResidueAvailable = data.deletion_residue?.available;

  // The count has to be read BEFORE the age, and this is not a style preference. The backend returns
  // `newest_record_seconds` as 0 both when there are no residue records at all and when one was
  // written this very instant — the two readings on this page that could not matter more differently.
  // `deletionResidueRecords` is the disambiguator, so the age card branches on it and renders 'None'
  // rather than '0s' when there is nothing to age. See domain.DeletionResidueMetrics (backend).
  const deletionResidueRecords = deletionResidueAvailable ? data.deletion_residue.records : 0;

  // Issue #1514 requirement #5. Same fail-closed shape and the same optional-chaining guard as the
  // three flags above: a FAILED read and a genuinely quiet 24 hours both arrive as identical
  // zero-valued fields, and the section itself can be absent on an older cached response or
  // mid-rolling-deploy.
  //
  // ⚠ THIS IS NOT data.paddle_status.failed_webhooks_24h, AND MUST NEVER BE COMPUTED FROM THE SAME
  // SOURCE. That number counts webhooks that PASSED signature verification and then failed during
  // processing; this section counts webhooks REJECTED because their signature never verified —
  // i.e. forged or malformed delivery attempts that were never written to the full webhook table
  // at all (they are still recorded, into a separate bounded metadata-only table). See
  // domain.RejectedWebhookAttemptMetrics (backend) for the full distinction. A non-zero reading
  // here is noteworthy in a way a routine processing failure is not: it means someone is actively
  // sending requests that fail to authenticate as Paddle or Plaid.
  const rejectedWebhookAttemptsAvailable = data.rejected_webhook_attempts_24h?.available;
  const rejectedWebhookAttemptsTotal = rejectedWebhookAttemptsAvailable
    ? data.rejected_webhook_attempts_24h.total_24h
    : 0;
  const rejectedWebhookAttemptsByProvider = rejectedWebhookAttemptsAvailable
    ? data.rejected_webhook_attempts_24h.by_provider
    : {};

  // #1631 M8b I5. Same fail-closed shape and the same optional-chaining guard as every gauge
  // above: a FAILED derivation and a genuine zero fork count both arrive as identical zero-valued
  // fields, and `forked_security_identities` itself can be absent entirely on an older cached
  // response or mid-rolling-deploy. See domain.ForkedSecurityIdentityMetrics (backend) — a group is
  // two or more `securities` rows sharing one institution_security_id under different
  // plaid_security_id values, the shape a Plaid corporate action can fork one instrument's
  // identity into. Detection only: nothing here (or on the backend) merges anything.
  //
  const forkedSecurityIdentitiesAvailable = data.forked_security_identities?.available;

  // #1631 M8b N1/I1. Same fail-closed shape and the same optional-chaining guard as every gauge
  // above: a FAILED derivation and a genuine all-clear both arrive as identical zero-valued
  // fields, and `position_reconciliation` itself can be absent entirely on an older cached
  // response or mid-rolling-deploy. See domain.PositionReconciliationSummary (backend).
  //
  // ⚠ drift_count IS NOT A CONFIRMED-PROBLEM COUNT. See
  // domain.PositionReconciliationReport's "NOT YET FULLY ACTIONABLE" doc comment (backend): a
  // position opened before Plaid's 2-year lookback and topped up ever since (DCA, auto-invest,
  // dividend reinvestment) still reports a drift equal to its entire pre-window base, and that
  // shape is expected to be the DOMINANT contributor to this count today, not a rare residual.
  // The card's helper text below carries this caveat so the number is never read as a to-do list.
  const positionReconciliationAvailable = data.position_reconciliation?.available;

  return (
    <div className="p-8 max-w-[1400px] mx-auto max-md:p-4">
      <header className="flex justify-between items-center mb-8 flex-wrap gap-4 max-md:flex-col max-md:items-start">
        <div className="flex flex-col gap-2">
          <h1 className="m-0 text-3xl font-semibold text-foreground">System Health</h1>
          <span className="text-sm text-muted-foreground">
            Status:{' '}
            <strong className={statusTextClass(data.status)}>{data.status.toUpperCase()}</strong>
          </span>
        </div>
        <div className="flex gap-2 max-md:w-full">
          <button
            onClick={() => setAutoRefresh(!autoRefresh)}
            className="flex items-center gap-2 px-4 py-2 border border-border rounded-md bg-card text-foreground text-sm cursor-pointer hover:bg-accent transition-colors max-md:flex-1 max-md:justify-center"
          >
            <RefreshCw className={cn('w-4 h-4', autoRefresh && 'animate-spin')} size={16} />
            Auto-refresh: {autoRefresh ? 'ON' : 'OFF'}
          </button>
          <button
            onClick={() => refetch()}
            className="flex items-center gap-2 px-4 py-2 border border-border rounded-md bg-card text-foreground text-sm cursor-pointer hover:bg-accent transition-colors max-md:flex-1 max-md:justify-center"
          >
            <RefreshCw size={16} />
            Refresh
          </button>
        </div>
      </header>

      {/* Dead Letter Alert */}
      {data.dead_letter_queue.count > 0 && (
        <div className="flex items-center gap-4 p-4 mb-8 bg-warning/10 border border-warning/20 rounded-lg text-warning">
          <AlertTriangle size={20} />
          <div className="flex-1">
            <strong>Dead Letter Queue Alert:</strong> {data.dead_letter_queue.count} failed jobs
            (oldest: {data.dead_letter_queue.oldest_job_age_hours}h ago)
          </div>
        </div>
      )}

      {/* Service Status Section */}
      <section className="mb-8">
        <h2 className="m-0 mb-4 text-xl font-semibold text-foreground">Services</h2>
        <div className="grid gap-4 [grid-template-columns:repeat(auto-fit,minmax(250px,1fr))] max-md:[grid-template-columns:1fr]">
          <SummaryCard
            title="Database"
            value={`${data.services.database.connections.active}/${data.services.database.connections.max} connections`}
            icon={<Database size={24} />}
            status={statusToVariant(data.services.database.status)}
          />
          <SummaryCard
            title="Clerk"
            value={`${data.services.clerk.latency_ms}ms`}
            icon={<Shield size={24} />}
            status={statusToVariant(data.services.clerk.status)}
          />
          <SummaryCard
            title="Plaid"
            value={data.services.plaid.environment}
            icon={<Landmark size={24} />}
            status={statusToVariant(data.services.plaid.status)}
          />
        </div>
      </section>

      {/* Sync Status Section */}
      <section className="mb-8">
        <h2 className="m-0 mb-4 text-xl font-semibold text-foreground">Plaid Sync Status</h2>
        <div className="grid gap-4 [grid-template-columns:repeat(auto-fit,minmax(250px,1fr))] max-md:[grid-template-columns:1fr]">
          <SummaryCard
            title="Active Syncs"
            value={data.sync_status.active_syncs.toString()}
            icon={<RefreshCw size={24} />}
            status="neutral"
          />
          <SummaryCard
            title="Failed (Last Hour)"
            value={data.sync_status.failed_last_hour.toString()}
            icon={<AlertTriangle size={24} />}
            status={data.sync_status.failed_last_hour > 5 ? 'warning' : 'success'}
          />
          <SummaryCard
            title="Avg Duration"
            value={`${Math.round(data.sync_status.average_duration_ms / 1000)}s`}
            icon={<Clock size={24} />}
            status="neutral"
          />
        </div>
      </section>

      {/* Paddle Status Section */}
      <RevenueCatHealthSummary />
      <section className="mb-8">
        <h2 className="m-0 mb-4 text-xl font-semibold text-foreground">Paddle Status</h2>
        <div className="grid gap-4 [grid-template-columns:repeat(auto-fit,minmax(250px,1fr))] max-md:[grid-template-columns:1fr]">
          <SummaryCard
            title="Active Subscriptions"
            value={data.paddle_status.active_subscriptions.toString()}
            icon={<CreditCard size={24} />}
            status="neutral"
          />
          <SummaryCard
            title="Webhook Success (24h)"
            value={`${data.paddle_status.webhook_success_rate_24h.toFixed(1)}%`}
            icon={<CheckCircle size={24} />}
            status={data.paddle_status.webhook_success_rate_24h < 90 ? 'warning' : 'success'}
          />
          <SummaryCard
            title="Failed Webhooks (24h)"
            value={data.paddle_status.failed_webhooks_24h.toString()}
            icon={<AlertTriangle size={24} />}
            status={data.paddle_status.failed_webhooks_24h > 5 ? 'warning' : 'success'}
          />
          <SummaryCard
            title="Last Reconciliation"
            value={
              data.paddle_status.last_reconciliation_at
                ? new Date(data.paddle_status.last_reconciliation_at).toLocaleString()
                : 'Never'
            }
            icon={<RefreshCw size={24} />}
            status={data.paddle_status.last_reconciliation_at ? 'success' : 'warning'}
          />
        </div>
      </section>

      {/* Job Backlog Section (Issue #1458) — the 3 DB-derived Plaid-cleanup/manual-tier-override
          backlog gauges PR #1443 introduced. Copies the Paddle Status SummaryCard grid above (the
          ticket that scoped this section originally cited a Dead Letter Queue SummaryCard to copy,
          but DLQ actually renders as the conditional alert banner above, not a SummaryCard — this
          section follows Paddle Status instead). The other four metrics #1443 added (three
          lifetime Prometheus counters plus the in-process JobLastSuccessTimestamp heartbeat)
          deliberately stay Prometheus-only — see domain.JobBacklogMetrics's doc comment (backend)
          for why.

          Follow-up G16: `jobBacklogAvailable` (below) distinguishes a query that legitimately
          found a real zero backlog from one that FAILED and silently reported the same zero —
          #1458's own reasoning for excluding PlaidCleanupServiceNotConfiguredTotal from this
          response ("unknown != zero") applied to those four metrics but not, until now, to these
          three. `!available` renders 'Unavailable' with a warning accent instead of a green "0"
          the operator would otherwise read as "no backlog" when the truth is "we don't know".
          Scope discipline: this only makes the failure VISIBLE — whether a non-zero (or
          unavailable) backlog should also degrade the overall /admin/health status is a separate
          product decision #1458 never asked for, so `data.status` is untouched here. */}
      <section className="mb-8">
        <h2 className="m-0 mb-4 text-xl font-semibold text-foreground">Job Backlog</h2>
        <div className="grid gap-4 [grid-template-columns:repeat(auto-fit,minmax(250px,1fr))] max-md:[grid-template-columns:1fr]">
          <SummaryCard
            title="Plaid Cleanup Outstanding Users"
            value={jobBacklogAvailable ? data.job_backlog.plaid_cleanup_outstanding_users.toString() : 'Unavailable'}
            icon={<Users size={24} />}
            status={!jobBacklogAvailable ? 'warning' : (data.job_backlog.plaid_cleanup_outstanding_users > 0 ? 'warning' : 'success')}
          />
          <SummaryCard
            title="Plaid Cleanup Oldest Outstanding"
            value={jobBacklogAvailable ? formatBacklogAge(data.job_backlog.plaid_cleanup_oldest_outstanding_seconds) : 'Unavailable'}
            icon={<Clock size={24} />}
            status={!jobBacklogAvailable ? 'warning' : (data.job_backlog.plaid_cleanup_oldest_outstanding_seconds > 0 ? 'warning' : 'success')}
          />
          <SummaryCard
            title="Manual Override Oldest Uncleared Expired"
            value={jobBacklogAvailable ? formatBacklogAge(data.job_backlog.manual_tier_override_oldest_uncleared_expired_seconds) : 'Unavailable'}
            icon={<Timer size={24} />}
            status={!jobBacklogAvailable ? 'warning' : (data.job_backlog.manual_tier_override_oldest_uncleared_expired_seconds > 0 ? 'warning' : 'success')}
          />
        </div>
      </section>

      {/* Orphaned Plaid Items Section (Issue #1497 item 5) — Plaid Items that survived a GDPR
          erasure. Placed immediately after Job Backlog because it is the same class of signal
          ("how much stuck work is there, and does a human have to do it?") and reuses the same
          SummaryCard grid.

          ⚠ THE TWO ACTIONABLE STATES ARE TWO CARDS AND MUST STAY TWO CARDS. They call for OPPOSITE
          responses: "Needs Manual Removal At Plaid" means go remove the Item in Plaid's own
          dashboard or it may keep billing an erased user; "Auto-Retrying" means do nothing, a sweep
          holds a live credential and is working on it. A single "unresolved" number — the obvious
          shorter rendering — erases exactly that distinction. Resolved rows are history and are
          deliberately not shown: they are never work, and nothing prunes them, so the count only
          ever grows and would dominate the section visually while meaning nothing.

          ⚠ Operator-attested rows (Issue #1497 item 2) ARE shown, unlike resolved ones, and that is
          not an inconsistency: a resolved row is a removal Plaid confirmed, whereas an attested row
          is one a colleague merely claimed, so it is still an open question rather than history. It
          is deliberately not accented as success and its title says "unverified".

          Scope discipline, matching the Job Backlog section: this only makes the state VISIBLE.
          Whether outstanding orphans should degrade `data.status` is a product decision #1497 never
          asked for, so it is untouched. */}
      <section className="mb-8">
        <h2 className="m-0 mb-4 text-xl font-semibold text-foreground">Orphaned Plaid Items</h2>
        <div className="grid gap-4 [grid-template-columns:repeat(auto-fit,minmax(250px,1fr))] max-md:[grid-template-columns:1fr]">
          <SummaryCard
            title="Needs Manual Removal At Plaid"
            value={orphanedItemsAvailable ? data.plaid_orphaned_items.manual_intervention_required.toString() : 'Unavailable'}
            icon={<AlertTriangle size={24} />}
            status={!orphanedItemsAvailable ? 'warning' : (data.plaid_orphaned_items.manual_intervention_required > 0 ? 'warning' : 'success')}
          />
          <SummaryCard
            title="Auto-Retrying"
            value={orphanedItemsAvailable ? data.plaid_orphaned_items.auto_retrying.toString() : 'Unavailable'}
            icon={<RefreshCw size={24} />}
            status={!orphanedItemsAvailable ? 'warning' : 'success'}
          />
          {/* ⚠ ITS OWN CARD, AND THE TITLE SAYS UNVERIFIED. This is the count of Items a colleague
              SAID they removed by hand (Issue #1497 item 2); "Resolved" would be the count Plaid
              confirmed. Summing them would produce a single "done" number whose accuracy nobody
              could state, on the surface whose whole job is to report what is genuinely outstanding.
              Status is deliberately never 'success': nothing here has been verified, and the
              automated sweep is still re-attempting these rows precisely because it might yet
              disprove the claim. */}
          <SummaryCard
            title="Operator-Attested (Unverified)"
            value={orphanedItemsAvailable ? data.plaid_orphaned_items.operator_attested.toString() : 'Unavailable'}
            icon={<UserCheck size={24} />}
            status={!orphanedItemsAvailable ? 'warning' : 'neutral'}
          />
          <SummaryCard
            title="Worst Retry Attempt Count"
            value={orphanedItemsAvailable ? data.plaid_orphaned_items.max_unresolved_retry_attempt_count.toString() : 'Unavailable'}
            icon={<Timer size={24} />}
            status={!orphanedItemsAvailable ? 'warning' : (data.plaid_orphaned_items.max_unresolved_retry_attempt_count > ORPHAN_RETRY_ATTEMPT_WARN_THRESHOLD ? 'warning' : 'success')}
          />
        </div>
      </section>

      {/* Deletion Residue Section (Issue #1504) — GDPR deletions that were REVOKED after the user's
          Paddle subscription-cancel call had already landed. The gate passed a moment earlier and a
          network call cannot be recalled, so the cancel went through; the cascade then correctly
          refused to delete anything. Each of these is a real person who kept their account and every
          byte of their data, and has no subscription. Nobody tells them, and nothing automated
          restores it — re-subscribing somebody automatically would be a worse failure than telling a
          human, which is why this is a dashboard section and not a repair job.

          Placed after Orphaned Plaid Items because it is the third instance of the same class of
          signal on this page ("work only a human can finish"), and it reuses the same SummaryCard grid
          rather than introducing a fourth shape.

          ⚠ THIS COUNT NEVER GOES BACK DOWN, AND THAT IS DELIBERATE. There is no resolution column for
          a residue record, nothing prunes audit_logs, and no record anywhere proves the affected user
          was made whole — so a "handled" affordance here would be recording a claim as a proof, which
          is precisely what the Operator-Attested card above exists to keep separate from Resolved. The
          "Most Recent Occurrence" card carries the is-this-happening-now signal instead.

          Scope discipline, matching both sections above: this only makes the residue VISIBLE. Whether
          it should degrade `data.status` is a product decision #1504 never asked for, so `data.status`
          is untouched. */}
      <section className="mb-8">
        <h2 className="m-0 mb-4 text-xl font-semibold text-foreground">Deletion Residue</h2>
        <div className="grid gap-4 [grid-template-columns:repeat(auto-fit,minmax(250px,1fr))] max-md:[grid-template-columns:1fr]">
          <SummaryCard
            title="Subscriptions Cancelled By A Revoked Deletion"
            value={deletionResidueAvailable ? deletionResidueRecords.toString() : 'Unavailable'}
            icon={<UserX size={24} />}
            status={!deletionResidueAvailable ? 'warning' : (deletionResidueRecords > 0 ? 'warning' : 'success')}
          />
          {/* ⚠ 'None' AND NOT '0s'. formatBacklogAge(0) renders '0s', which on an age card reads as
              "this happened seconds ago" — the exact opposite of the truth when the real answer is
              "this has never happened". The count is what distinguishes the two, so it gates the
              branch. */}
          <SummaryCard
            title="Most Recent Occurrence"
            value={
              !deletionResidueAvailable
                ? 'Unavailable'
                : deletionResidueRecords === 0
                  ? 'None'
                  : `${formatBacklogAge(data.deletion_residue.newest_record_seconds)} ago`
            }
            icon={<History size={24} />}
            status={!deletionResidueAvailable ? 'warning' : (deletionResidueRecords > 0 ? 'warning' : 'success')}
          />
        </div>
      </section>

      {/* Rejected Webhook Attempts Section (Issue #1514, requirement #5) — the operator-visibility
          signal restored after Phase 1 stopped persisting unsigned/invalid-signature Paddle and
          Plaid webhooks into paddle_webhook_events / webhook_events. /admin/webhooks reads that
          table, which by construction can NEVER again contain one of these rows, so during a live
          forgery flood that page renders "No webhook events found" — read by an operator as "no
          attacks", when it actually means "we stopped recording rejections where you're looking."
          This section is the replacement signal, and its own section (not folded into Paddle
          Status) precisely because of the next paragraph.

          ⚠ NOT THE SAME THING AS "Failed Webhooks (24h)" IN THE PADDLE STATUS SECTION ABOVE, AND
          THE TWO MUST NEVER BE WIRED TO THE SAME SOURCE. That card counts webhooks that PASSED
          signature verification and then failed during processing (an app bug, a transient
          dependency failure). This section counts webhooks that FAILED signature verification and
          were never written to that full webhook table at all — someone sending a request that
          never proved it came from Paddle or Plaid (they are still recorded, into the separate,
          bounded invalid_webhook_attempts table this section reads). A forgery attempt and a
          processing bug are unrelated failure modes; see
          domain.RejectedWebhookAttemptMetrics's own doc comment (backend) for the same warning
          stated at the source of truth.

          A non-zero count here is deliberately rendered as 'warning', matching this page's
          existing convention (Job Backlog, Orphaned Plaid Items, Deletion Residue above): it is
          noteworthy, not neutral — see the brief's framing that a non-zero reading here should not
          read as routine. */}
      <section className="mb-8">
        <h2 className="m-0 mb-4 text-xl font-semibold text-foreground">Rejected Webhook Attempts</h2>
        <p className="mt-0 mb-4 text-sm text-muted-foreground max-w-2xl">
          Deliveries rejected before verification could confirm they came from Paddle or Plaid.
          These never appear on the Webhooks page — it only shows deliveries that passed signature
          verification.
        </p>
        <div className="grid gap-4 [grid-template-columns:repeat(auto-fit,minmax(250px,1fr))] max-md:[grid-template-columns:1fr]">
          <SummaryCard
            title="Rejected Attempts (24h)"
            value={rejectedWebhookAttemptsAvailable ? rejectedWebhookAttemptsTotal.toString() : 'Unavailable'}
            icon={<Shield size={24} />}
            status={
              !rejectedWebhookAttemptsAvailable
                ? 'warning'
                : rejectedWebhookAttemptsTotal > 0
                  ? 'warning'
                  : 'success'
            }
          />
          <SummaryCard
            title="Most Recent Attempt"
            value={
              !rejectedWebhookAttemptsAvailable
                ? 'Unavailable'
                : data.rejected_webhook_attempts_24h.most_recent_at
                  ? new Date(data.rejected_webhook_attempts_24h.most_recent_at).toLocaleString()
                  : 'None'
            }
            icon={<Clock size={24} />}
            status={
              !rejectedWebhookAttemptsAvailable
                ? 'warning'
                : data.rejected_webhook_attempts_24h.most_recent_at
                  ? 'warning'
                  : 'success'
            }
          />
          {rejectedWebhookAttemptsAvailable &&
            Object.entries(rejectedWebhookAttemptsByProvider).map(([provider, count]) => (
              <SummaryCard
                key={provider}
                title={`${provider.charAt(0).toUpperCase()}${provider.slice(1)} Attempts (24h)`}
                value={count.toString()}
                icon={<AlertTriangle size={24} />}
                status={count > 0 ? 'warning' : 'success'}
              />
            ))}
        </div>
      </section>

      {/* Shared Institution Security IDs Section (#1631 M8b, narrowed by CX7) — a Plaid corporate
          action can reissue plaid_security_id, silently forking one real instrument's identity
          into multiple `securities` rows with separate transaction/holding histories. Detection
          only: nothing here (or on the backend) merges anything.

          ⚠ CX7: this is a CANDIDATE count, not a confirmed fork count, and the section used to
          claim otherwise. institution_security_id is documented INSTITUTION-SCOPED — it is "the
          identifier given to the security by the institution" — while the grouping behind this
          number has no institution dimension at all, because `securities` carries no institution
          column and is shared across every user and every provider. Two institutions that happen
          to reuse the same internal id therefore group together and are indistinguishable, from
          here, from one instrument whose Plaid id was reissued. The heading, the card title and
          the helper text all say candidate for that reason: the metric must not assert a fork it
          cannot establish.

          Per-group member detail (which securities share the id, `.groups`) is in the generated
          API client but not rendered here yet — only the count renders. Listing the members is a
          UI follow-up, not blocked on generation, and is what would let an operator settle a
          candidate by eye.

          Scope discipline, matching every other gauge section on this page: this only makes the
          count VISIBLE. Whether a non-zero count should degrade `data.status` is a product
          decision this task never asked for, so `data.status` is untouched — and a candidate
          count is not something to gate anything on regardless. */}
      <section className="mb-8">
        <h2 className="m-0 mb-4 text-xl font-semibold text-foreground">Shared Institution Security IDs</h2>
        <p className="mt-0 mb-4 text-sm text-muted-foreground max-w-2xl">
          Groups of securities rows sharing one institution_security_id under different Plaid
          security ids — the shape a corporate-action reissue leaves behind. A review list, not a
          problem count.
        </p>
        <div className="grid gap-4 [grid-template-columns:repeat(auto-fit,minmax(250px,1fr))] max-md:[grid-template-columns:1fr]">
          <SummaryCard
            title="Candidate Groups"
            value={forkedSecurityIdentitiesAvailable ? data.forked_security_identities.group_count.toString() : 'Unavailable'}
            icon={<GitFork size={24} />}
            status={!forkedSecurityIdentitiesAvailable ? 'warning' : 'neutral'}
          />
        </div>
        <p className="mt-3 mb-0 text-xs text-muted-foreground max-w-2xl">
          These are candidates, not confirmed forks. institution_security_id is institution-scoped,
          and this grouping has no institution dimension — securities rows carry no institution —
          so two institutions reusing the same internal id land in one group and cannot be told
          apart from a genuine reissue. Check the members before acting on a group.
        </p>
      </section>

      {/* Position Reconciliation Section (#1631 M8b I1/N1) — the trade-vs-position diagnostic's
          own admin gauge: how many active holdings' quantities disagree with what the trade
          ledger (buys, sells, reinvestments, transfers/splits) implies. Global across every user,
          matching the Forked Security Identities section above.

          ⚠ "Drift Positions" IS NOT A CONFIRMED-PROBLEM COUNT — see
          domain.PositionReconciliationReport's "NOT YET FULLY ACTIONABLE" doc comment (backend).
          A position opened before Plaid's 2-year lookback and topped up ever since (DCA,
          auto-invest, dividend reinvestment) still reports a drift equal to its entire pre-window
          base, and that shape is expected to be the DOMINANT contributor to this count today, not
          a rare residual. The card's own helper text repeats this so the number is never read in
          isolation from its caveat.

          "Suppressed (Pre-Window)" is the sibling count this diagnostic COULD exclude — positions
          with no buy anywhere in the ledger, the one case Plaid's lookback edge can be proven to
          explain. It is informational, not a second problem count.

          Scope discipline, matching every other gauge section on this page: this only makes the
          counts VISIBLE. Whether a non-zero drift count should degrade `data.status` is a product
          decision this task never asked for, so `data.status` is untouched — and DriftCount is
          not yet trustworthy enough to gate anything on regardless, per the caveat above.

          ⚠ #1631 CX6: the card itself renders NEUTRAL at any count, not warning. It was yellow
          on any nonzero count, which — given the caveat two paragraphs up says the DCA population
          is expected to dominate — made an ordinary portfolio paint this dashboard permanently
          yellow. A gauge that is always yellow teaches an operator to stop reading it, which is
          precisely what the caveat exists to prevent. */}
      <section className="mb-8">
        <h2 className="m-0 mb-4 text-xl font-semibold text-foreground">Position Reconciliation</h2>
        <p className="mt-0 mb-4 text-sm text-muted-foreground max-w-2xl">
          Active holdings whose quantity disagrees with what the trade ledger implies. Not yet a
          confirmed-problem count — see below.
        </p>
        <div className="grid gap-4 [grid-template-columns:repeat(auto-fit,minmax(250px,1fr))] max-md:[grid-template-columns:1fr]">
          <SummaryCard
            title="Drift Positions"
            value={positionReconciliationAvailable ? data.position_reconciliation.drift_count.toString() : 'Unavailable'}
            // An alert triangle is the same claim the warning colour was making. This is a
            // diagnostic reading, not an alarm (#1631 CX6).
            icon={<Activity size={24} />}
            // #1631 CX6: NEUTRAL when available, at any count. This metric's own documentation
            // says a long-held DCA position reports its entire pre-window base as drift forever
            // and is expected to be the DOMINANT population, so a warning here makes an ordinary
            // portfolio render permanently yellow — which trains an operator to ignore the
            // dashboard, the exact outcome that documentation says it wants to avoid. Unavailable
            // stays a warning, because unknown is not zero.
            status={!positionReconciliationAvailable ? 'warning' : 'neutral'}
          />
          <SummaryCard
            title="Suppressed (Pre-Window)"
            value={positionReconciliationAvailable ? data.position_reconciliation.suppressed_pre_window_count.toString() : 'Unavailable'}
            icon={<Clock size={24} />}
            status={!positionReconciliationAvailable ? 'warning' : 'neutral'}
          />
          <SummaryCard
            title="Total Positions Checked"
            value={positionReconciliationAvailable ? data.position_reconciliation.total_positions_checked.toString() : 'Unavailable'}
            icon={<Landmark size={24} />}
            status={!positionReconciliationAvailable ? 'warning' : 'neutral'}
          />
        </div>
        <p className="mt-3 mb-0 text-xs text-muted-foreground max-w-2xl">
          Drift Positions is NOT a confirmed-problem count: a long-held position topped up by
          ongoing contributions (DCA, auto-invest, dividend reinvestment) reports a drift equal to
          its entire pre-2-year-lookback base, and this is expected to be the dominant contributor
          today — investigate before treating a non-zero count as a to-do list.
        </p>
      </section>

      {/* Job Queues Section */}
      <section className="mb-8">
        <h2 className="m-0 mb-4 text-xl font-semibold text-foreground">Job Queues</h2>
        <div className="grid gap-4 [grid-template-columns:repeat(auto-fit,minmax(280px,1fr))] max-md:[grid-template-columns:1fr]">
          {Object.entries(data.job_queues).map(([queueName, metrics]) => (
            <Card key={queueName}>
              <h3 className="m-0 mb-3 text-base font-semibold text-foreground">{queueName}</h3>
              {metrics.unavailable ? <p className="text-warning">Unavailable</p> : <div className="flex flex-col gap-2">
                <div className="flex justify-between items-center">
                  <span className="text-sm text-muted-foreground">Pending:</span>
                  <span className="text-sm font-semibold text-foreground">{metrics.pending}</span>
                </div>
                <div className="flex justify-between items-center">
                  <span className="text-sm text-muted-foreground">Running:</span>
                  <span className="text-sm font-semibold text-info">{metrics.running}</span>
                </div>
                <div className="flex justify-between items-center">
                  <span className="text-sm text-muted-foreground">Failed:</span>
                  <span className={cn('text-sm font-semibold', metrics.failed > 0 ? 'text-destructive' : 'text-foreground')}>
                    {metrics.failed}
                  </span>
                </div>
                <div className="flex justify-between items-center">
                  <span className="text-sm text-muted-foreground">Completed (1h):</span>
                  <span className="text-sm font-semibold text-foreground">{metrics.completed_last_hour}</span>
                </div>
              </div>}
            </Card>
          ))}
        </div>
      </section>
    </div>
  );
}
