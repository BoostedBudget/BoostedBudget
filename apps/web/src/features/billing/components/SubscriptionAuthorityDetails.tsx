import type { RevenueCatSummary } from '@boostedbudget/api-client/models'

export function SubscriptionAuthorityDetails({ summary }: { summary?: RevenueCatSummary }) {
  if (!summary) return null
  return (
    <section aria-label="Access and billing confirmation" className="max-w-[800px] mx-auto px-8 pt-6 space-y-2" data-ph-mask>
      {summary.access_resolved && summary.effective
        ? <p>Effective access: {summary.effective.Tier} ({summary.effective.Status}, {summary.effective.Source})</p>
        : <p>Access confirmation is temporarily unavailable. Your saved data remains available.</p>}
      {summary.paid && <p>Paid access: {summary.paid.tier} ({summary.paid.status})</p>}
      {summary.own && <p>Own access: {summary.own.tier} ({summary.own.status}, {summary.own.source})</p>}
      {summary.comp && <p>Complimentary access: {summary.comp.tier} ({summary.comp.status}){summary.comp.expires_at ? `, ends ${summary.comp.expires_at.slice(0, 10)}` : ''}</p>}
      {summary.trial && <p>Trial access: {summary.trial.tier} ({summary.trial.status}){summary.trial.expires_at ? `, ends ${summary.trial.expires_at.slice(0, 10)}` : ''}</p>}
      {!summary.billing_available && <p>Billing details are temporarily unavailable. You can retry confirmation.</p>}
      {summary.pending_operations > 0 && <p>A subscription update is awaiting confirmation.</p>}
    </section>
  )
}
