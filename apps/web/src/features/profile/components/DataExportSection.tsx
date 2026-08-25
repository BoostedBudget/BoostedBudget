/**
 * DataExportSection — the surface where a user exercises their GDPR Article 20 right (#1587).
 *
 * It lives beside the other privacy controls rather than in a new page, because that is where the
 * privacy policy tells people to look ("full control over your privacy settings ... and can request
 * data export or deletion at any time") and because export and erasure are two halves of the same
 * decision.
 *
 * ⚠ NO FINANCIAL VALUES ARE RENDERED HERE, so there is nothing to mask. The only number shown is a
 * row COUNT, which is not an amount, not a balance and not a merchant. `data-ph-mask` is applied to
 * it anyway: a per-user row count is a weak but real signal about the size of someone's financial
 * life, and it costs nothing to keep it out of session replay. No PII reaches any `aria-label` here
 * — attributes are recorded verbatim by replay and cannot be masked.
 */
import { useDataExport } from '../hooks/useDataExport'
import { Card } from '../../../components/ui/card'

export function DataExportSection() {
  const { requestExport, isExporting, error, lastExport } = useDataExport()

  return (
    <Card className="mb-6">
      <h3 className="text-xl font-semibold text-foreground mt-0 mb-2">Export Your Data</h3>
      <p className="text-muted-foreground mb-4 leading-relaxed">
        Download everything BoostedBudget holds about you as a single JSON file — accounts,
        transactions, budgets, savings goals, investments and your full history, including records
        that are no longer shown in the app. This is your right to data portability under GDPR
        Article&nbsp;20.
      </p>
      <p className="text-muted-foreground mb-6 text-sm leading-relaxed">
        Your export contains only your own data. If you share a household, other members&apos;
        accounts and transactions are not included. Login credentials and bank access tokens are
        never exported.
      </p>

      <button
        type="button"
        onClick={requestExport}
        disabled={isExporting}
        className="bg-primary text-primary-foreground border-none px-6 py-3 rounded-md text-base font-semibold cursor-pointer hover:opacity-90 transition-opacity disabled:opacity-60 disabled:cursor-not-allowed focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
      >
        {isExporting ? 'Preparing your export…' : 'Download my data'}
      </button>

      {/*
        role="status" (a polite live region) rather than an alert: the result is the answer to
        something the user just asked for, so a screen reader should hear it without having focus
        yanked away from the button they are still on.
      */}
      <div role="status" aria-live="polite" className="mt-4">
        {isExporting && (
          <p className="text-muted-foreground text-sm">
            Gathering your data. This can take a few seconds.
          </p>
        )}
        {lastExport && !isExporting && (
          <p className="text-success text-sm font-medium">
            {/*
              The check mark is decorative and hidden from assistive tech; the success is carried by
              the WORDS and by the text colour together, never by colour alone.
            */}
            <span aria-hidden="true">✓ </span>
            Downloaded{' '}
            <span className="font-semibold">{lastExport.filename}</span> —{' '}
            <span data-ph-mask>{lastExport.rowCount.toLocaleString()}</span> records across{' '}
            <span data-ph-mask>{lastExport.sectionCount}</span> sections.
          </p>
        )}
      </div>

      {error && (
        <p role="alert" className="text-destructive bg-destructive/10 px-4 py-3 rounded-md mt-4 text-sm font-medium">
          <span aria-hidden="true">⚠ </span>
          {error}
        </p>
      )}
    </Card>
  )
}
