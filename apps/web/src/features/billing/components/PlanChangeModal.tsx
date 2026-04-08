import { useState, useEffect } from 'react'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { usePreviewPaddleSubscriptionUpdate } from '@boostedbudget/api-client/endpoints/subscription/subscription'
import { SubscriptionTier } from '@boostedbudget/api-client/models'

interface PlanChangeModalProps {
  open: boolean
  currentTier: string
  currentBillingInterval: 'month' | 'year'
  tiers: Record<string, SubscriptionTier>
  isUpdating: boolean
  onChangePlan: (tier: string, interval: 'month' | 'year', prorationMode: 'prorated_immediately' | 'full_next_billing_period') => void
  onClose: () => void
}

export function PlanChangeModal({
  open,
  currentTier,
  currentBillingInterval,
  tiers,
  isUpdating,
  onChangePlan,
  onClose,
}: PlanChangeModalProps) {
  const [selectedTier, setSelectedTier] = useState<string | null>(null)
  const [billingInterval, setBillingInterval] = useState<'month' | 'year'>(currentBillingInterval)
  const [downgradeConfirmed, setDowngradeConfirmed] = useState(false)

  const previewUpdate = usePreviewPaddleSubscriptionUpdate()

  // Reset state when modal closes
  useEffect(() => {
    if (!open) {
      setSelectedTier(null)
      setDowngradeConfirmed(false)
      setBillingInterval(currentBillingInterval)
    }
  }, [open, currentBillingInterval])

  // Trigger preview when tier is selected
  useEffect(() => {
    if (selectedTier && open && tiers[selectedTier]) {
      previewUpdate.mutateAsync({
        data: {
          new_tier: selectedTier,
          billing_interval: billingInterval,
        }
      })
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedTier, billingInterval, open])

  const currentTierData = tiers[currentTier]
  if (!currentTierData) return null

  // Helper to get price for billing interval
  const getPrice = (tier: SubscriptionTier) =>
    billingInterval === 'month' ? tier.pricing.price_monthly : tier.pricing.price_yearly

  // Filter out current tier and sort by price
  const availableTiers = Object.entries(tiers)
    .filter(([key]) => key !== currentTier)
    .sort(([, a], [, b]) => getPrice(a) - getPrice(b))

  const selectedTierData = selectedTier ? tiers[selectedTier] : null

  // Determine if selected tier is upgrade or downgrade
  const isUpgrade = selectedTierData ? getPrice(selectedTierData) > getPrice(currentTierData) : false

  // Helper for billing interval display
  const getBillingLabel = () => billingInterval === 'month' ? 'month' : 'year'

  const canConfirm = selectedTier && (isUpgrade || downgradeConfirmed)

  const handleConfirm = () => {
    if (!selectedTier) return

    const prorationMode = isUpgrade ? 'prorated_immediately' : 'full_next_billing_period'
    onChangePlan(selectedTier, billingInterval, prorationMode)
  }

  const formatPrice = (price: number) => {
    return price === 0 ? 'Free' : `$${price.toFixed(2)}`
  }

  return (
    <Dialog open={open} onOpenChange={(isOpen) => !isOpen && onClose()}>
      <DialogContent className="max-w-5xl max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Change Your Plan</DialogTitle>
          <DialogDescription>
            Select a new tier to change your subscription. Upgrades are prorated immediately,
            downgrades take effect at the end of your billing period.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-6">
          {/* Billing interval selector */}
          <div
            role="group"
            aria-label="Billing interval selection"
            className="flex items-center justify-center gap-4 p-4 bg-muted rounded-lg"
          >
            <label className="flex items-center gap-2 cursor-pointer">
              <input
                type="radio"
                name="billing"
                value="month"
                checked={billingInterval === 'month'}
                onChange={() => setBillingInterval('month')}
                className="w-4 h-4"
                aria-label="Monthly billing"
              />
              <span className="text-sm font-medium">Monthly</span>
            </label>
            <label className="flex items-center gap-2 cursor-pointer">
              <input
                type="radio"
                name="billing"
                value="year"
                checked={billingInterval === 'year'}
                onChange={() => setBillingInterval('year')}
                className="w-4 h-4"
                aria-label="Yearly billing"
              />
              <span className="text-sm font-medium">Yearly</span>
            </label>
          </div>

          {/* Tier selection grid */}
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
            {availableTiers.map(([key, tier]) => {
              const tierPrice = getPrice(tier)
              const currentPrice = getPrice(currentTierData)
              const isUpgradeTier = tierPrice > currentPrice
              const isSelected = selectedTier === key

              const handleSelectTier = () => {
                setSelectedTier(key)
                setDowngradeConfirmed(false)
              }

              return (
                <div
                  key={key}
                  className={`border rounded-lg p-4 cursor-pointer transition-all ${
                    isSelected ? 'border-2 border-blue-500 bg-info/10' : 'border border-border'
                  }`}
                  onClick={handleSelectTier}
                  role="button"
                  tabIndex={0}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' || e.key === ' ') {
                      handleSelectTier()
                    }
                  }}
                >
                  <div className="flex items-center justify-between mb-3">
                    <h3 className="text-lg font-semibold">{tier.display_name}</h3>
                    <span
                      className={`text-xs px-2 py-1 rounded ${
                        isUpgradeTier
                          ? 'bg-green-100 text-green-800'
                          : 'bg-amber-100 text-amber-800'
                      }`}
                    >
                      {isUpgradeTier ? 'Upgrade' : 'Downgrade'}
                    </span>
                  </div>
                  <div className="mb-3">
                    <div className="text-2xl font-bold">{formatPrice(tierPrice)}</div>
                    <div className="text-sm text-muted-foreground">per {getBillingLabel()}</div>
                  </div>
                  <div className="text-sm space-y-1">
                    <div className="text-foreground">{tier.pricing.users_included} users</div>
                    <div className="text-foreground">{tier.limits.accounts} accounts</div>
                  </div>
                </div>
              )
            })}
          </div>

          {/* Preview section */}
          {selectedTier && previewUpdate.isPending && (
            <div className="bg-muted border border-border rounded-lg p-4">
              <div className="flex items-center gap-2">
                <div className="animate-spin h-4 w-4 border-2 border-info border-t-transparent rounded-full"></div>
                <span className="text-sm text-muted-foreground">Loading preview...</span>
              </div>
            </div>
          )}

          {selectedTier && previewUpdate.isSuccess && previewUpdate.data && (
            <div className="bg-info/10 border border-info/30 rounded-lg p-4">
              {isUpgrade ? (
                <div className="text-sm text-info">
                  <p>
                    <strong>Immediate charge:</strong> You'll be charged{' '}
                    {previewUpdate.data.currency} {previewUpdate.data.proration_amount} immediately.
                  </p>
                  <p className="mt-2">
                    <strong>Next billing:</strong> {previewUpdate.data.currency}{' '}
                    {selectedTierData ? formatPrice(getPrice(selectedTierData)) : ''}{' '}
                    on {previewUpdate.data.next_charge_date}
                  </p>
                </div>
              ) : (
                <div className="text-sm text-info">
                  <p>
                    <strong>Changes take effect on {previewUpdate.data.next_charge_date}</strong>
                  </p>
                  <p className="mt-2">You'll keep full access to your current plan until then.</p>
                </div>
              )}
            </div>
          )}

          {/* Downgrade confirmation checkbox */}
          {selectedTier && !isUpgrade && (
            <div className="flex items-start gap-3">
              <input
                type="checkbox"
                id="downgrade-confirm"
                checked={downgradeConfirmed}
                onChange={(e) => setDowngradeConfirmed(e.target.checked)}
                className="mt-1"
              />
              <label htmlFor="downgrade-confirm" className="text-sm text-foreground">
                I understand I'll be switching to {selectedTierData?.display_name} with{' '}
                {selectedTierData?.limits.accounts} accounts and {selectedTierData?.pricing.users_included} users
              </label>
            </div>
          )}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={onClose} disabled={isUpdating}>
            Cancel
          </Button>
          <Button onClick={handleConfirm} disabled={!canConfirm || isUpdating}>
            {isUpdating ? 'Processing...' : 'Confirm'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
