import { describe, expect, test } from 'bun:test';
import { redactPii, stripQueryString, tokenize } from './piiScrubbing';

describe('tokenize', () => {
  // Table-driven so a failure names exactly which key shape broke, and the
  // tokenizer is tested directly rather than only observed through
  // redactPii's redact/no-redact outcome, which would hide which stage
  // (splitting vs. denylist lookup) actually failed.
  const cases: Array<[string, string[]]> = [
    ['balance2', ['balance', '2']],
    ['email2', ['email', '2']],
    ['userIDBalance', ['user', 'id', 'balance']],
    ['accountIDName', ['account', 'id', 'name']],
    ['HTTPStatus', ['http', 'status']],
    ['account1Name', ['account', '1', 'name']],
    ['ID', ['id']],
    ['a', ['a']],
    ['', []],
    ['snake_case_key', ['snake', 'case', 'key']],
    ['kebab-case-key', ['kebab', 'case', 'key']],
    ['Balance', ['balance']],
  ];

  for (const [input, expected] of cases) {
    test(`tokenizes ${JSON.stringify(input)} as ${JSON.stringify(expected)}`, () => {
      expect(tokenize(input)).toEqual(expected);
    });
  }
});

describe('stripQueryString', () => {
  test('strips everything from ? onward', () => {
    expect(stripQueryString('https://api.example.com/v1/accounts?account_id=acc_123')).toBe(
      'https://api.example.com/v1/accounts'
    );
  });

  test('returns the url unchanged when there is no query string', () => {
    expect(stripQueryString('https://api.example.com/v1/accounts')).toBe(
      'https://api.example.com/v1/accounts'
    );
  });
});

