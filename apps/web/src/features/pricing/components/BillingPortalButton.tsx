import { Button } from '@/components/ui/button'

interface BillingPortalButtonProps {
  billingPortalUrl: string | undefined
  isLoadingPortal: boolean
  /** CTA label: "Resubscribe" for cancelled, "Update Payment Method" for past_due. */
  label: string
}

export function BillingPortalButton({
  billingPortalUrl,
  isLoadingPortal,
  label,
}: BillingPortalButtonProps) {
  const isDisabled = !billingPortalUrl || isLoadingPortal

  const openBillingPortal = () => {
    if (billingPortalUrl) {
      window.open(billingPortalUrl, '_blank', 'noopener,noreferrer')
    }
  }

  return (
    <Button
      onClick={openBillingPortal}
      disabled={isDisabled}
      variant="outline"
      className="w-full mt-2"
    >
      {isLoadingPortal ? 'Loading...' : label}
    </Button>
  )
}
