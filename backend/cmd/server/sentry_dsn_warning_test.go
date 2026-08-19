package main

import (
	"os"
	"strings"
	"testing"
)

func TestSentryDSNWarning_TruthTable(t *testing.T) {
	cases := []struct {
		name          string
		environment   string
		sentryDSN     string
		wantWarranted bool
		why           string
	}{
		{
			name: "production, DSN missing", environment: "production", sentryDSN: "",
			wantWarranted: true,
			why:           "this is the gap round 2 defect itself: the Sentry tee is dead and nothing says so",
		},
		{
			name: "staging, DSN missing", environment: "staging", sentryDSN: "",
			wantWarranted: true,
			why:           "staging is just as affected as production — the tee is gated on cfg.SentryDSN alone",
		},
		{
			name: "production, DSN set", environment: "production", sentryDSN: "https://key@sentry.io/1",
			wantWarranted: false,
			why:           "a configured DSN means the Sentry tee is live; nothing to warn about",
		},
		{
			name: "staging, DSN set", environment: "staging", sentryDSN: "https://key@sentry.io/1",
			wantWarranted: false,
			why:           "same as above for staging",
		},
		{
			name: "development, DSN missing", environment: "development", sentryDSN: "",
			wantWarranted: false,
			why:           "a missing DSN in local dev is expected, not an operational gap worth an ERROR log",
		},
		{
			name: "test, DSN missing", environment: "test", sentryDSN: "",
			wantWarranted: false,
			why:           "test runs should not emit this warning either",
		},
		{
			name: "empty environment, DSN missing", environment: "", sentryDSN: "",
			wantWarranted: false,
			why: "an unset ENVIRONMENT defaults to \"development\" upstream in config.Load; this function " +
				"only recognizes the literal strings \"production\"/\"staging\" as warranting the warning",
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			msg, attrs, warranted := sentryDSNWarning(c.environment, c.sentryDSN)

			if warranted != c.wantWarranted {
				t.Fatalf("sentryDSNWarning(environment=%q, sentryDSN=%q) warranted = %v, want %v — %s",
					c.environment, c.sentryDSN, warranted, c.wantWarranted, c.why)
			}

			if !warranted {
				if msg != "" {
					t.Errorf("no warning warranted but msg = %q, want empty", msg)
				}
				if attrs != nil {
					t.Errorf("no warning warranted but attrs = %v, want nil", attrs)
				}
			}
		})
	}
}

func TestSentryDSNWarning_NamesTheConsequence(t *testing.T) {
	msg, attrs, warranted := sentryDSNWarning("production", "")
	if !warranted {
		t.Fatal("precondition: production + empty DSN must warrant a warning")
	}

	if msg != sentryDSNWarningMessage {
		t.Errorf("msg = %q, want the exported constant %q", msg, sentryDSNWarningMessage)
	}
	for _, want := range []string{"SENTRY_DSN", "Sentry", "page"} {
		if !strings.Contains(msg, want) {
			t.Errorf("headline %q must mention %q", msg, want)
		}
	}

	if len(attrs)%2 != 0 {
		t.Fatalf("attrs has odd length %d — slog key/value pairs must come in twos", len(attrs))
	}

	keys := map[string]string{}
	for i := 0; i < len(attrs); i += 2 {
		k, ok := attrs[i].(string)
		if !ok {
			t.Fatalf("attrs[%d] = %v is not a string key", i, attrs[i])
		}
		v, ok := attrs[i+1].(string)
		if !ok {
			t.Fatalf("attrs[%d] = %v is not a string value", i+1, attrs[i+1])
		}
		keys[k] = v
	}

	for _, k := range []string{"environment", "consequence_alerting", "consequence_scope", "remediation"} {
		v, present := keys[k]
		if !present {
			t.Errorf("warning must carry the %q attribute", k)
			continue
		}
		if strings.TrimSpace(v) == "" {
			t.Errorf("attribute %q is present but empty", k)
		}
	}

	if keys["environment"] != "production" {
		t.Errorf("attrs[\"environment\"] = %q, want %q (the environment that was actually checked)",
			keys["environment"], "production")
	}

	if !strings.Contains(keys["consequence_alerting"], "redis_limiter_fallback_activations_total") {
		t.Errorf("consequence_alerting attribute must name the specific metric this warning protects "+
			"(redis_limiter_fallback_activations_total), got %q", keys["consequence_alerting"])
	}
}

func TestSentryDSNWarning_IsWiredIntoMain(t *testing.T) {
	data, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("could not read main.go: %v", err)
	}
	src := string(data)

	if !strings.Contains(src, "sentryDSNWarning(cfg.Environment, cfg.SentryDSN)") {
		t.Error("main.go must call sentryDSNWarning(cfg.Environment, cfg.SentryDSN) — the helper is " +
			"useless if nothing invokes it, and its own unit tests would still pass")
	}
	if !strings.Contains(src, "appLogger.Error(msg, attrs...)") {
		t.Error("main.go must log the helper's returned msg/attrs via appLogger.Error(msg, attrs...) at " +
			"ERROR level (not Warn) — an ERROR log is the correct volume per Issue #1531's gap-round-2 " +
			"analysis: loud enough to notice, not a validate() hard failure that would trade a silent " +
			"alerting gap for a loud availability outage")
	}
}
