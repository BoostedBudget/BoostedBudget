package main

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/queue/jobs"
)

func TestRevenueCatProductionWiring(t *testing.T) {
	b, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(b)
	for _, fragment := range []string{
		"WithRevenueCatBilling(services.NewRevenueCatBillingService(revenueCatSummaryService, paddleService, paddleService.GetClient()))",
		"WithCheckout(repository.NewRevenueCatCheckoutRepository(db, fieldEncryptor))",
		"WithTierConfig(tierConfig)",
		"revenuecat.NewClient(revenuecat.Config{", "repository.NewRevenueCatRepository(db, fieldEncryptor, cfg.RetentionPeriodMonths)",
		"profileRepo.EnableRevenueCatRetentionGuard()",
		"services.NewRevenueCatService(revenueCatRepo, revenueCatClient, profileRepo)",
		"householdEntitlementService.SetOwnAccessResolver(revenueCatService)",
		"registrationService.SetTrialGranter(revenueCatService)", "subscriptionWebhookWorker.SetRevenueCatAuthority()",
		"adminUserService.SetRevenueCatSummary(revenueCatSummaryService)",
		"operations.RegisterRevenueCatWebhookRoutes(humaAPI", "revenueCatRepairWorker.SetJobQueue(jobQueue)",
		"river.AddWorker(riverWorkers, revenueCatSyncWorker)", "river.AddWorker(riverWorkers, revenueCatRepairWorker)",
		"WithRevenueCat(operations.RevenueCatAdminDeps{Grants: revenueCatService, Profiles: profileRepo, States: revenueCatRepo, Summary: revenueCatSummaryService, Admin: repository.NewRevenueCatAdminRepository(db)})",
		"operations.RegisterAdminRevenueCatRoutes(humaAPI, humaAdminHandler)",
	} {
		if !strings.Contains(source, fragment) {
			t.Errorf("missing production authority wiring: %s", fragment)
		}
	}
	for _, name := range []string{"trialExpirationCheckWorker", "pastDueCheckWorker", "paddleReconciliationWorker", "manualTierOverrideExpiryCheckWorker"} {
		if strings.Contains(source, "river.AddWorker(riverWorkers, "+name+")") {
			t.Errorf("legacy worker remains live: %s", name)
		}
		if strings.Contains(source, name+":") {
			t.Errorf("legacy worker remains periodically scheduled: %s", name)
		}
	}
	for _, kind := range []string{"TrialExpirationCheckArgs", "PastDueCheckArgs", "PaddleReconciliationArgs", "ManualTierOverrideExpiryCheckArgs"} {
		if !strings.Contains(source, "jobs.RetiredSubscriptionWorker[jobs."+kind+"]") {
			t.Errorf("queued legacy kind has no drain: %s", kind)
		}
	}
}

func TestRevenueCatRepairPeriodicRegistration(t *testing.T) {
	deps := periodicJobsDeps{revenueCatRepairWorker: jobs.NewRevenueCatRepairWorker(nil, nil)}
	for _, spec := range buildPeriodicJobSpecs(deps) {
		if spec.name != "RevenueCat repair" {
			continue
		}
		args, opts := spec.build()
		if args.Kind() != "revenuecat_repair" || spec.interval != time.Minute || !spec.opts.RunOnStart || spec.opts.ID != args.Kind() || opts.UniqueOpts.ByPeriod != time.Minute {
			t.Fatalf("incorrect repair schedule: %+v", spec)
		}
		return
	}
	t.Fatal("RevenueCat repair periodic registration missing")
}
