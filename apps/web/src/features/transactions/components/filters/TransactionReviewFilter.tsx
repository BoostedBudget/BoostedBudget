interface TransactionReviewFilterProps {
  value: boolean | undefined
  onChange: (value: boolean | undefined) => void
  disabled?: boolean
}

const options = [
  { label: 'All', value: undefined },
  { label: 'Unreviewed', value: false },
  { label: 'Reviewed', value: true },
] as const

export function TransactionReviewFilter({ value, onChange, disabled }: TransactionReviewFilterProps) {
  return (
    <fieldset className="flex flex-col gap-1 min-w-0">
      <legend className="text-sm font-medium text-foreground mb-1">Review status</legend>
      <div className="inline-flex rounded-lg border border-border bg-muted p-1">
        {options.map((option) => (
          <button
            key={option.label}
            type="button"
            onClick={() => onChange(option.value)}
            disabled={disabled}
            aria-pressed={value === option.value}
            className={`px-3 py-1.5 text-sm font-medium rounded-md transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${value === option.value ? 'bg-card text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'} ${disabled ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer'}`}
          >
            {option.label}
          </button>
        ))}
      </div>
    </fieldset>
  )
}
