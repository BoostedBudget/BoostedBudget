import { titleCaseWords } from '@/features/transactions/investmentSubcategory'

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

export function isPFCPrimary(value: string): boolean {
  return (CURATED_PRIMARIES as readonly string[]).includes(value)
}

export function formatPFCPrimary(primary: string): string {
  return titleCaseWords(primary)
}

export function formatPFCDetailed(detailed: string): string {
  const matchingPrimary = [...CURATED_PRIMARIES]
    .sort((a, b) => b.length - a.length)
    .find(p => detailed.startsWith(p + '_'))

  if (matchingPrimary) {
    const suffix = detailed.slice(matchingPrimary.length + 1)
    return `${formatPFCPrimary(matchingPrimary)} › ${titleCaseWords(suffix)}`
  }

  const tokens = detailed.split('_')
  return `${titleCaseWords(tokens[0])} › ${titleCaseWords(tokens.slice(1).join('_'))}`
}

export function formatPFCEnum(enumValue: string): string {
  if (CURATED_PRIMARIES.includes(enumValue)) {
    return formatPFCPrimary(enumValue)
  }
  return formatPFCDetailed(enumValue)
}
