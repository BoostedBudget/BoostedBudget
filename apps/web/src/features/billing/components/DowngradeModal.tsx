import { useState, useMemo } from 'react'
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
  features: string[]
}

interface DowngradeModalProps {
  open: boolean
  currentTier: TierInfo
  downgradeTier: TierInfo
  nextBillingDate: string
  onDowngrade: (tierId: string) => void
  onClose: () => void
}

export function DowngradeModal({
  open,
  currentTier,
  downgradeTier,
  nextBillingDate,
  onDowngrade,
  onClose,
}: DowngradeModalProps) {
  const [confirmed, setConfirmed] = useState(false)

  const lostFeatures = useMemo(() => {
    const downgradeFeatureSet = new Set(downgradeTier.features.map((f) => f.toLowerCase()))
    return currentTier.features.filter(
      (feature) => !downgradeFeatureSet.has(feature.toLowerCase())
    )
  }, [currentTier.features, downgradeTier.features])

  const handleClose = () => {
    setConfirmed(false)
    onClose()
  }

  const handleDowngrade = () => {
    if (confirmed) {
      onDowngrade(downgradeTier.id)
    }
  }

  const lostFeaturesText = lostFeatures.length > 0 ? lostFeatures.join(', ') : 'some features'

  return (
    <Dialog open={open} onOpenChange={(isOpen) => !isOpen && handleClose()}>
      <DialogContent className="max-w-4xl max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Downgrade Your Plan</DialogTitle>
          <DialogDescription>
            Review the features you will lose access to when downgrading from {currentTier.name} to{' '}
            {downgradeTier.name}.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-6">
          <div
            role="alert"
            className="bg-amber-50 border border-amber-200 rounded-lg p-4"
            aria-label="Downgrade warning"
          >
            <p className="text-sm text-amber-900">
              <strong>Warning:</strong> You will lose access to some features when downgrading.
              Please review the comparison below carefully.
            </p>
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
                  Current
                </span>
              </div>
              <div className="mb-4">
                <div className="text-2xl font-bold">{currentTier.price_monthly}</div>
                <div className="text-sm text-gray-600">per month</div>
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

            <div
              className="border-2 border-amber-500 rounded-lg p-4 bg-amber-50"
              aria-label="Downgrade plan"
            >
              <div className="flex items-center justify-between mb-4">
                <h3 className="text-lg font-semibold">{downgradeTier.name}</h3>
                <span
                  className="text-xs px-2 py-1 bg-amber-200 text-amber-900 rounded"
                  aria-label="Downgrade badge"
                >
                  Downgrade
                </span>
              </div>
              <div className="mb-4">
                <div className="text-2xl font-bold">{downgradeTier.price_monthly}</div>
                <div className="text-sm text-gray-600">per month</div>
              </div>
              <ul className="space-y-2" aria-label="Downgrade plan features">
                {downgradeTier.features.map((feature, index) => (
                  <li key={index} className="flex items-start gap-2 text-sm">
                    <span className="text-green-600" aria-hidden="true">
                      ✓
                    </span>
                    <span className="font-medium">{feature}</span>
                  </li>
                ))}
              </ul>
            </div>
          </div>

          <div
            role="note"
            className="bg-blue-50 border border-blue-200 rounded-lg p-4"
            aria-label="Effective date information"
          >
            <p className="text-sm text-blue-900">
              <strong>Effective Date:</strong> Your downgrade will take effect at the end of your
              current billing period on <strong>{nextBillingDate}</strong>. You'll continue to have
              access to all {currentTier.name} features until then.
            </p>
          </div>

          <div
            className="border border-gray-300 rounded-lg p-4 bg-white"
            role="group"
            aria-label="Confirmation checkbox"
          >
            <label className="flex items-start gap-3 cursor-pointer">
              <input
                type="checkbox"
                checked={confirmed}
                onChange={(e) => setConfirmed(e.target.checked)}
                className="mt-1 w-4 h-4"
                aria-label={`Confirm understanding of feature loss: ${lostFeaturesText}`}
              />
              <span className="text-sm">
                I understand that I will lose access to certain features, including{' '}
                <strong>{lostFeaturesText}</strong>, when I downgrade to {downgradeTier.name}.
              </span>
            </label>
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={handleClose} aria-label="Cancel downgrade">
            Cancel
          </Button>
          <Button
            onClick={handleDowngrade}
            disabled={!confirmed}
            variant="destructive"
            aria-label="Confirm downgrade to lower tier"
          >
            Confirm Downgrade
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
