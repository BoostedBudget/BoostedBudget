/**
 * Transaction Type Filter Component
 * Toggle buttons for filtering by transaction type: All, Income, Expenses
 * Issue #529: Transaction page filtering
 */

interface TransactionTypeFilterProps {
  value: 'all' | 'income' | 'expense'
  onChange: (value: 'all' | 'income' | 'expense') => void
  disabled?: boolean
}

const options: { value: 'all' | 'income' | 'expense'; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'income', label: 'Income' },
  { value: 'expense', label: 'Expenses' },
]

export function TransactionTypeFilter({ value, onChange, disabled }: TransactionTypeFilterProps) {
  return (
    <div className="flex flex-col gap-1">
      <label className="text-sm font-medium text-foreground">Type</label>
      <div className="inline-flex rounded-lg border border-border bg-muted p-1">
        {options.map((option) => (
          <button
            key={option.value}
            type="button"
            onClick={() => onChange(option.value)}
            disabled={disabled}
            className={`
              px-3 py-1.5 text-sm font-medium rounded-md transition-colors
              ${
                value === option.value
                  ? 'bg-card text-foreground shadow-sm'
                  : 'text-muted-foreground hover:text-foreground'
              }
              ${disabled ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer'}
            `}
            aria-pressed={value === option.value}
          >
            {option.label}
          </button>
        ))}
      </div>
    </div>
  )
}
