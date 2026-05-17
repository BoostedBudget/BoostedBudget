export const CURATED_PRIMARIES: readonly string[] = [
  'BANK_FEES',
  'ENTERTAINMENT',
  'FOOD_AND_DRINK',
  'GENERAL_MERCHANDISE',
  'GENERAL_SERVICES',
  'GOVERNMENT_AND_NON_PROFIT',
  'HOME_IMPROVEMENT',
  'INCOME',
  'LOAN_DISBURSEMENTS',
  'LOAN_PAYMENTS',
  'MEDICAL',
  'PERSONAL_CARE',
  'RENT_AND_UTILITIES',
  'TRANSFER_IN',
  'TRANSFER_OUT',
  'TRANSPORTATION',
  'TRAVEL',
  'OTHER',
]

function titleCase(tokens: string[]): string {
  return tokens.map(t => t.charAt(0).toUpperCase() + t.slice(1).toLowerCase()).join(' ')
}

export function isPFCPrimary(value: string): boolean {
  return (CURATED_PRIMARIES as readonly string[]).includes(value)
}

export function formatPFCPrimary(primary: string): string {
  return titleCase(primary.split('_'))
}

export function formatPFCDetailed(detailed: string): string {
  const matchingPrimary = [...CURATED_PRIMARIES]
    .sort((a, b) => b.length - a.length)
    .find(p => detailed.startsWith(p + '_'))

  if (matchingPrimary) {
    const suffix = detailed.slice(matchingPrimary.length + 1)
    return `${formatPFCPrimary(matchingPrimary)} › ${titleCase(suffix.split('_'))}`
  }

  const tokens = detailed.split('_')
  return `${titleCase([tokens[0]])} › ${titleCase(tokens.slice(1))}`
}

export function formatPFCEnum(enumValue: string): string {
  if (CURATED_PRIMARIES.includes(enumValue)) {
    return formatPFCPrimary(enumValue)
  }
  return formatPFCDetailed(enumValue)
}
