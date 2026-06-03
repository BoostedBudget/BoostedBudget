import { Button } from '@/components/ui/button'

interface ResumeButtonProps {
  billingPortalUrl: string | undefined
  isLoadingPortal: boolean
}

export function ResumeButton({ billingPortalUrl, isLoadingPortal }: ResumeButtonProps) {
  const isDisabled = !billingPortalUrl || isLoadingPortal

  const handleResume = () => {
    if (billingPortalUrl) {
      window.open(billingPortalUrl, '_blank', 'noopener,noreferrer')
    }
  }

  return (
    <Button
      onClick={handleResume}
      disabled={isDisabled}
      variant="outline"
      className="w-full mt-2"
    >
      {isLoadingPortal ? 'Loading...' : 'Resume'}
    </Button>
  )
}
