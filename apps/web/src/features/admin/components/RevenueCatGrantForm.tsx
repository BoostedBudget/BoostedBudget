import { useId, useState } from 'react'
import type { AdminRevenueCatGrantInputBody } from '@boostedbudget/api-client/models'
import { Button } from '@/components/ui/button'

const tiers: AdminRevenueCatGrantInputBody['tier'][] = ['premium', 'household_2', 'household_3', 'household_4', 'household_5']

export function RevenueCatGrantForm({disabled, pending, onSubmit}: {
  disabled: boolean
  pending: boolean
  onSubmit: (body: AdminRevenueCatGrantInputBody) => void
}) {
  const id = useId()
  const [grantClass, setGrantClass] = useState<AdminRevenueCatGrantInputBody['class']>('comp')
  const [tier, setTier] = useState<AdminRevenueCatGrantInputBody['tier']>('premium')
  const [action, setAction] = useState('grant')
  const [permanent, setPermanent] = useState(false)
  const [expiry, setExpiry] = useState('')
  const [reason, setReason] = useState('')
  const [error, setError] = useState('')
  const revoke = action === 'revoke'
  const finite = !revoke && (grantClass === 'trial' || !permanent)
  const selectClass = 'rounded-md border border-input bg-background p-2 text-sm'

  return <form className="space-y-4" onSubmit={event => {
    event.preventDefault()
    if (disabled || pending) return
    if (!reason.trim()) { setError('Enter a reason.'); return }
    const date = finite ? new Date(expiry) : null
    if (date && (!Number.isFinite(date.getTime()) || date.getTime() <= Date.now())) { setError('Choose a future expiry.'); return }
    setError('')
    onSubmit({class: grantClass, tier: grantClass === 'trial' ? 'premium' : tier, expires_at: date?.toISOString() ?? null, revoke, reason: reason.trim()})
  }}>
    <h3 className="text-lg font-semibold">Request Access Change</h3>
    <p className="text-sm text-muted-foreground">Changes are queued and become effective after provider readback. The server checks eligibility. Extending a trial does not reset trial eligibility or change paid billing.</p>
    <fieldset disabled={disabled || pending} className="space-y-4">
      <div className="grid gap-4 sm:grid-cols-2">
        <label className="flex flex-col gap-1" htmlFor={`${id}-class`}>Grant class
          <select id={`${id}-class`} className={selectClass} value={grantClass} onChange={e => { setGrantClass(e.target.value as AdminRevenueCatGrantInputBody['class']); setAction('grant'); setPermanent(false) }}>
            <option value="comp">Complimentary</option><option value="trial">Trial</option>
          </select>
        </label>
        <label className="flex flex-col gap-1" htmlFor={`${id}-action`}>Action
          <select id={`${id}-action`} className={selectClass} value={action} onChange={e => setAction(e.target.value)}>
            <option value="grant">{grantClass === 'trial' ? 'Extend trial' : 'Grant, extend, or change comp'}</option>
            <option value="revoke">{grantClass === 'trial' ? 'End trial' : 'Revoke comp'}</option>
          </select>
        </label>
        <label className="flex flex-col gap-1" htmlFor={`${id}-tier`}>Tier
          <select id={`${id}-tier`} className={selectClass} value={grantClass === 'trial' ? 'premium' : tier} onChange={e => setTier(e.target.value as AdminRevenueCatGrantInputBody['tier'])}>
            {(grantClass === 'trial' ? ['premium'] : tiers).map(t => <option key={t} value={t}>{t === 'premium' ? 'Premium' : `Household (${t.slice(-1)})`}</option>)}
          </select>
        </label>
        {grantClass === 'comp' && !revoke && <label className="flex items-center gap-2"><input type="checkbox" checked={permanent} onChange={e => setPermanent(e.target.checked)} />Permanent</label>}
        {finite && <label className="flex flex-col gap-1" htmlFor={`${id}-expiry`}>Expires at
          <input className={selectClass} id={`${id}-expiry`} data-ph-mask type="datetime-local" value={expiry} onChange={e => setExpiry(e.target.value)} />
        </label>}
      </div>
      {revoke && <p className="text-sm text-muted-foreground">This ends access from this grant class. Paid subscriptions and household access may still provide access.</p>}
      <label className="flex flex-col gap-1" htmlFor={`${id}-reason`}>Reason
        <textarea className={selectClass} id={`${id}-reason`} data-ph-mask maxLength={1000} value={reason} onChange={e => setReason(e.target.value)} />
      </label>
      {error && <p role="alert" className="text-destructive">{error}</p>}
      <Button type="submit" disabled={disabled || pending}>{pending ? 'Queueing…' : 'Queue change'}</Button>
    </fieldset>
  </form>
}
