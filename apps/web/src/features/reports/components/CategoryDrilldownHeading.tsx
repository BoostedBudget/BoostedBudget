interface Props {
  drilldown: { category: string; displayLabel: string } | undefined
  onBack: () => void
}

export function CategoryDrilldownHeading({ drilldown, onBack }: Props) {
  return (
    <h2 id="category-heading" className="text-lg font-semibold">
      Spending by Category
      {drilldown && (
        <span className="ml-2 text-sm font-normal text-muted-foreground">
          — {drilldown.displayLabel}{' '}
          <button onClick={onBack} className="underline hover:no-underline">
            (back)
          </button>
        </span>
      )}
    </h2>
  )
}
