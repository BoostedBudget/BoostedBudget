// revenuecat-bootstrap inventories the prelaunch cutover before recording grants.
// It never changes access directly or sends promotional mutations itself.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/config"
	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/maxnussbaum/BoostedBudget/internal/database"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	rc "github.com/maxnussbaum/BoostedBudget/internal/integration/revenuecat"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

type bootstrapStore interface {
	Inventory(context.Context) ([]*domain.User, error)
	Apply(context.Context, string) (int, error)
}
type bootstrapRemote interface {
	ValidateCatalog(context.Context, map[string][]string, string) error
	FetchSubscriptions(context.Context, string) ([]rc.Subscription, error)
}
type bootstrapResult struct {
	Users         int  `json:"users"`
	PlannedGrants int  `json:"planned_grants"`
	QueuedGrants  int  `json:"queued_grants"`
	Apply         bool `json:"apply"`
}

func runBootstrap(ctx context.Context, repo bootstrapStore, remote bootstrapRemote, catalog map[string][]string, appID string, apply bool) (bootstrapResult, error) {
	out := bootstrapResult{Apply: apply}
	if err := remote.ValidateCatalog(ctx, catalog, appID); err != nil {
		return out, err
	}
	users, err := repo.Inventory(ctx)
	if err != nil {
		return out, err
	}
	out.Users = len(users)
	for _, user := range users {
		if user.IsDeleted || user.DeletionRequestedAt != nil {
			continue
		}
		if user.ClerkUserID == "" {
			return out, errors.New("inventory has an account without a canonical Clerk identity")
		}
		ops, err := domain.PlanRevenueCatBootstrap(user, time.Now())
		if err != nil {
			return out, err
		}
		out.PlannedGrants += len(ops)
		subscriptions, err := remote.FetchSubscriptions(ctx, user.ClerkUserID)
		var httpErr *rc.HTTPError
		if err != nil && (!errors.As(err, &httpErr) || httpErr.StatusCode != 404) {
			return out, err
		}
		for _, sub := range subscriptions {
			if sub.Store != "promotional" && sub.Status != "expired" {
				return out, errors.New("live or unresolved provider billing found; prelaunch bootstrap refused")
			}
		}
	}
	if !apply {
		return out, nil
	}
	for _, user := range users {
		if user.IsDeleted || user.DeletionRequestedAt != nil {
			continue
		}
		queued, err := repo.Apply(ctx, user.ID)
		if err != nil {
			return out, err
		}
		out.QueuedGrants += queued
	}
	return out, nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "RevenueCat bootstrap failed:", err)
		os.Exit(1)
	}
}
func run() error {
	apply := flag.Bool("apply", false, "Record durable grant intents after a read-only inventory passes")
	appID := flag.String("paddle-app-id", "", "RevenueCat Paddle app ID matching the SPA public key")
	confirm := flag.String("confirm-project", "", "Exact RevenueCat project ID required with --apply")
	maintenance := flag.Bool("maintenance-confirmed", false, "Confirm registration and purchase traffic is stopped during cutover")
	flag.Parse()
	project := os.Getenv("REVENUECAT_PROJECT_ID")
	environment := os.Getenv("REVENUECAT_ENVIRONMENT")
	if project == "" || (environment != "sandbox" && environment != "production") || os.Getenv("PADDLE_ENVIRONMENT") != environment {
		return errors.New("set matching REVENUECAT_PROJECT_ID, REVENUECAT_ENVIRONMENT and PADDLE_ENVIRONMENT")
	}
	if *apply && (*confirm != project || !*maintenance) {
		return errors.New("--apply requires --confirm-project matching the configured project and --maintenance-confirmed")
	}
	encryptionKey := os.Getenv("ENCRYPTION_KEY")
	encryptionService, err := services.NewEncryptionService(&config.Config{EncryptionKey: encryptionKey})
	if err != nil {
		return err
	}
	enc, err := appcrypto.NewFieldEncryptor(encryptionService, []byte(encryptionKey))
	if err != nil {
		return err
	}
	client, err := rc.NewClient(rc.Config{ProjectID: project, SecretAPIKey: os.Getenv("REVENUECAT_SECRET_API_KEY"), GrantAPIKey: os.Getenv("REVENUECAT_GRANT_API_KEY"), Environment: environment})
	if err != nil {
		return err
	}
	tiers, err := config.NewTierConfig("")
	if err != nil {
		return err
	}
	expected := map[string][]string{"trial_premium": nil}
	for _, name := range []string{"premium", "household_2", "household_3", "household_4", "household_5"} {
		tier, err := tiers.GetTier(domain.TierName(name))
		if err != nil {
			return err
		}
		if tier.Pricing.PaddlePriceIDMonthly == nil || tier.Pricing.PaddlePriceIDYearly == nil {
			return errors.New("both Paddle price IDs must be configured for every tier")
		}
		expected["paid_"+name] = []string{*tier.Pricing.PaddlePriceIDMonthly, *tier.Pricing.PaddlePriceIDYearly}
		expected["comp_"+name] = nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	db, err := database.NewPoolFromURL(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer db.Close()
	result, err := runBootstrap(ctx, repository.NewRevenueCatBootstrapRepository(db, enc), client, expected, *appID, *apply)
	// Counts only: never print credentials, customer identities, or account metadata.
	//
	// ⚠ THE LOG IS THE ONLY OPERATIONAL SURFACE FOR THIS COMMAND, deliberately. This is a
	// short-lived CLI with no Prometheus listener and no pushgateway anywhere in backend/, so a
	// counter registered here would be scraped by nobody. Batch mechanics are recorded as counts
	// per run instead; if a durable record is ever wanted, revenuecat_operations is the place.
	slog.Default().Info("revenuecat bootstrap run completed",
		"apply", result.Apply,
		"users", result.Users,
		"planned_grants", result.PlannedGrants,
		"queued_grants", result.QueuedGrants,
		"error", err,
	)
	if printErr := json.NewEncoder(os.Stdout).Encode(result); printErr != nil {
		return printErr
	}
	return err
}
