import { Calendar, CreditCard, ExternalLink } from 'lucide-react'

interface BillingDetailsProps {
  nextBillingDate: string
  amount: string
  currency: string
  billingInterval: string
  portalUrl?: string
  paymentMethod?: string
}

export function BillingDetails({
  nextBillingDate,
  amount,
  currency,
  billingInterval,
  portalUrl,
  paymentMethod,
}: BillingDetailsProps) {
  const billingFrequency = billingInterval === 'year' ? 'Yearly' : 'Monthly'
  const formattedAmount = `${currency} ${amount}`

  return (
    <section className="billing-details-card" aria-label="Billing details">
      <div className="card-header">
        <div className="header-content">
          <Calendar className="header-icon" aria-hidden="true" size={24} />
          <h2 className="card-title">Billing Details</h2>
        </div>
      </div>

      <div className="card-body">
        <dl className="billing-info" role="list" aria-label="Billing information">
          <div className="info-row">
            <dt className="info-label">Next Billing Date:</dt>
            <dd className="info-value">
              <time dateTime={nextBillingDate}>{nextBillingDate}</time>
            </dd>
          </div>

          <div className="info-row">
            <dt className="info-label">Billing Cycle:</dt>
            <dd className="info-value">{billingFrequency}</dd>
          </div>

          <div className="info-row">
            <dt className="info-label">Amount:</dt>
            <dd className="info-value" aria-label={`Amount: ${formattedAmount}`}>
              {formattedAmount}
            </dd>
          </div>

          {paymentMethod && (
            <div className="info-row">
              <dt className="info-label">Payment Method:</dt>
              <dd className="info-value">
                <CreditCard className="payment-icon" aria-hidden="true" size={16} />
                <span aria-label={`Card ending in ${paymentMethod.replace(/•/g, '')}`}>
                  {paymentMethod}
                </span>
              </dd>
            </div>
          )}
        </dl>

        {portalUrl && (
          <div className="portal-link-container">
            <a
              href={portalUrl}
              target="_blank"
              rel="noopener noreferrer"
              className="portal-link"
              aria-label="Manage payment methods in billing portal (opens in new tab)"
            >
              Manage Payment Methods
              <ExternalLink className="external-icon" aria-hidden="true" size={16} />
            </a>
          </div>
        )}
      </div>
    </section>
  )
}