describe('redactPii', () => {
  test('redacts a denylisted key nested inside a plain array', () => {
    const input = {
      accounts: [
        { id: 'acc_1', nickname: 'My Checking' },
        { id: 'acc_2', nickname: 'My Savings' },
      ],
    };

    const result = redactPii(input) as typeof input;

    expect(result.accounts[0].nickname).toBe('[Redacted]');
    expect(result.accounts[1].nickname).toBe('[Redacted]');
    expect(result.accounts[0].id).toBe('acc_1');
    expect(result.accounts[1].id).toBe('acc_2');
  });

  test('redacts a denylisted key inside an array of arrays', () => {
    const input = {
      rows: [
        [{ merchant_name: 'Starbucks', amount: 4.5 }],
        [{ merchant_name: 'Chipotle', amount: 9.0 }],
      ],
    };

    const result = redactPii(input) as typeof input;

    expect(result.rows[0][0].merchant_name).toBe('[Redacted]');
    expect(result.rows[1][0].merchant_name).toBe('[Redacted]');
    expect(result.rows[0][0].amount).toBe('[Redacted]');
  });

  test('terminates on a genuinely circular object and emits the [Circular] sentinel', () => {
    const input: Record<string, unknown> = { name: 'circular-test' };
    input.self = input;

    let result: unknown;
    expect(() => {
      result = redactPii(input);
    }).not.toThrow();

    const typed = result as Record<string, unknown>;
    expect(typed.name).toBe('circular-test');
    expect(typed.self).toBe('[Circular]');
  });

  test('terminates on a circular reference reached through an array', () => {
    const input: Record<string, unknown> = { name: 'circular-array-test' };
    input.items = [input];

    let result: unknown;
    expect(() => {
      result = redactPii(input);
    }).not.toThrow();

    const typed = result as Record<string, unknown>;
    const items = typed.items as unknown[];
    expect(items[0]).toBe('[Circular]');
  });

  test('redacts camelCase near-misses of denylisted PII tokens', () => {
    const input = {
      currentBalance: 100,
      institutionName: 'Chase',
      availableBalance: 200,
      merchantName: 'Starbucks',
      accountBalance: 500,
      userEmail: 'jane@example.com',
    };

    const result = redactPii(input) as typeof input;

    expect(result.currentBalance).toBe('[Redacted]');
    expect(result.institutionName).toBe('[Redacted]');
    expect(result.availableBalance).toBe('[Redacted]');
    expect(result.merchantName).toBe('[Redacted]');
    expect(result.accountBalance).toBe('[Redacted]');
    expect(result.userEmail).toBe('[Redacted]');
  });

  test('redacts snake_case denylisted keys via token matching', () => {
    const input = { current_balance: 100, institution_name: 'Chase' };

    const result = redactPii(input) as typeof input;

    expect(result.current_balance).toBe('[Redacted]');
    expect(result.institution_name).toBe('[Redacted]');
  });

  test('does not over-scrub benign compounds sharing a token with a denylisted word', () => {
    const input = { accountCount: 3, descriptionLength: 12, requestId: 'req-1', step: 'checkout' };

    const result = redactPii(input) as typeof input;

    expect(result.accountCount).toBe(3);
    expect(result.descriptionLength).toBe(12);
    expect(result.requestId).toBe('req-1');
    expect(result.step).toBe('checkout');
  });

  test('redacts accountName via the account_name two-token window, without over-scrubbing accountCount or typeName', () => {
    // Neither `account` nor `name` is a bare denylist token (they'd redact
    // accountCount/typeName), but accountName is still PII (an account
    // nickname) — closed via the adjacent-token-window check in isPiiKey
    // matching the existing `account_name` compound entry.
    const input = { accountName: 'My Checking', accountCount: 3, typeName: 'checking' };

    const result = redactPii(input) as typeof input;

    expect(result.accountName).toBe('[Redacted]');
    expect(result.accountCount).toBe(3);
    expect(result.typeName).toBe('checking');
  });

  test('redacts realistic fail-open key shapes: digit-suffixed and acronym-adjacent PII fields', () => {
    const input = { balance2: 100, institutionName2: 'Chase', userIDBalance: 200, accountIDName: 'Checking' };

    const result = redactPii(input) as typeof input;

    expect(result.balance2).toBe('[Redacted]');
    expect(result.institutionName2).toBe('[Redacted]');
    expect(result.userIDBalance).toBe('[Redacted]');
    expect(result.accountIDName).toBe('[Redacted]');
  });

  test('scrubs a real-shaped Axios-like error with config/response and a self-reference, without throwing or hanging', () => {
    // Axios attaches `error.request`/`error.config` back onto the response,
    // and some environments attach the originating error back onto its own
    // `response.config` — this loop is exactly what a naive recursive
    // redactor would hang or stack-overflow on.
    const config: Record<string, unknown> = {
      url: 'https://api.boostedbudget.com/v1/accounts?account_id=acc_123',
      headers: { Authorization: 'Bearer super-secret-token' },
    };
    const response: Record<string, unknown> = {
      status: 500,
      data: { email: 'jane.doe@example.com', detail: 'Internal error' },
      config,
    };
    const axiosError: Record<string, unknown> = {
      message: 'Request failed with status code 500',
      config,
      response,
    };
    // Self-reference: response points back at the error that produced it.
    response.axiosError = axiosError;

    let result: unknown;
    expect(() => {
      result = redactPii(axiosError);
    }).not.toThrow();

    const typed = result as Record<string, unknown>;
    expect(typed.config.headers.Authorization).toBe('[Redacted]');
    expect(typed.config.url).toBe('https://api.boostedbudget.com/v1/accounts');
    expect(typed.response.data.email).toBe('[Redacted]');
    expect(typed.response.data.detail).toBe('Internal error');
    expect(typed.response.status).toBe(500);
    // `config` is the *same object* referenced from both the top-level error
    // and `response.config` — a shared reference (DAG), not a cycle. It must
    // still be redacted on the second occurrence rather than replaced with
    // the [Circular] sentinel, which would silently drop real diagnostics.
    expect(typed.response.config.headers.Authorization).toBe('[Redacted]');
    expect(typed.response.config.url).toBe('https://api.boostedbudget.com/v1/accounts');
    // The cycle back to the same axiosError object is cut with the sentinel.
    expect(typed.response.axiosError).toBe('[Circular]');
  });
});
