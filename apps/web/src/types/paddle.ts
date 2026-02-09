// Paddle SDK type definitions for TypeScript
// Based on Paddle.js v2 Overlay Checkout
// https://developer.paddle.com/paddlejs/overview

export interface PaddleCheckoutOptions {
  transactionId?: string
  settings?: {
    checkoutURL?: string
    theme?: 'light' | 'dark'
    locale?: string
    frameTarget?: string
    frameInitialHeight?: number
    frameStyle?: string
    displayMode?: 'inline' | 'overlay'
    successUrl?: string
  }
}

export interface PaddleCheckout {
  open: (options: PaddleCheckoutOptions) => void
}

export interface PaddleEnvironment {
  set: (env: 'sandbox' | 'production') => void
}

export interface PaddleInitializeOptions {
  token: string
  eventCallback?: (event: { name: string; data?: unknown }) => void
}

export interface Paddle {
  Environment: PaddleEnvironment
  Initialize: (options: PaddleInitializeOptions) => void
  Checkout: PaddleCheckout
}

declare global {
  interface Window {
    Paddle?: Paddle
    __paddleCheckoutCalled?: PaddleCheckoutOptions
  }
}
