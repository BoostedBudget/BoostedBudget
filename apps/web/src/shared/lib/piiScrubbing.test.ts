import { describe, expect, test } from 'bun:test';
import { isPiiKey, redactPii, redactUrlPath, sanitizeUrl, stripQueryString, tokenize } from './piiScrubbing';
import { posthogConfig } from './posthog';

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

// Issue #1514 (fix round 6): `stripQueryString` only ever strips from `?` onward — a
// capability token embedded as a raw PATH SEGMENT (e.g.
// `/api/v1/user/delete/cancel/<token>`, which the backend's CancelDeletion endpoint
// treats as the entire authentication for the request — see
// backend/internal/huma/operations/deletion.go) survives it completely. This is a
// bearer-capability leak, not PII, but the containment mechanism (redact by URL shape,
// not query string) is the same kind of fix.
describe('redactUrlPath', () => {
  const TOKEN = 'eyJhbGciOiJIUzI1NiJ9.SENTINEL_CAPABILITY_TOKEN.abc123';

  test('redacts the account-deletion cancellation token path segment', () => {
    const url = `https://app.example.com/api/v1/user/delete/cancel/${TOKEN}`;
    const result = redactUrlPath(url);
    expect(result).not.toContain(TOKEN);
    expect(result).toBe('https://app.example.com/api/v1/user/delete/cancel/[redacted]');
  });

  test('redacts the household-invite token path segment, including trailing /accept', () => {
    const url = `https://app.example.com/api/v1/households/invites/${TOKEN}/accept`;
    const result = redactUrlPath(url);
    expect(result).not.toContain(TOKEN);
    expect(result).toBe('https://app.example.com/api/v1/households/invites/[redacted]/accept');
  });

  // Issue #1514 (fix round 7): the ORIGINAL fix only covered the API path shape
  // (`/api/v1/user/delete/cancel/{token}`). The actual exposure is the FRONTEND route the
  // emailed link points at — `/cancel-deletion/{token}` (App.tsx's route, built by
  // backend/internal/services/email_service.go:116's `cancellationURL`) — which the page
  // URL itself carries once a user clicks the email. Neither of the two patterns above
  // matches that shape.
  test('redacts the frontend cancel-deletion page route (the actual emailed link shape)', () => {
    const url = `https://app.example.com/cancel-deletion/${TOKEN}`;
    const result = redactUrlPath(url);
    expect(result).not.toContain(TOKEN);
    expect(result).toBe('https://app.example.com/cancel-deletion/[redacted]');
  });

  // household_service.go:1055 builds the emailed invite link as
  // `frontendURL + "/households/invites/" + rawToken` — no `/api/v1` prefix. The existing
  // pattern above has no `/api/v1` anchor, so it already matches this shape too; this test
  // confirms that rather than assuming it.
  test('redacts the frontend household-invite page route (no /api/v1 prefix)', () => {
    const url = `https://app.example.com/households/invites/${TOKEN}`;
    const result = redactUrlPath(url);
    expect(result).not.toContain(TOKEN);
    expect(result).toBe('https://app.example.com/households/invites/[redacted]');
  });

  // App.tsx also registers a second, differently-spelled route to the same
  // AcceptInvitePage — "an alias matching the original issue text" per
  // AcceptInvitePage.tsx's own comment. Nothing in the backend currently builds a link in
  // this shape, but the route is live, so a token-bearing URL of this shape can still
  // reach the app (an old bookmark, a manually-typed link, etc.) and must be redacted the
  // same way.
  test('redacts the legacy /household/invite/{token} alias route', () => {
    const url = `https://app.example.com/household/invite/${TOKEN}`;
    const result = redactUrlPath(url);
    expect(result).not.toContain(TOKEN);
    expect(result).toBe('https://app.example.com/household/invite/[redacted]');
  });

  test('leaves a URL with no known capability route unchanged', () => {
    const url = 'https://app.example.com/api/v1/accounts/acc_123';
    expect(redactUrlPath(url)).toBe(url);
  });

  test('leaves a resource id path (not a capability route) unchanged', () => {
    // /accounts/{id} is an opaque resource id scoped by an authenticated session — not a
    // bearer capability on its own — so it must NOT be redacted by this function.
    const url = 'https://app.example.com/api/v1/accounts/acc_123/transactions';
    expect(redactUrlPath(url)).toBe(url);
  });
});

