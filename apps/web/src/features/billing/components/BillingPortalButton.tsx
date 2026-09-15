import { Button } from '@/components/ui/button'

interface BillingPortalButtonProps {
  billingPortalUrl: string | undefined
  isLoadingPortal: boolean
  /**
   * CTA label. Defaults to the single self-service action (#1652): plan changes, cancellation and
   * payment-method updates all happen in the provider's own portal, so there is one action.
   */
  label?: string
  /**
   * When the portal URL could not be resolved, surface an explicit error state instead of a
   * disabled "dead" button that gives the user no explanation and no way forward.
   */
  error?: unknown
}

export function BillingPortalButton({
  billingPortalUrl,
  isLoadingPortal,
  label = 'Manage subscription',
  error,
}: BillingPortalButtonProps) {
  if (error) {
    return (
      <div
        role="alert"
        className="w-full mt-2 rounded-lg border border-destructive/30 bg-destructive/10 px-3 py-2 text-center text-sm text-destructive"
      >
        Couldn&apos;t load the billing portal. Please try again.
      </div>
    )
  }

  if (isLoadingPortal) {
    return (
      <Button disabled variant="outline" className="w-full mt-2">
        Loading...
      </Button>
    )
  }

  if (!billingPortalUrl) {
    return (
      <Button disabled variant="outline" className="w-full mt-2">
        {label}
      </Button>
    )
  }

  return (
    <Button asChild variant="outline" className="w-full mt-2">
      <a href={billingPortalUrl} target="_blank" rel="noopener noreferrer">
        {label}
      </a>
    </Button>
  )
}
