/**
 * TransactionReversedFilter
 *
 * Issue #1569 T12. Opt in to transactions the BANK withdrew after they had already posted.
 *
 * Off by default, and the default is the decision rather than caution: a tombstoned row landing
 * unrequested in a list the user has already reconciled is the same silent change #1569 exists to
 * stop, arriving from the other side. The API defaults include_reversed=false for the same
 * reason, so the control and the contract agree.
 *
 * The helper text is not decoration. "Reversed" is a word most people have not had to think about
 * before, and a user who turns this on without knowing what it does will read struck-through rows
 * as an error in their data.
 */

import { Switch } from '@/components/ui/switch'

interface TransactionReversedFilterProps {
  includeReversed: boolean
  onChange: (val: boolean) => void
  disabled?: boolean
}

export function TransactionReversedFilter({
  includeReversed,
  onChange,
  disabled,
}: TransactionReversedFilterProps) {
  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex items-center justify-between gap-3">
        <label
          htmlFor="include-reversed-switch"
          className="text-sm font-medium text-foreground cursor-pointer"
        >
          Show reversed transactions
        </label>
        <Switch
          id="include-reversed-switch"
          aria-label="Show reversed transactions"
          aria-describedby="include-reversed-hint"
          checked={includeReversed}
          onCheckedChange={onChange}
          disabled={disabled}
        />
      </div>
      <p id="include-reversed-hint" className="text-xs text-muted-foreground">
        Charges your bank withdrew after they posted. They are kept on record and shown struck
        through, and they never counted toward your spending.
      </p>
    </div>
  )
}
