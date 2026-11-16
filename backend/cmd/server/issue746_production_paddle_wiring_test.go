package main

import (
	"os"
	"strings"
	"testing"
)

func TestIssue746ProductionPaddleEnvironmentWiringNoDrift(t *testing.T) {
	tests := []struct {
		name         string
		path         string
		sectionStart string
		sectionEnd   string
		required     string
	}{
		{
			name:         "Fly backend selects the production Paddle catalog",
			path:         "../../fly.toml",
			sectionStart: "[env]",
			sectionEnd:   "[experimental]",
			required:     `PADDLE_ENVIRONMENT = "production"`,
		},
		{
			name:         "production frontend build uses the RevenueCat Paddle app key",
			path:         "../../../.github/workflows/deploy-production.yml",
			sectionStart: "- name: Build frontend",
			sectionEnd:   "bun run build",
			required:     "VITE_REVENUECAT_PUBLIC_API_KEY: ${{ secrets.VITE_REVENUECAT_PUBLIC_API_KEY }}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contents, err := os.ReadFile(tt.path)
			if err != nil {
				t.Fatalf("read production deployment config %s: %v", tt.path, err)
			}

			section := textBetween(t, string(contents), tt.sectionStart, tt.sectionEnd)
			if !containsConfigLine(section, tt.required) {
				t.Fatalf("production deployment config %s must contain %q", tt.path, tt.required)
			}
		})
	}
}

func TestIssue746PaddleWebhookGuideCoversHandledEvents(t *testing.T) {
	guideContents, err := os.ReadFile("../../../PADDLE_SETUP_GUIDE.md")
	if err != nil {
		if os.IsNotExist(err) {
			t.Skip("PADDLE_SETUP_GUIDE.md is no longer present in the repository")
		}
		t.Fatalf("read Paddle setup guide: %v", err)
	}

	workerContents, err := os.ReadFile("../../internal/queue/jobs/subscription_webhook.go")
	if err != nil {
		t.Fatalf("read Paddle webhook worker: %v", err)
	}

	guideEvents := textBetween(t, string(guideContents), "Event types to subscribe:", "4. Click")
	workerSwitch := textBetween(t, string(workerContents), "switch webhook.EventType {", "default:")
	expectedEvents := []string{
		"subscription.created",
		"subscription.activated",
		"subscription.updated",
		"subscription.canceled",
		"subscription.paused",
		"subscription.past_due",
		"subscription.resumed",
		"subscription.trialing",
		"transaction.completed",
		"transaction.payment_failed",
		"transaction.past_due",
		"transaction.canceled",
	}

	for _, event := range expectedEvents {
		t.Run(event, func(t *testing.T) {
			if !strings.Contains(workerSwitch, `case "`+event+`":`) {
				t.Fatalf("Paddle webhook worker must handle %s", event)
			}
			if !strings.Contains(guideEvents, "`"+event+"`") {
				t.Fatalf("Paddle setup guide must subscribe to handled event %s", event)
			}
		})
	}
}

func textBetween(t *testing.T, contents, start, end string) string {
	t.Helper()

	startIndex := strings.Index(contents, start)
	if startIndex < 0 {
		t.Fatalf("production deployment config is missing section start %q", start)
	}

	section := contents[startIndex+len(start):]
	endIndex := strings.Index(section, end)
	if endIndex < 0 {
		t.Fatalf("production deployment config is missing section end %q", end)
	}
	return section[:endIndex]
}

func containsConfigLine(section, required string) bool {
	for _, line := range strings.Split(section, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if trimmed == required {
			return true
		}
	}
	return false
}
