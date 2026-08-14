import { afterEach, beforeEach, describe, expect, mock, test } from 'bun:test';
import { devLog } from './devLog';

describe('devLog', () => {
  let logSpy: ReturnType<typeof mock>;
  let originalLog: typeof console.log;

  beforeEach(() => {
    originalLog = console.log;
    logSpy = mock(() => {});
    console.log = logSpy as unknown as typeof console.log;
  });

  afterEach(() => {
    console.log = originalLog;
  });

  test('does not log when import.meta.env.DEV is false', () => {
    // Under bun:test, import.meta.env.DEV is not injected (falsy), which
    // matches the production build's value — this exercises the same
    // branch that keeps Paddle/Plaid payloads out of the production console.
    expect(import.meta.env.DEV).toBeFalsy();

    devLog('Paddle event:', { name: 'checkout.completed', data: { email: 'jane@example.com' } });

    expect(logSpy).not.toHaveBeenCalled();
  });
});
