export type TransactionAmountFormat = {
  text: string
  signClass: string
  arrow: 'down' | 'up' | 'none'
  testId: 'expense-arrow' | 'income-arrow' | undefined
}

// PFCv2 convention: positive amount = outflow (spending/debit)
export function isOutflow(amount: number): boolean {
  return amount > 0
}

// PFCv2 convention: negative amount = inflow (refund/income/credit)
export function isInflow(amount: number): boolean {
  return amount < 0
}

export function formatTransactionAmount(amount: number): TransactionAmountFormat {
  const absAmount = Math.abs(amount)
  const formatted = new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  }).format(absAmount)

  if (amount === 0) {
    return {
      text: formatted,
      signClass: 'text-foreground',
      arrow: 'none',
      testId: undefined,
    }
  }

  if (isOutflow(amount)) {
    return {
      text: `-${formatted}`,
      signClass: 'text-destructive',
      arrow: 'down',
      testId: 'expense-arrow',
    }
  }

  // inflow
  return {
    text: `+${formatted}`,
    signClass: 'text-success',
    arrow: 'up',
    testId: 'income-arrow',
  }
}