describe('sanitizeUrl', () => {
  test('strips the query string AND redacts a capability path segment in one call', () => {
    const TOKEN = 'SENTINEL_TOKEN_ABC';
    const url = `https://app.example.com/api/v1/user/delete/cancel/${TOKEN}?utm_source=email`;
    const result = sanitizeUrl(url);
    expect(result).not.toContain(TOKEN);
    expect(result).not.toContain('utm_source');
    expect(result).toBe('https://app.example.com/api/v1/user/delete/cancel/[redacted]');
  });
});

describe('isPiiKey: merchant_id/institution_id allowlist (issue #1514, defect 2)', () => {
  // Defect 1's fix exempted every key whose *last token* is `id` from the
  // bare-token window scan, redacting only on an explicit whole-key denylist
  // match. That is not narrow: it allowlisted the entire `*_id` shape, not
  // two specific keys, so any denylisted bare token followed by an `id`
  // suffix survived too (`email_id`, `nickname_id`, `balance_id`, ...). This
  // block is the RED for that regression — every one of these keys survives
  // scrubbing under the pre-fix suffix rule; run this block against that code
  // and quote the real failure before trusting the fix below.
  const adversarialKeysThatMustBeRedacted: string[] = [
    'email_id',
    'emailId',
    'user_email_id',
    'nickname_id',
    'balance_id',
    'current_balance_id',
    'maskId',
    'merchant_name_id',
    'accountNameId',
  ];

  for (const key of adversarialKeysThatMustBeRedacted) {
    test(`${key} is redacted (defect 2 regression: ending in id is not a free pass)`, () => {
      expect(isPiiKey(key)).toBe(true);
    });
  }

  // The fix: an explicit, named allowlist of exactly two keys (both
  // spellings, matched on the normalized/tokenized form so `merchant_id` and
  // `merchantId` hit the same entry) — not a shape-based carve-out. This is
  // the entire allowlist; it covers nothing else.
  const allowlistedIdKeysThatMustSurvive: string[] = [
    'merchant_id',
    'merchantId',
    'institution_id',
    'institutionId',
  ];

  for (const key of allowlistedIdKeysThatMustSurvive) {
    test(`${key} survives (explicit allowlist entry)`, () => {
      expect(isPiiKey(key)).toBe(false);
    });
  }

  // Explicit denylist entries and name variants (not *_id) must be
  // unaffected by the allowlist, which only ever suppresses a match for its
  // two named keys — it never adds one and never widens beyond them.
  const mustStayRedacted: string[] = [
    'account_id',
    'link_session_id',
    'merchant',
    'merchantName',
    'merchant_name',
    'institution',
    'institutionName',
    'institution_name',
    'amount',
    'currentBalance',
    'userEmail',
    'query_string',
    'cookies',
  ];

  for (const key of mustStayRedacted) {
    test(`${key} stays redacted`, () => {
      expect(isPiiKey(key)).toBe(true);
    });
  }

  // Benign trackEvent property names actually used at real call sites across
  // the codebase (issue #1514 brief). None of these are in
  // PII_PROPERTY_DENYLIST or the merchant_id/institution_id allowlist — they
  // survive because the ordinary token scan never matches them, not because
  // of any carve-out. Do NOT add these to the allowlist: the allowlist exists
  // only for keys that would otherwise be redacted, and padding it here would
  // hide whether the underlying scan still works.
  const benignPropertyNamesThatMustSurviveWithoutAllowlisting: string[] = [
    'transaction_id',
    'goal_id',
    'record_id',
    'contribution_id',
    'job_id',
    'widget_id',
    'columnId',
    'step_id',
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

  for (const key of benignPropertyNamesThatMustSurviveWithoutAllowlisting) {
    test(`${key} survives (benign, not allowlisted)`, () => {
      expect(isPiiKey(key)).toBe(false);
    });
  }
});

describe('isPiiKey: digit-interrupted ID_ALLOWLIST entries (issue #1514, defect 3)', () => {
  // The pre-fix code filtered pure-digit tokens out of `tokens` BEFORE
  // comparing against ID_ALLOWLIST, so a digit sitting inside `merchant_id`
  // was invisible to the allowlist check: `merchant_1_id` tokenizes to
  // ['merchant', '1', 'id'], the '1' is dropped, and the remaining
  // ['merchant', 'id'] joins to the literal string 'merchant_id' — which IS
  // in ID_ALLOWLIST, even though the allowlist is documented as exempting
  // exactly the two keys `merchant_id`/`institution_id`, not an unbounded
  // family of digit-infix variants. The fix compares the allowlist against
  // the UNFILTERED token join and only drops digit tokens afterward, for the
  // window scan. This is the RED: every one of these keys survives scrubbing
  // under the pre-fix code (verified by running this block before the fix).
  const digitDefeatsAllowlistMustBeRedacted: string[] = [
    'merchant_1_id',
    'merchant_2_id',
    'merchant_99_id',
    'merchant_0_id',
    'institution_1_id',
    'merchant_id2',
    'MERCHANT_7_ID',
  ];

  // Both spellings and separator/case variants of the two genuinely
  // allowlisted keys must still be exempt — the fix must not narrow the
  // allowlist while closing the digit-infix gap.
  const stillExempt: string[] = [
    'merchant_id',
    'merchantId',
    'institution_id',
    'institutionId',
    'merchant-id',
    'merchant__id',
    'MERCHANT_ID',
    '_merchant_id',
  ];

  // Full re-run of the previously verified redact/survive matrices, plus the
  // digit-filter regression guard (`account1Name`) that this fix must not
  // break: the digit filter still has to run for the ordinary window scan,
  // just after (not instead of) the allowlist comparison.
  const stillRedacted: string[] = [
    'email_id',
    'emailId',
    'user_email_id',
    'nickname_id',
    'balance_id',
    'current_balance_id',
    'maskId',
    'merchant_name_id',
    'accountNameId',
    'account_id',
    'accountId',
    'link_session_id',
    'merchant',
    'merchantName',
    'merchant_name',
    'institution',
    'institutionName',
    'institution_name',
    'amount',
    'currentBalance',
    'userEmail',
    'query_string',
    'cookies',
    'account1Name',
  ];

  const stillSurvivesBenign: string[] = [
    'transaction_id',
    'goal_id',
    'record_id',
    'contribution_id',
    'job_id',
    'widget_id',
    'columnId',
    'step_id',
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

  // Exercise every key above through all three real consumers: isPiiKey
  // directly, Sentry's redactPii, and PostHog's before_send. isPiiKey is
  // shared by both analytics paths — a fix verified against isPiiKey alone
  // could still leave one of the two consumers broken (see the file-header
  // comment on the pre-unification PostHog exact-match bug).
  function expectMatrix(keys: string[], expectRedacted: boolean) {
    for (const key of keys) {
      const label = expectRedacted ? 'is redacted' : 'survives';

      test(`${key} ${label} (isPiiKey)`, () => {
        expect(isPiiKey(key)).toBe(expectRedacted);
      });

      test(`${key} ${label} (redactPii)`, () => {
        const result = redactPii({ [key]: 'sensitive-value' }) as Record<string, unknown>;
        if (expectRedacted) {
          expect(result[key]).toBe('[Redacted]');
        } else {
          expect(result[key]).toBe('sensitive-value');
        }
      });

      test(`${key} ${label} (posthog before_send)`, () => {
        const cfg = posthogConfig(() => true);
        const event = { event: 'test_event', properties: { [key]: 'sensitive-value' } };
        const out = cfg.before_send?.(event as Parameters<NonNullable<typeof cfg.before_send>>[0]);
        if (expectRedacted) {
          expect(out?.properties?.[key]).toBeUndefined();
        } else {
          expect(out?.properties?.[key]).toBe('sensitive-value');
        }
      });
    }
  }

  expectMatrix(digitDefeatsAllowlistMustBeRedacted, true);
  expectMatrix(stillExempt, false);
  expectMatrix(stillRedacted, true);
  expectMatrix(stillSurvivesBenign, false);
});

describe('isPiiKey: punctuation-delimited keys bypass the matcher (issue #1514)', () => {
  // tokenize() only ever split on `[_-]`. A denylisted word joined to the
  // rest of the key by ANY other separator — `.`, `/`, whitespace — produced
  // a single un-split token that never equals a denylist entry and never
  // enters the window scan as its own word, so isPiiKey silently returned
  // false. This is the RED: every key below survives (is NOT redacted) under
  // the pre-fix tokenizer, through both real consumers (redactPii and
  // PostHog's before_send), not just isPiiKey in isolation.
  const punctuationKeysThatMustBeRedacted: string[] = [
    'merchant.name',
    'user.email',
    'merchant/name',
    'current balance',
    'institution.name',
    'account.balance',
  ];

  for (const key of punctuationKeysThatMustBeRedacted) {
    test(`${key} is redacted (isPiiKey)`, () => {
      expect(isPiiKey(key)).toBe(true);
    });

    test(`${key} is redacted (redactPii)`, () => {
      const result = redactPii({ [key]: 'sensitive-value' }) as Record<string, unknown>;
      expect(result[key]).toBe('[Redacted]');
    });

    test(`${key} is redacted (posthog before_send)`, () => {
      const cfg = posthogConfig(() => true);
      const event = { event: 'test_event', properties: { [key]: 'sensitive-value' } };
      const out = cfg.before_send?.(event as Parameters<NonNullable<typeof cfg.before_send>>[0]);
      expect(out?.properties?.[key]).toBeUndefined();
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

  // Issue #1514 (fix round 7): "Redaction is by property name only — string values are
  // never scanned for PII-shaped content" (see this function's own doc comment above) was
  // true and reasonable for PII-shaped content, but it also meant a capability URL sitting
  // in ANY string value — `breadcrumb.message`, an exception `value`, a stack-frame
  // string — survived untouched unless it happened to be under a property literally named
  // `url`. This widens `redactPii` to run every string value it visits through
  // `redactUrlPath` — deliberately still a targeted rewrite (redactUrlPath only ever
  // matches the known capability-route shapes), not a general content scan, so it cannot
  // introduce new false positives on ordinary error text.
  describe('capability-route redaction applies to string values generally, not just `url`-keyed ones', () => {
    const TOKEN = 'eyJhbGciOiJIUzI1NiJ9.SENTINEL_GENERAL_STRING_TOKEN.xyz';

    test('redacts a capability URL embedded in an arbitrary string value (not keyed "url")', () => {
      const input = {
        message: `Request failed: https://app.example.com/cancel-deletion/${TOKEN}`,
      };
      const result = redactPii(input) as Record<string, unknown>;
      expect(result.message).not.toContain(TOKEN);
      expect(result.message).toBe('Request failed: https://app.example.com/cancel-deletion/[redacted]');
    });

    test('redacts a capability URL inside a nested stack-frame-shaped string', () => {
      const input = {
        exception: {
          values: [
            {
              value: `Network Error: https://app.example.com/households/invites/${TOKEN}/accept`,
              stacktrace: {
                frames: [
                  { filename: 'app.js', context_line: `fetch("/households/invites/${TOKEN}")` },
                ],
              },
            },
          ],
        },
      };
      const result = redactPii(input) as {
        exception: { values: Array<{ value: string; stacktrace: { frames: Array<{ filename: string; context_line: string }> } }> };
      };
      const frame = result.exception.values[0].stacktrace.frames[0];
      expect(result.exception.values[0].value).not.toContain(TOKEN);
      expect(frame.context_line).not.toContain(TOKEN);
    });

    test('does not over-redact ordinary strings that merely contain a slash', () => {
      const input = { message: 'Loaded 3/5 accounts successfully' };
      const result = redactPii(input) as Record<string, unknown>;
      expect(result.message).toBe('Loaded 3/5 accounts successfully');
    });
  });
});
