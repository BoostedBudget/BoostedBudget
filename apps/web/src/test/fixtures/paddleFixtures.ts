/**
 * Paddle API test fixtures
 * Mock responses for Paddle payment integration tests
 */

export const mockPaddleCheckoutResponse = {
  checkout_url: 'https://checkout.paddle.com/txn_01h1234567890abcdef',
  transaction_id: 'txn_01h1234567890abcdef',
}

export const mockPaddleErrorResponses = {
  unauthorized: {
    status: 401,
    message: 'Unauthorized',
    detail: 'Please sign in to subscribe',
  },
  forbidden: {
    status: 403,
    message: 'Forbidden',
    detail: 'You do not have permission to subscribe',
  },
  badRequest: {
    status: 400,
    message: 'Bad Request',
    detail: 'Invalid tier',
  },
  invalidTier: {
    status: 400,
    message: 'Bad Request',
    detail: 'Invalid subscription tier selected',
  },
  serverError: {
    status: 500,
    message: 'Internal Server Error',
    detail: 'Payment system error. Please try again later.',
  },
  serviceUnavailable: {
    status: 503,
    message: 'Service Unavailable',
    detail: 'Payment service temporarily unavailable',
  },
  networkError: {
    message: 'Network Error',
    detail: 'Network error. Please check your connection.',
  },
}

export const mockPaddleCustomer = {
  id: 'ctm_01h1234567890abcdef',
  email: 'test@example.com',
  name: 'Test User',
  custom_data: {
    user_id: 'user_123',
  },
}

export const mockPaddleTransaction = {
  id: 'txn_01h1234567890abcdef',
  status: 'completed',
  customer_id: 'ctm_01h1234567890abcdef',
  checkout: {
    url: 'https://checkout.paddle.com/txn_01h1234567890abcdef',
  },
}

export const mockPaddleSubscription = {
  id: 'sub_01h1234567890abcdef',
  status: 'active',
  customer_id: 'ctm_01h1234567890abcdef',
  items: [
    {
      price: {
        id: 'pri_premium_month',
        description: 'Premium Monthly',
        unit_price: {
          amount: '999',
          currency_code: 'USD',
        },
      },
      quantity: 1,
    },
  ],
}

// Mock responses for different subscription tiers
export const mockCheckoutResponsesByTier = {
  premium: {
    checkout_url: 'https://checkout.paddle.com/txn_premium_123',
    transaction_id: 'txn_premium_123',
  },
  household_2: {
    checkout_url: 'https://checkout.paddle.com/txn_household2_123',
    transaction_id: 'txn_household2_123',
  },
  household_3: {
    checkout_url: 'https://checkout.paddle.com/txn_household3_123',
    transaction_id: 'txn_household3_123',
  },
  household_4: {
    checkout_url: 'https://checkout.paddle.com/txn_household4_123',
    transaction_id: 'txn_household4_123',
  },
  household_5: {
    checkout_url: 'https://checkout.paddle.com/txn_household5_123',
    transaction_id: 'txn_household5_123',
  },
}

// Mock responses for different billing intervals
export const mockCheckoutResponsesByInterval = {
  month: {
    checkout_url: 'https://checkout.paddle.com/txn_monthly_123',
    transaction_id: 'txn_monthly_123',
  },
  year: {
    checkout_url: 'https://checkout.paddle.com/txn_yearly_123',
    transaction_id: 'txn_yearly_123',
  },
}
