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

interface Subscription {
  tier: string
  status: string
  billing_interval: string
  amount: string
  currency: string
  next_billing_date: string
  features: string[]
}

interface CancelConfirmationModalProps {
  open: boolean
  subscription: Subscription
  onConfirm: () => void
  onClose: () => void
}

export function CancelConfirmationModal({
  open,
  subscription,
  onConfirm,
  onClose,
}: CancelConfirmationModalProps) {
  const [step, setStep] = useState<'first' | 'second'>('first')
  const [confirmText, setConfirmText] = useState('')

  useEffect(() => {
    if (!open) {
      setStep('first')
      setConfirmText('')
    }
  }, [open])

  const handleClose = () => {
    setStep('first')
    setConfirmText('')
    onClose()
  }

  const handleProceedToSecond = () => {
    setStep('second')
  }

  const handleGoBack = () => {
    setStep('first')
    setConfirmText('')
  }

  const handleConfirm = () => {
    if (confirmText === 'CANCEL') {
      onConfirm()
    }
  }

  const isConfirmDisabled = confirmText !== 'CANCEL'

  return (
    <Dialog open={open} onOpenChange={(isOpen) => !isOpen && handleClose()}>
      <DialogContent className="max-w-2xl">
        {step === 'first' ? (
          <>
            <DialogHeader>
              <DialogTitle>Cancel Subscription</DialogTitle>
              <DialogDescription>
                Are you sure you want to cancel your {subscription.tier} subscription?
              </DialogDescription>
            </DialogHeader>

            <div className="space-y-4">
              <div
                role="note"
                className="bg-blue-50 border border-blue-200 rounded-lg p-4"
                aria-label="Access retention information"
              >
                <p className="text-sm text-blue-900">
                  <strong>Access Retained:</strong> You'll continue to have full access to all
                  features until your current billing period ends on{' '}
                  <strong>{subscription.next_billing_date}</strong>.
                </p>
              </div>

              <div
                className="border rounded-lg p-4"
                role="region"
                aria-label="Features that will be lost"
              >
                <h3 className="font-semibold mb-3">Features You'll Lose:</h3>
                <ul className="space-y-2" aria-label="List of features">
                  {subscription.features.map((feature, index) => (
                    <li key={index} className="flex items-start gap-2 text-sm">
                      <span className="text-red-500" aria-hidden="true">
                        ✗
                      </span>
                      <span>{feature}</span>
                    </li>
                  ))}
                </ul>
              </div>

              <div
                role="note"
                className="bg-amber-50 border border-amber-200 rounded-lg p-4"
                aria-label="Resubscription notice"
              >
                <p className="text-sm text-amber-900">
                  <strong>Note:</strong> After cancellation, you'll need to resubscribe to regain
                  access to these features.
                </p>
              </div>
            </div>

            <DialogFooter>
              <Button variant="outline" onClick={handleClose}>
                Keep Subscription
              </Button>
              <Button variant="destructive" onClick={handleProceedToSecond}>
                Yes, Cancel
              </Button>
            </DialogFooter>
          </>
        ) : (
          <>
            <DialogHeader>
              <DialogTitle>Final Confirmation</DialogTitle>
              <DialogDescription>
                This action is permanent and cannot be undone.
              </DialogDescription>
            </DialogHeader>

            <div className="space-y-4">
              <div
                role="alert"
                className="bg-red-50 border border-red-200 rounded-lg p-4"
                aria-label="Final warning"
              >
                <p className="text-sm text-red-900">
                  <strong>Warning:</strong> You are about to permanently cancel your{' '}
                  {subscription.tier} subscription. After {subscription.next_billing_date}, you will
                  lose access to all premium features.
                </p>
              </div>

              <div>
                <label htmlFor="confirm-text" className="block text-sm font-medium mb-2">
                  To confirm cancellation, type <strong>CANCEL</strong> below:
                </label>
                <input
                  id="confirm-text"
                  type="text"
                  value={confirmText}
                  onChange={(e) => setConfirmText(e.target.value)}
                  placeholder="Type CANCEL to confirm"
                  className="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500"
                  aria-label="Type CANCEL to confirm cancellation"
                  aria-required="true"
                  autoComplete="off"
                />
              </div>
            </div>

            <DialogFooter>
              <Button variant="outline" onClick={handleGoBack}>
                Go Back
              </Button>
              <Button
                variant="destructive"
                onClick={handleConfirm}
                disabled={isConfirmDisabled}
              >
                Confirm Cancellation
              </Button>
            </DialogFooter>
          </>
        )}
      </DialogContent>
    </Dialog>
  )
}
