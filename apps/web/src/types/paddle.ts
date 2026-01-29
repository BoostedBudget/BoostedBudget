// Paddle SDK type definitions for TypeScript
// Based on Paddle.js v2 Overlay Checkout
// https://developer.paddle.com/paddlejs/overview

export interface PaddleCheckoutOptions {
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

export interface Paddle {
  Checkout: PaddleCheckout
  Initialize?: (options?: { token?: string; pwCustomer?: unknown }) => void
}

declare global {
  interface Window {
    Paddle?: Paddle
    __paddleCheckoutCalled?: PaddleCheckoutOptions
  }
}
