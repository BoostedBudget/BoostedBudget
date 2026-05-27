/**
 * Plaid Personal Finance Category (PFC) curated options
 * Issue #1200: Multi-Level Category Hierarchy
 *
 * Provides a hook returning the two-level PFC hierarchy:
 * primary categories → detailed sub-categories
 */

export interface PfcDetailedOption {
  value: string
  label: string
}

export interface PfcPrimaryOption {
  primary: string
  primaryLabel: string
  detailed: PfcDetailedOption[]
}

/**
 * Plaid PFC two-level hierarchy (curated subset used in BoostedBudget).
 * Source: https://plaid.com/docs/api/products/transactions/#transactions-get-response-transactions-personal-finance-category
 */
const PFC_HIERARCHY: PfcPrimaryOption[] = [
  {
    primary: 'FOOD_AND_DRINK',
    primaryLabel: 'Food & Drink',
    detailed: [
      { value: 'FOOD_AND_DRINK_BEER_WINE_AND_LIQUOR', label: 'Beer, Wine & Liquor' },
      { value: 'FOOD_AND_DRINK_COFFEE', label: 'Coffee' },
      { value: 'FOOD_AND_DRINK_FAST_FOOD', label: 'Fast Food' },
      { value: 'FOOD_AND_DRINK_GROCERIES', label: 'Groceries' },
      { value: 'FOOD_AND_DRINK_RESTAURANT', label: 'Restaurant' },
      { value: 'FOOD_AND_DRINK_VENDING_MACHINES', label: 'Vending Machines' },
      { value: 'FOOD_AND_DRINK_OTHER_FOOD_AND_DRINK', label: 'Other Food & Drink' },
    ],
  },
  {
    primary: 'GENERAL_MERCHANDISE',
    primaryLabel: 'General Merchandise',
    detailed: [
      { value: 'GENERAL_MERCHANDISE_BOOKSTORES_AND_NEWSSTANDS', label: 'Bookstores & Newsstands' },
      { value: 'GENERAL_MERCHANDISE_CLOTHING_AND_ACCESSORIES', label: 'Clothing & Accessories' },
      { value: 'GENERAL_MERCHANDISE_CONVENIENCE_STORES', label: 'Convenience Stores' },
      { value: 'GENERAL_MERCHANDISE_DEPARTMENT_STORES', label: 'Department Stores' },
      { value: 'GENERAL_MERCHANDISE_DISCOUNT_STORES', label: 'Discount Stores' },
      { value: 'GENERAL_MERCHANDISE_ELECTRONICS', label: 'Electronics' },
      { value: 'GENERAL_MERCHANDISE_GIFTS_AND_NOVELTIES', label: 'Gifts & Novelties' },
      { value: 'GENERAL_MERCHANDISE_OFFICE_SUPPLIES', label: 'Office Supplies' },
      { value: 'GENERAL_MERCHANDISE_ONLINE_MARKETPLACES', label: 'Online Marketplaces' },
      { value: 'GENERAL_MERCHANDISE_PET_SUPPLIES', label: 'Pet Supplies' },
      { value: 'GENERAL_MERCHANDISE_SPORTING_GOODS', label: 'Sporting Goods' },
      { value: 'GENERAL_MERCHANDISE_SUPERSTORES', label: 'Superstores' },
      { value: 'GENERAL_MERCHANDISE_TOBACCO_AND_VAPE', label: 'Tobacco & Vape' },
      { value: 'GENERAL_MERCHANDISE_OTHER_GENERAL_MERCHANDISE', label: 'Other General Merchandise' },
    ],
  },
  {
    primary: 'HOME_IMPROVEMENT',
    primaryLabel: 'Home Improvement',
    detailed: [
      { value: 'HOME_IMPROVEMENT_FURNITURE', label: 'Furniture' },
      { value: 'HOME_IMPROVEMENT_HARDWARE', label: 'Hardware' },
      { value: 'HOME_IMPROVEMENT_REPAIR_AND_MAINTENANCE', label: 'Repair & Maintenance' },
      { value: 'HOME_IMPROVEMENT_SECURITY', label: 'Security' },
      { value: 'HOME_IMPROVEMENT_OTHER_HOME_IMPROVEMENT', label: 'Other Home Improvement' },
    ],
  },
  {
    primary: 'ENTERTAINMENT',
    primaryLabel: 'Entertainment',
    detailed: [
      { value: 'ENTERTAINMENT_CASINOS_AND_GAMBLING', label: 'Casinos & Gambling' },
      { value: 'ENTERTAINMENT_MUSIC_AND_AUDIO', label: 'Music & Audio' },
      { value: 'ENTERTAINMENT_SPORTING_EVENTS_AMUSEMENT_PARKS_AND_MUSEUMS', label: 'Events, Parks & Museums' },
      { value: 'ENTERTAINMENT_TV_AND_MOVIES', label: 'TV & Movies' },
      { value: 'ENTERTAINMENT_VIDEO_GAMES', label: 'Video Games' },
      { value: 'ENTERTAINMENT_OTHER_ENTERTAINMENT', label: 'Other Entertainment' },
    ],
  },
  {
    primary: 'PERSONAL_CARE',
    primaryLabel: 'Personal Care',
    detailed: [
      { value: 'PERSONAL_CARE_GYMS_AND_FITNESS_CENTERS', label: 'Gyms & Fitness' },
      { value: 'PERSONAL_CARE_HAIR_AND_BEAUTY', label: 'Hair & Beauty' },
      { value: 'PERSONAL_CARE_LAUNDRY_AND_DRY_CLEANING', label: 'Laundry & Dry Cleaning' },
      { value: 'PERSONAL_CARE_OTHER_PERSONAL_CARE', label: 'Other Personal Care' },
    ],
  },
  {
    primary: 'MEDICAL',
    primaryLabel: 'Medical',
    detailed: [
      { value: 'MEDICAL_DENTAL_CARE', label: 'Dental Care' },
      { value: 'MEDICAL_EYE_CARE', label: 'Eye Care' },
      { value: 'MEDICAL_PHARMACIES_AND_SUPPLEMENTS', label: 'Pharmacies & Supplements' },
      { value: 'MEDICAL_PRIMARY_CARE', label: 'Primary Care' },
      { value: 'MEDICAL_VETERINARY_SERVICES', label: 'Veterinary Services' },
      { value: 'MEDICAL_OTHER_MEDICAL', label: 'Other Medical' },
    ],
  },
  {
    primary: 'TRANSPORTATION',
    primaryLabel: 'Transportation',
    detailed: [
      { value: 'TRANSPORTATION_GAS', label: 'Gas' },
      { value: 'TRANSPORTATION_PARKING', label: 'Parking' },
      { value: 'TRANSPORTATION_PUBLIC_TRANSIT', label: 'Public Transit' },
      { value: 'TRANSPORTATION_TAXIS_AND_RIDE_SHARES', label: 'Taxis & Rideshares' },
      { value: 'TRANSPORTATION_TOLLS', label: 'Tolls' },
      { value: 'TRANSPORTATION_VEHICLES', label: 'Vehicles' },
      { value: 'TRANSPORTATION_OTHER_TRANSPORTATION', label: 'Other Transportation' },
    ],
  },
  {
    primary: 'TRAVEL',
    primaryLabel: 'Travel',
    detailed: [
      { value: 'TRAVEL_FLIGHTS', label: 'Flights' },
      { value: 'TRAVEL_HOTELS_AND_MOTELS', label: 'Hotels & Motels' },
      { value: 'TRAVEL_RENTAL_CARS', label: 'Rental Cars' },
      { value: 'TRAVEL_OTHER_TRAVEL', label: 'Other Travel' },
    ],
  },
  {
    primary: 'RENT_AND_UTILITIES',
    primaryLabel: 'Rent & Utilities',
    detailed: [
      { value: 'RENT_AND_UTILITIES_GAS_AND_ELECTRICITY', label: 'Gas & Electricity' },
      { value: 'RENT_AND_UTILITIES_INTERNET_AND_CABLE', label: 'Internet & Cable' },
      { value: 'RENT_AND_UTILITIES_RENT', label: 'Rent' },
      { value: 'RENT_AND_UTILITIES_SEWAGE_AND_WASTE_MANAGEMENT', label: 'Sewage & Waste' },
      { value: 'RENT_AND_UTILITIES_TELEPHONE', label: 'Telephone' },
      { value: 'RENT_AND_UTILITIES_WATER', label: 'Water' },
      { value: 'RENT_AND_UTILITIES_OTHER_UTILITIES', label: 'Other Utilities' },
    ],
  },
  {
    primary: 'LOAN_PAYMENTS',
    primaryLabel: 'Loan Payments',
    detailed: [
      { value: 'LOAN_PAYMENTS_CAR_PAYMENT', label: 'Car Payment' },
      { value: 'LOAN_PAYMENTS_CREDIT_CARD_PAYMENT', label: 'Credit Card Payment' },
      { value: 'LOAN_PAYMENTS_MORTGAGE_PAYMENT', label: 'Mortgage Payment' },
      { value: 'LOAN_PAYMENTS_PERSONAL_LOAN_PAYMENT', label: 'Personal Loan' },
      { value: 'LOAN_PAYMENTS_STUDENT_LOAN_PAYMENT', label: 'Student Loan' },
      { value: 'LOAN_PAYMENTS_OTHER_PAYMENT', label: 'Other Payment' },
    ],
  },
  {
    primary: 'INCOME',
    primaryLabel: 'Income',
    detailed: [
      { value: 'INCOME_DIVIDENDS', label: 'Dividends' },
      { value: 'INCOME_INTEREST_EARNED', label: 'Interest Earned' },
      { value: 'INCOME_RETIREMENT_PENSION', label: 'Retirement / Pension' },
      { value: 'INCOME_TAX_REFUND', label: 'Tax Refund' },
      { value: 'INCOME_UNEMPLOYMENT', label: 'Unemployment' },
      { value: 'INCOME_WAGES', label: 'Wages' },
      { value: 'INCOME_OTHER_INCOME', label: 'Other Income' },
    ],
  },
  {
    primary: 'TRANSFER_IN',
    primaryLabel: 'Transfer In',
    detailed: [
      { value: 'TRANSFER_IN_CASH_ADVANCES_AND_LOANS', label: 'Cash Advances & Loans' },
      { value: 'TRANSFER_IN_DEPOSIT', label: 'Deposit' },
      { value: 'TRANSFER_IN_INVESTMENT_AND_RETIREMENT_FUNDS', label: 'Investment & Retirement' },
      { value: 'TRANSFER_IN_SAVINGS', label: 'Savings' },
      { value: 'TRANSFER_IN_ACCOUNT_TRANSFER', label: 'Account Transfer' },
      { value: 'TRANSFER_IN_OTHER_TRANSFER_IN', label: 'Other Transfer In' },
    ],
  },
  {
    primary: 'TRANSFER_OUT',
    primaryLabel: 'Transfer Out',
    detailed: [
      { value: 'TRANSFER_OUT_INVESTMENT_AND_RETIREMENT_FUNDS', label: 'Investment & Retirement' },
      { value: 'TRANSFER_OUT_SAVINGS', label: 'Savings' },
      { value: 'TRANSFER_OUT_WITHDRAWAL', label: 'Withdrawal' },
      { value: 'TRANSFER_OUT_ACCOUNT_TRANSFER', label: 'Account Transfer' },
      { value: 'TRANSFER_OUT_OTHER_TRANSFER_OUT', label: 'Other Transfer Out' },
    ],
  },
  {
    primary: 'GOVERNMENT_AND_NON_PROFIT',
    primaryLabel: 'Government & Non-Profit',
    detailed: [
      { value: 'GOVERNMENT_AND_NON_PROFIT_DONATIONS', label: 'Donations' },
      { value: 'GOVERNMENT_AND_NON_PROFIT_GOVERNMENT_DEPARTMENTS_AND_AGENCIES', label: 'Government Agencies' },
      { value: 'GOVERNMENT_AND_NON_PROFIT_TAX_PAYMENT', label: 'Tax Payment' },
      { value: 'GOVERNMENT_AND_NON_PROFIT_OTHER_GOVERNMENT_AND_NON_PROFIT', label: 'Other Govt & Non-Profit' },
    ],
  },
  {
    primary: 'BANK_FEES',
    primaryLabel: 'Bank Fees',
    detailed: [
      { value: 'BANK_FEES_ATM_FEES', label: 'ATM Fees' },
      { value: 'BANK_FEES_FOREIGN_TRANSACTION_FEES', label: 'Foreign Transaction Fees' },
      { value: 'BANK_FEES_INSUFFICIENT_FUNDS', label: 'Insufficient Funds' },
      { value: 'BANK_FEES_INTEREST_CHARGE', label: 'Interest Charge' },
      { value: 'BANK_FEES_OVERDRAFT_FEES', label: 'Overdraft Fees' },
      { value: 'BANK_FEES_OTHER_BANK_FEES', label: 'Other Bank Fees' },
    ],
  },
]

/**
 * Returns the curated PFC two-level hierarchy.
 * The returned object mirrors a query-like shape ({ data }) for easy
 * future migration to a server-driven source.
 */
export function useCuratedPfcDetailed(): { data: PfcPrimaryOption[] } {
  return { data: PFC_HIERARCHY }
}
