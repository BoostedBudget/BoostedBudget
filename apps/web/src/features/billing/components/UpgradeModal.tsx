import { useState } from 'react'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'

interface TierInfo {
  id: string
  name: string
  price_monthly: string
  price_yearly: string
  features: string[]
}

interface UpgradeModalProps {
  open: boolean
  currentTier: TierInfo
  availableTiers: TierInfo[]
  onUpgrade: (tierId: string, billingInterval: 'month' | 'year') => void
  onClose: () => void
}

export function UpgradeModal({
  open,
  currentTier,
  availableTiers,
  onUpgrade,
  onClose,
}: UpgradeModalProps) {
  const [billingInterval, setBillingInterval] = useState<'month' | 'year'>('month')
  const [selectedTier] = useState<TierInfo | null>(
    availableTiers.length > 0 ? availableTiers[0] : null
  )

  const handleUpgrade = () => {
    if (selectedTier) {
      onUpgrade(selectedTier.id, billingInterval)
    }
  }

  const getPrice = (tier: TierInfo) => {
    return billingInterval === 'month' ? tier.price_monthly : tier.price_yearly
  }

  const billingPeriodLabel = billingInterval === 'month' ? 'month' : 'year'

  if (availableTiers.length === 0) {
    return (
      <Dialog open={open} onOpenChange={(isOpen) => !isOpen && onClose()}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>No Upgrades Available</DialogTitle>
            <DialogDescription>
              You are already on the highest tier or there are no upgrade options available at this
              time.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button onClick={onClose}>Close</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    )
  }

  return (
    <Dialog open={open} onOpenChange={(isOpen) => !isOpen && onClose()}>
      <DialogContent className="max-w-4xl max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Upgrade Your Plan</DialogTitle>
          <DialogDescription>
            Choose a higher tier to unlock more features. Changes will be prorated immediately.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-6">
          <div
            role="group"
            aria-label="Billing interval selection"
            className="flex items-center justify-center gap-4 p-4 bg-gray-50 rounded-lg"
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

          <div
            className="grid grid-cols-1 sm:grid-cols-2 gap-4"
            role="region"
            aria-label="Tier comparison"
          >
            <div className="border rounded-lg p-4 bg-gray-50" aria-label="Current plan">
              <div className="flex items-center justify-between mb-4">
                <h3 className="text-lg font-semibold">{currentTier.name}</h3>
                <span
                  className="text-xs px-2 py-1 bg-blue-100 text-blue-800 rounded"
                  aria-label="Current plan badge"
                >
                  Current Plan
                </span>
              </div>
              <div className="mb-4">
                <div className="text-2xl font-bold">{getPrice(currentTier)}</div>
                <div className="text-sm text-gray-600">per {billingPeriodLabel}</div>
              </div>
              <ul className="space-y-2" aria-label="Current plan features">
                {currentTier.features.map((feature, index) => (
                  <li key={index} className="flex items-start gap-2 text-sm">
                    <span className="text-gray-400" aria-hidden="true">
                      ✓
                    </span>
                    <span>{feature}</span>
                  </li>
                ))}
              </ul>
            </div>

            {selectedTier && (
              <div
                className="border-2 border-blue-500 rounded-lg p-4 bg-blue-50"
                aria-label="Upgrade plan"
              >
                <div className="flex items-center justify-between mb-4">
                  <h3 className="text-lg font-semibold">{selectedTier.name}</h3>
                  <span
                    className="text-xs px-2 py-1 bg-green-100 text-green-800 rounded"
                    aria-label="Upgrade badge"
                  >
                    Upgrade
                  </span>
                </div>
                <div className="mb-4">
                  <div className="text-2xl font-bold">{getPrice(selectedTier)}</div>
                  <div className="text-sm text-gray-600">per {billingPeriodLabel}</div>
                </div>
                <ul className="space-y-2" aria-label="Upgrade plan features">
                  {selectedTier.features.map((feature, index) => (
                    <li key={index} className="flex items-start gap-2 text-sm">
                      <span className="text-green-600" aria-hidden="true">
                        ✓
                      </span>
                      <span className="font-medium">{feature}</span>
                    </li>
                  ))}
                </ul>
              </div>
            )}
          </div>

          <div
            role="note"
            className="bg-blue-50 border border-blue-200 rounded-lg p-4"
            aria-label="Proration information"
          >
            <p className="text-sm text-blue-900">
              <strong>Note:</strong> Your upgrade will be prorated immediately. You'll be charged
              the difference between your current plan and the new plan for the remainder of your
              billing cycle.
            </p>
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={onClose} aria-label="Cancel upgrade">
            Cancel
          </Button>
          <Button
            onClick={handleUpgrade}
            disabled={!selectedTier}
            aria-label={`Upgrade to ${selectedTier?.name || 'selected tier'}`}
          >
            Upgrade to {selectedTier?.name}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
