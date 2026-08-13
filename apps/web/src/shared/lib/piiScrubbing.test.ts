import { describe, expect, test } from 'bun:test';
import { isPiiKey, redactPii, stripQueryString, tokenize } from './piiScrubbing';

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
    // Digit -> lowercase-letter boundary (pass 4). Pass 2 already handles
    // digit -> UPPERCASE (see `account1Name` above), so this is the only
    // case in the table that actually exercises pass 4: deleting it collapses
    // to ['account', '2name'] instead of splitting off the trailing word.
    ['account2name', ['account', '2', 'name']],
    // Non-ASCII letter immediately before a camelCase boundary. Regression
    // for a fail-open bug where ASCII-only character classes let `é` block
    // the lower/digit->upper pass from firing, gluing the whole key into one
    // token and defeating denylist matching entirely.
    ['caféBalance', ['café', 'balance']],
    ['naïveAccountName', ['naïve', 'account', 'name']],
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

describe('isPiiKey: *_id allowlist (issue #1514, defect 1)', () => {
  // Prior to this fix, `merchant_id` tokenized to ['merchant', 'id'], and the
  // bare-token window scan matched the `merchant` entry regardless of the
  // trailing `id` — so `isPiiKey('merchant_id')` returned `true` and the
  // documented trackEvent pattern (`{ merchant_id: uuid }`, per
  // docs/security/posthog-pii-masking.md's "what NOT to do" section) was
  // silently deleted from PostHog events by `before_send`. Same bug for
  // `institution_id`. This matrix pins the full, deliberately narrow fix:
  // a key ending in `id` survives unless the whole key is itself an
  // explicit denylist entry (only `account_id` and `link_session_id` are).
  const idKeysThatMustSurvive: string[] = [
    'merchant_id',
    'merchantId',
    'institution_id',
    'institutionId',
    // Benign *_id/_type properties actually used at real trackEvent call
    // sites in this codebase (issue #1514 brief) — none of these were ever
    // broken, but they must not regress alongside the merchant_id/
    // institution_id fix, since both paths run through the same *_id
    // branch in isPiiKey.
    'transaction_id',
    'goal_id',
    'record_id',
    'contribution_id',
    'job_id',
    'widget_id',
    'columnId',
    'step_id',
  ];

  for (const key of idKeysThatMustSurvive) {
    test(`${key} survives (not on the denylist itself)`, () => {
      expect(isPiiKey(key)).toBe(false);
    });
  }

  const idKeysThatMustStayRedacted: string[] = ['account_id', 'accountId', 'link_session_id', 'linkSessionId'];

  for (const key of idKeysThatMustStayRedacted) {
    test(`${key} stays redacted (explicit denylist entry)`, () => {
      expect(isPiiKey(key)).toBe(true);
    });
  }

  // Name variants (not *_id) must be unaffected by the *_id carve-out —
  // they still go through the ordinary bare-token window scan.
  const nameKeysThatMustStayRedacted: string[] = [
    'merchant',
    'merchantName',
    'merchant_name',
    'institution',
    'institutionName',
    'institution_name',
  ];

  for (const key of nameKeysThatMustStayRedacted) {
    test(`${key} stays redacted (name variant, not an id key)`, () => {
      expect(isPiiKey(key)).toBe(true);
    });
  }

  test('amount (a non-id benign-adjacent PII field) still stays redacted', () => {
    expect(isPiiKey('amount')).toBe(true);
  });

  // The other ~16 benign trackEvent property names from the issue #1514
  // brief that aren't *_id-shaped — confirming the unification didn't
  // over-scrub these either.
  const otherBenignPropertyNames: string[] = [
    'widget_type',
    'query_length',
    'excluded_count',
    'tab',
    'theme',
    'page',
    'count',
    'sort',
    'event_type',
    'new_basis',
    'parent_kind',
    'preset',
    'scope',
    'resolved',
    'enabled',
    'deleted',
  ];

  for (const key of otherBenignPropertyNames) {
    test(`${key} survives (benign, non-id trackEvent property)`, () => {
      expect(isPiiKey(key)).toBe(false);
    });
  }
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
    const input = {
      accountCount: 3,
      descriptionLength: 12,
      requestId: 'req-1',
      step: 'checkout',
      typeName: 'checking',
      fieldName: 'note',
    };

    const result = redactPii(input) as typeof input;

    expect(result.accountCount).toBe(3);
    expect(result.descriptionLength).toBe(12);
    expect(result.requestId).toBe('req-1');
    expect(result.step).toBe('checkout');
    expect(result.typeName).toBe('checking');
    expect(result.fieldName).toBe('note');
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

  test('redacts account_name-style compounds even when a digit token sits between the words', () => {
    // `account`/`name` are individually excluded as bare denylist tokens (see
    // the denylist comment), so `account_name` only matches via the
    // adjacent-token window in isPiiKey. A digit token between the words
    // (`account1Name`, `account2name`) used to break that contiguity and
    // silently defeat the match, even though the digit-free `accountName`
    // matched. isPiiKey now drops pure-digit tokens before building windows.
    const input = { account1Name: 'My Checking', account2name: 'My Savings', accountName: 'My Loan' };

    const result = redactPii(input) as typeof input;

    expect(result.account1Name).toBe('[Redacted]');
    expect(result.account2name).toBe('[Redacted]');
    expect(result.accountName).toBe('[Redacted]');
  });

  test('redacts a PII key with a non-ASCII letter immediately before a camelCase boundary', () => {
    // Regression: ASCII-only character classes in tokenize() let `é` sitting
    // directly before the `B` boundary block the split entirely, gluing the
    // key into one token (`cafébalance`) that never matches the `balance`
    // denylist entry — a fail-open of the same class the tokenizer fix
    // originally closed.
    const input = { caféBalance: 100 };

    const result = redactPii(input) as typeof input;

    expect(result.caféBalance).toBe('[Redacted]');
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
