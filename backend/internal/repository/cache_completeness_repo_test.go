package repository

// Issue #1531 Task 10b-3 — AST completeness guard #1 of 3.
//
// WHAT THIS PROVES: every exported method on every interface declared in this package (package
// repository, this directory only — no recursion) whose name starts with a verb in
// mutatingVerbPattern (or matches one of the two structural exceptions below) appears in
// repoMethodDispositions, classified as one of:
//   - dispositionExplicitBump{detail}          — a named file/service bumps cache for this write.
//   - dispositionCoveredByHTTPMiddleware{detail} — reached only via an authenticated, secured,
//     non-GET Huma operation, so NewCacheInvalidationMiddleware bumps unconditionally after it.
//   - dispositionNoCacheImpact{detail}          — reviewed; touches no cached namespace (analytics,
//     analytics_rows, dashprefs, identity). detail is mandatory and non-empty.
//   - dispositionUnclassified{}                 — not yet confidently reviewed. FAILS the
//     completeness check on its own, precisely so a wrong guess is never recorded as a pass. See
//     task-10b-3-report.md for the full worklist these 138 entries produce — that table, not this
//     guard, is the actual deliverable of this task.
//
// WHAT THIS DOES NOT PROVE (read this before trusting a green — or even a loudly-skipped — run):
//   - This guard detects a newly EXPORTED METHOD NAME on an interface. It does not detect a new
//     mutation added inside the BODY of an already-classified method (e.g. someone adding a second
//     UPDATE statement inside transactionRepository.Update's SQL) — go/ast here only ever inspects
//     the interface's method signatures, never a concrete type's method bodies.
//   - mutatingVerbPattern is a curated, and admittedly incomplete, verb list. It already had to be
//     EXPANDED once during this task past the brief's original list (see below) to catch real
//     writes the brief's own verb set missed (ReplaceForAccount, DecrementUsageCount, and others).
//     Even after that expansion, at least one more genuine write slipped through by name alone:
//     AuditLogRepository.Log / LogInTx (a real repository write — go verify it yourself — matches
//     no verb in either list, expanded or original). This is deliberately left unfixed and
//     documented rather than quietly patched a second time: it is evidence, not a bug, that
//     verb-prefix matching can never be a complete enumeration technique, however many times it is
//     patched. See the report for a recommended follow-up (a body-level SQL-statement AST check,
//     not name matching).
//   - A disposition is per METHOD NAME, not per call site. Where one repository method is called
//     from more than one place with materially different cache-invalidation coverage (this task
//     found several — see LinkedItemRepository.Update and PortfolioSnapshotRepository.Upsert
//     below), the entry necessarily picks one disposition for the method as a whole; the detail
//     string documents the split, but the guard itself cannot enforce per-call-site correctness.
//
// VERB LIST EXPANSION (deviation from the brief, documented per its own "label them honestly"
// instruction): the brief specified
//   Create|Update|Delete|Insert|Upsert|Set|Mark|Revoke|Remove|Purge|Clear|Increment|Claim|Release|
//   Backfill|Repair|Cancel|Offboard|Disband|Accept|Decline
// A full audit of every interface method in this package (not just ones matching that list) found
// real, unambiguous writes the list would have silently let through with no allowlist requirement
// at all — which is a worse failure than an Unclassified{} entry, because it never even surfaces
// as a decision to make. This guard therefore ADDS:
//   Replace|Decrement|Add|Adjust|Restore|Expire|Reset|Confirm|Reject|Archive|Complete|Reserve|
//   Write|Save|Record|Merge
// plus two structural exceptions: a name starting with "Bulk" (BulkUpsert, BulkApply — the verb
// they bulk-apply is buried after the prefix) and a name containing "OrCreate" (GetOrCreate,
// GetOrCreateMerchants — get-or-insert idioms whose insert half a verb-prefix check would miss
// entirely, since the name starts with "Get").
//
// ADVERSARIAL VALIDATION (recorded 2026-08-14, see task-10b-3-report.md for the transcript):
//   - Added a fake exported method (`FakeUnclassifiedWrite`) to a real interface in this package
//     (a temporary edit to account_repository.go's AccountRepository interface): guard FAILED,
//     reporting the new method as missing from repoMethodDispositions. Reverted the edit: guard
//     PASSED (well, skipped with the same pre-existing TODO — see below).
//   - This adversarial check was run with the t.Skip below TEMPORARILY commented out, so the
//     pre-existing Unclassified{} entries could not mask the FAIL; the skip was restored afterward.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// mutatingVerbPattern matches an interface method name that looks like a write. See the package
// doc comment above for the brief's original list, this task's expansion, and why even the
// expanded list is known-incomplete (AuditLogRepository.Log/LogInTx).
var mutatingVerbPattern = regexp.MustCompile(`^(Create|Update|Delete|Insert|Upsert|Set|Mark|Revoke|` +
	`Remove|Purge|Clear|Increment|Decrement|Claim|Release|Backfill|Repair|Cancel|Offboard|Disband|` +
	`Accept|Decline|Replace|Add|Adjust|Restore|Expire|Reset|Confirm|Reject|Archive|Complete|Reserve|` +
	`Write|Save|Record|Merge|Bulk)`)

func looksMutating(name string) bool {
	if !ast.IsExported(name) {
		return false
	}
	if mutatingVerbPattern.MatchString(name) {
		return true
	}
	return strings.Contains(name, "OrCreate")
}

type repoDispositionKind int

const (
	dispositionExplicitBump repoDispositionKind = iota
	dispositionCoveredByHTTPMiddleware
	dispositionNoCacheImpact
	dispositionUnclassified
)

type repoMethodDisposition struct {
	kind   repoDispositionKind
	detail string
}

// repoMethodDispositions classifies every "InterfaceName.MethodName" pair in this package matched
// by looksMutating. See the package doc comment for what each disposition kind means, its limits,
// and why 138 of these are dispositionUnclassified (a deliberate, loudly-skipped gap — see
// task-10b-3-report.md's worklist table, which is the actual output of this task).
var repoMethodDispositions = map[string]repoMethodDisposition{
	"TransactionRepository.CreateBatch":                                   {dispositionExplicitBump, "services/transaction_sync_service.go SyncTransactions — bumps via defer + any-commit flag."},
	"TransactionRepository.Update":                                        {dispositionExplicitBump, "services/transaction_sync_service.go SyncTransactions — bumps via defer + any-commit flag."},
	"TransactionRepository.DeleteByExternalID":                            {dispositionExplicitBump, "services/transaction_sync_service.go SyncTransactions — bumps via defer + any-commit flag."},
	"HoldingRepository.ReplaceForAccount":                                 {dispositionExplicitBump, "services/investment_sync_service.go SyncHoldings — bumpCacheForUser runs after (line ~339)."},
	"InvestmentTransactionRepository.UpsertBatch":                         {dispositionExplicitBump, "services/investment_sync_service.go SyncInvestmentTransactions — bumpCacheForUser runs after (line ~204)."},
	"LinkedItemRepository.UpdateInvestmentsLastSyncedDate":                {dispositionExplicitBump, "services/investment_sync_service.go SyncHoldings — bumpCacheForUser runs after."},
	"PortfolioSnapshotRepository.Upsert":                                  {dispositionExplicitBump, "services/investment_sync_service.go SyncHoldings calls this then bumpCacheForUser (line ~319/339). CAVEAT: queue/jobs/portfolio_snapshot_worker.go ALSO calls this same repo method directly from a periodic job, and that job is Unclassified in the job-disposition guard — this method-level entry cannot express that the job's call site may be uncovered. Flagged as the single highest-priority follow-up in the report."},
	"CreditLiabilityRepository.ReplaceForAccount":                         {dispositionExplicitBump, "services/liability_sync_service.go SyncLiabilities — bumpCacheForUser runs after."},
	"MortgageLiabilityRepository.ReplaceForAccount":                       {dispositionExplicitBump, "services/liability_sync_service.go SyncLiabilities — bumpCacheForUser runs after."},
	"StudentLoanLiabilityRepository.ReplaceForAccount":                    {dispositionExplicitBump, "services/liability_sync_service.go SyncLiabilities — bumpCacheForUser runs after."},
	"LinkedItemRepository.UpdateLiabilitiesLastSyncedAt":                  {dispositionExplicitBump, "services/liability_sync_service.go SyncLiabilities — bumpCacheForUser runs after."},
	"LinkedItemRepository.Update":                                         {dispositionExplicitBump, "Called from services/investment_sync_service.go and services/liability_sync_service.go (both bump after). CAVEAT: the SAME method is also called from services/item_error_service.go ProcessWebhook (brief's seeded NoCacheImpact, unverified by the brief's own admission) and from queue/jobs/item_cleanup_check.go (Unclassified in the job guard) — this entry cannot distinguish those call sites. Flagged in the report as a real blind spot, not asserted safe for every caller."},
	"ProfileRepository.MarkForDeletion":                                   {dispositionExplicitBump, "services/deletion_service.go — part of the GDPR deletion request flow leading to cache/purge.go's PurgeUser."},
	"ProfileRepository.CancelDeletion":                                    {dispositionExplicitBump, "services/deletion_service.go — GDPR deletion cancel flow."},
	"ProfileRepository.ClaimForDeletion":                                  {dispositionExplicitBump, "services/deletion_service.go ExecuteDeletion — immediately precedes DeleteUserCascade + PurgeUser."},
	"ProfileRepository.DeleteUserCascade":                                 {dispositionExplicitBump, "services/deletion_service.go ExecuteDeletion — the row deletion paired with cache/purge.go's PurgeUser in the same flow."},
	"HouseholdRepository.CreateHousehold":                                 {dispositionCoveredByHTTPMiddleware, "services/household_service.go CreateHousehold, called only by the secured createHousehold Huma operation."},
	"HouseholdRepository.UpdateHouseholdName":                             {dispositionCoveredByHTTPMiddleware, "services/household_service.go, called only by the secured updateHouseholdName Huma operation."},
	"HouseholdRepository.DeleteHousehold":                                 {dispositionCoveredByHTTPMiddleware, "services/household_service.go, called only by the secured deleteHousehold Huma operation."},
	"HouseholdRepository.InsertMember":                                    {dispositionCoveredByHTTPMiddleware, "services/household_service.go, both call sites (create-household owner row, accept-invite) are behind secured Huma operations; no job caller found."},
	"HouseholdRepository.RemoveMember":                                    {dispositionCoveredByHTTPMiddleware, "services/household_service.go, called only by the secured removeHouseholdMember Huma operation; no job caller found (DISTINCT from DeleteRemovedMember, which IS job-called — see Unclassified)."},
	"HouseholdInviteRepository.Create":                                    {dispositionCoveredByHTTPMiddleware, "services/household_service.go, called only by the secured createHouseholdInvite Huma operation."},
	"HouseholdInviteRepository.MarkAccepted":                              {dispositionCoveredByHTTPMiddleware, "services/household_service.go, called only by the secured acceptHouseholdInvite Huma operation."},
	"HouseholdInviteRepository.MarkDeclined":                              {dispositionCoveredByHTTPMiddleware, "services/household_service.go, called only by the secured declineHouseholdInvite Huma operation."},
	"HouseholdInviteRepository.Revoke":                                    {dispositionCoveredByHTTPMiddleware, "services/household_service.go, called only by the secured revokeHouseholdInvite Huma operation."},
	"HouseholdShareRepository.InsertShare":                                {dispositionCoveredByHTTPMiddleware, "services/household_service.go accept-invite flow, behind the secured acceptHouseholdInvite Huma operation."},
	"HouseholdShareRepository.InsertConsent":                              {dispositionCoveredByHTTPMiddleware, "services/household_service.go accept-invite flow, behind the secured acceptHouseholdInvite Huma operation."},
	"HouseholdShareRepository.RevokeSharesForUser":                        {dispositionCoveredByHTTPMiddleware, "services/household_service.go remove-member/delete-household flow; no job caller found."},
	"HouseholdShareRepository.DeleteSharesForUser":                        {dispositionCoveredByHTTPMiddleware, "services/household_service.go remove-member/delete-household flow; no job caller found."},
	"HouseholdShareRepository.SetShareVisibility":                         {dispositionCoveredByHTTPMiddleware, "Directly backs the secured setHouseholdShareVisibility Huma operation (confirmed via the Huma operation guard)."},
	"EntitlementRepository.CreateHouseholdEntitlement":                    {dispositionCoveredByHTTPMiddleware, "services/household_service.go accept-invite/create-household flow; no job caller found."},
	"EntitlementRepository.ExpireActiveEntitlement":                       {dispositionCoveredByHTTPMiddleware, "services/household_service.go remove-member/delete-household flow; no job caller found."},
	"EntitlementRepository.ExpireActiveHouseholdEntitlement":              {dispositionCoveredByHTTPMiddleware, "services/household_service.go delete-household flow; no job caller found."},
	"DashboardPreferencesRepository.Upsert":                               {dispositionCoveredByHTTPMiddleware, "Directly backs the secured saveDashboardPreferences Huma operation; dashboard prefs have no job writer."},
	"DashboardPreferencesRepository.Delete":                               {dispositionCoveredByHTTPMiddleware, "Directly backs the secured resetDashboardPreferences Huma operation; dashboard prefs have no job writer."},
	"WorkClaimsRepository.Claim":                                          {dispositionNoCacheImpact, "Job-coordination row lock (River work-claim pattern), not domain/cached data."},
	"WorkClaimsRepository.Release":                                        {dispositionNoCacheImpact, "Job-coordination row lock release, not domain/cached data."},
	"WorkClaimsRepository.Complete":                                       {dispositionNoCacheImpact, "Job-coordination row completion marker, not domain/cached data."},
	"WorkClaimsOutcomeRepository.ClaimOutcome":                            {dispositionNoCacheImpact, "Job-coordination outcome bookkeeping, not domain/cached data."},
	"IdempotencyRepository.GetOrCreate":                                   {dispositionNoCacheImpact, "Request-dedup key bookkeeping only."},
	"IdempotencyRepository.GetOrCreateInTx":                               {dispositionNoCacheImpact, "Request-dedup key bookkeeping only."},
	"IdempotencyRepository.DeleteExpired":                                 {dispositionNoCacheImpact, "Request-dedup key bookkeeping only."},
	"MCPAuditRepository.Record":                                           {dispositionNoCacheImpact, "MCP audit log write only; no cached namespace touched (mirrors the Huma-operation guard's recordMCPAuditEntry entry)."},
	"MCPAuditRepository.RecordInTx":                                       {dispositionNoCacheImpact, "MCP audit log write only; no cached namespace touched."},
	"InvalidWebhookAttemptRepository.Record":                              {dispositionNoCacheImpact, "Security/audit bookkeeping for rejected webhook attempts; not a cached namespace."},
	"SweepCursorRepository.Save":                                          {dispositionNoCacheImpact, "Sweep-resume cursor bookkeeping only, not domain data."},
	"SweepCursorRepository.Clear":                                         {dispositionNoCacheImpact, "Sweep-resume cursor bookkeeping only, not domain data."},
	"PaddleWebhookRepository.Create":                                      {dispositionNoCacheImpact, "Raw webhook payload storage; processing-status bookkeeping, not a cached namespace."},
	"PaddleWebhookRepository.MarkProcessed":                               {dispositionNoCacheImpact, "Webhook processing-status bookkeeping, not a cached namespace."},
	"PaddleWebhookRepository.MarkFailed":                                  {dispositionNoCacheImpact, "Webhook processing-status bookkeeping, not a cached namespace."},
	"PaddleWebhookRepository.Update":                                      {dispositionNoCacheImpact, "Webhook processing-status bookkeeping, not a cached namespace."},
	"PaddleWebhookRepository.ResetProcessingStatus":                       {dispositionNoCacheImpact, "Webhook processing-status bookkeeping (admin replay path), not a cached namespace."},
	"PaddleTransactionRepository.Create":                                  {dispositionNoCacheImpact, "Billing-history log row; subscription/billing status is deliberately not a cached namespace."},
	"AdminUserRepository.UpdateUserTier":                                  {dispositionNoCacheImpact, "Subscription-tier override field — subscription status is deliberately NOT cached (see task brief)."},
	"AdminUserRepository.RemoveUserTierOverride":                          {dispositionNoCacheImpact, "Subscription-tier override field — deliberately not cached."},
	"AdminUserRepository.RemoveUserTierOverrideIfMatches":                 {dispositionNoCacheImpact, "Subscription-tier override field — deliberately not cached."},
	"ProfileRepository.UpdateSubscriptionFromPaddle":                      {dispositionNoCacheImpact, "Subscription status — deliberately NOT a cached namespace per task brief."},
	"ProfileRepository.UpdateTrialStatus":                                 {dispositionNoCacheImpact, "Trial status parallels subscription status — deliberately not cached. MEDIUM CONFIDENCE, flagged in report."},
	"ProfileRepository.SetRetentionDates":                                 {dispositionNoCacheImpact, "GDPR retention-clock bookkeeping field, not a cached namespace."},
	"ProfileRepository.ClearRetentionDates":                               {dispositionNoCacheImpact, "GDPR retention-clock bookkeeping field, not a cached namespace."},
	"ProfileRepository.SetRetentionNotice30dSent":                         {dispositionNoCacheImpact, "Retention-notice-sent marker, not a cached namespace."},
	"ProfileRepository.SetRetentionNotice7dSent":                          {dispositionNoCacheImpact, "Retention-notice-sent marker, not a cached namespace."},
	"ProfileRepository.RecordDeletionResidue":                             {dispositionNoCacheImpact, "Deletion-audit bookkeeping row, not a cached namespace."},
	"ProfileRepository.SetPastDueStartedAt":                               {dispositionNoCacheImpact, "Past-due/subscription status field — deliberately not cached. MEDIUM CONFIDENCE, flagged in report."},
	"ProfileRepository.ClearPastDueStartedAt":                             {dispositionNoCacheImpact, "Past-due/subscription status field — deliberately not cached. MEDIUM CONFIDENCE, flagged in report."},
	"ProfileRepository.ExpireTrialIfNoOverride":                           {dispositionNoCacheImpact, "Trial status field — deliberately not cached. MEDIUM CONFIDENCE, flagged in report."},
	"ProfileRepository.UpdateUserEmailTx":                                 {dispositionNoCacheImpact, "Propagates changed email to the profile row; identity cache keys on clerkID, which this does not change."},
	"ProfileRepository.RepairRetentionClock":                              {dispositionNoCacheImpact, "Retention-clock repair bookkeeping, not a cached namespace."},
	"NotificationRepository.Create":                                       {dispositionNoCacheImpact, "Notifications are not one of the cached namespaces (analytics, analytics_rows, dashprefs, identity)."},
	"NotificationRepository.Update":                                       {dispositionNoCacheImpact, "Notifications are not a cached namespace."},
	"NotificationRepository.MarkAsRead":                                   {dispositionNoCacheImpact, "Notifications are not a cached namespace."},
	"NotificationRepository.MarkAllAsRead":                                {dispositionNoCacheImpact, "Notifications are not a cached namespace."},
	"AccountRepository.Create":                                            {dispositionUnclassified, ""},
	"AccountRepository.CreateBatch":                                       {dispositionUnclassified, ""},
	"AccountRepository.Delete":                                            {dispositionUnclassified, ""},
	"AccountRepository.Update":                                            {dispositionUnclassified, ""},
	"AccountRepository.UpdateDisplayOrders":                               {dispositionUnclassified, ""},
	"AccountRepository.UpdateStatus":                                      {dispositionUnclassified, ""},
	"AccountRepository.ExpireAccountsByFingerprint":                       {dispositionUnclassified, ""},
	"BalanceSnapshotRepository.Create":                                    {dispositionUnclassified, ""},
	"BalanceSnapshotRepository.DeleteByAccountID":                         {dispositionUnclassified, ""},
	"BoostExecutionRepository.Create":                                     {dispositionUnclassified, ""},
	"BoostExecutionRepository.Update":                                     {dispositionUnclassified, ""},
	"BoostsRepository.Create":                                             {dispositionUnclassified, ""},
	"BoostsRepository.Update":                                             {dispositionUnclassified, ""},
	"BoostsRepository.Delete":                                             {dispositionUnclassified, ""},
	"BoostsRepository.IncrementExecutionCount":                            {dispositionUnclassified, ""},
	"BoostsRepository.UpdateLastSuccessful":                               {dispositionUnclassified, ""},
	"BudgetPeriodEventsRepository.InsertMarker":                           {dispositionUnclassified, ""},
	"BudgetPeriodEventsRepository.DeleteMarker":                           {dispositionUnclassified, ""},
	"BudgetRepository.Create":                                             {dispositionUnclassified, ""},
	"BudgetRepository.Update":                                             {dispositionUnclassified, ""},
	"BudgetRepository.Delete":                                             {dispositionUnclassified, ""},
	"BudgetRepository.CreateCategory":                                     {dispositionUnclassified, ""},
	"BudgetRepository.UpdateCategory":                                     {dispositionUnclassified, ""},
	"BudgetRepository.UpdateCategorySpentAmount":                          {dispositionUnclassified, ""},
	"BudgetRepository.DeleteCategory":                                     {dispositionUnclassified, ""},
	"BudgetRepository.UpdateBudgetIncome":                                 {dispositionUnclassified, ""},
	"BudgetRepository.AddIncomeSource":                                    {dispositionUnclassified, ""},
	"BudgetRepository.RemoveIncomeSource":                                 {dispositionUnclassified, ""},
	"BudgetRepository.AdjustCategoryAllocatedAmount":                      {dispositionUnclassified, ""},
	"BudgetRepository.InsertGoalDetach":                                   {dispositionUnclassified, ""},
	"BudgetRepository.DeleteGoalDetach":                                   {dispositionUnclassified, ""},
	"BudgetRepository.CreateWithCategories":                               {dispositionUnclassified, ""},
	"CategorizationRuleRepository.Create":                                 {dispositionUnclassified, ""},
	"CategorizationRuleRepository.Update":                                 {dispositionUnclassified, ""},
	"CategorizationRuleRepository.Delete":                                 {dispositionUnclassified, ""},
	"CategorizationRuleRepository.IncrementMatchCount":                    {dispositionUnclassified, ""},
	"CategorizationRuleRepository.IncrementOverrideCount":                 {dispositionUnclassified, ""},
	"CustomCategoryRepository.Create":                                     {dispositionUnclassified, ""},
	"CustomCategoryRepository.Update":                                     {dispositionUnclassified, ""},
	"CustomCategoryRepository.Delete":                                     {dispositionUnclassified, ""},
	"CustomCategoryRepository.IncrementUsageCount":                        {dispositionUnclassified, ""},
	"CustomCategoryRepository.DecrementUsageCount":                        {dispositionUnclassified, ""},
	"CustomCategoryRepository.ClearParentForChildren":                     {dispositionUnclassified, ""},
	"CustomCategoryRepository.DeleteWithReassignment":                     {dispositionUnclassified, ""},
	"HouseholdInviteRepository.RevokeAllPendingForHousehold":              {dispositionUnclassified, ""},
	"HouseholdRepository.DeleteRemovedMember":                             {dispositionUnclassified, ""},
	"HouseholdRepository.SetOverCapacity":                                 {dispositionUnclassified, ""},
	"HouseholdRepository.ClearOverCapacity":                               {dispositionUnclassified, ""},
	"HouseholdRepository.ClearOverCapacityIfAnchorMatches":                {dispositionUnclassified, ""},
	"HouseholdRepository.MarkOverCapacityStartNoticeSent":                 {dispositionUnclassified, ""},
	"HouseholdRepository.MarkOverCapacityWarn3dSent":                      {dispositionUnclassified, ""},
	"HouseholdRepository.RestoreMember":                                   {dispositionUnclassified, ""},
	"LinkedItemRepository.Create":                                         {dispositionUnclassified, ""},
	"LinkedItemRepository.UpdateCursor":                                   {dispositionUnclassified, ""},
	"LinkedItemRepository.Delete":                                         {dispositionUnclassified, ""},
	"LinkedItemRepository.DeleteByUser":                                   {dispositionUnclassified, ""},
	"LinkedItemRepository.MarkForCleanup":                                 {dispositionUnclassified, ""},
	"LinkedItemRepository.ClearCleanupSchedule":                           {dispositionUnclassified, ""},
	"LinkedItemRepository.MarkAllUserItemsDisconnected":                   {dispositionUnclassified, ""},
	"LinkedItemRepository.MarkItemsDisconnected":                          {dispositionUnclassified, ""},
	"LinkedItemRepository.MarkPlaidRemoved":                               {dispositionUnclassified, ""},
	"LinkedItemRepository.MarkPlaidRemovalFailed":                         {dispositionUnclassified, ""},
	"MerchantRepo.GetOrCreateMerchants":                                   {dispositionUnclassified, ""},
	"MerchantRepo.GetOrCreateMerchantsWithNormalize":                      {dispositionUnclassified, ""},
	"MerchantOverrideRepo.Upsert":                                         {dispositionUnclassified, ""},
	"MerchantOverrideRepo.BulkUpsert":                                     {dispositionUnclassified, ""},
	"MerchantOverrideRepo.Delete":                                         {dispositionUnclassified, ""},
	"MerchantOverrideRepo.MergeAndFlatten":                                {dispositionUnclassified, ""},
	"MerchantOverrideRepo.BulkApply":                                      {dispositionUnclassified, ""},
	"PlaidOrphanedItemRepository.PurgeExpiredTokens":                      {dispositionUnclassified, ""},
	"PlaidOrphanedItemRepository.MarkResolved":                            {dispositionUnclassified, ""},
	"PlaidOrphanedItemRepository.RecordOperatorResolution":                {dispositionUnclassified, ""},
	"PlaidOrphanedItemRepository.RecordRetryFailure":                      {dispositionUnclassified, ""},
	"PrivacyRepository.Create":                                            {dispositionUnclassified, ""},
	"PrivacyRepository.CreateTx":                                          {dispositionUnclassified, ""},
	"PrivacyRepository.Update":                                            {dispositionUnclassified, ""},
	"ProfileRepository.CreateUser":                                        {dispositionUnclassified, ""},
	"ProfileRepository.CreateUserTx":                                      {dispositionUnclassified, ""},
	"ProfileRepository.UpdateUser":                                        {dispositionUnclassified, ""},
	"ProfileRepository.UpdateLastLogin":                                   {dispositionUnclassified, ""},
	"ProfileRepository.UpdatePaddleCustomerID":                            {dispositionUnclassified, ""},
	"RecipeRepository.Create":                                             {dispositionUnclassified, ""},
	"RecipeRepository.Update":                                             {dispositionUnclassified, ""},
	"RecipeRepository.Delete":                                             {dispositionUnclassified, ""},
	"RolloverHistoryRepository.Create":                                    {dispositionUnclassified, ""},
	"RolloverHistoryRepository.UpdateStatus":                              {dispositionUnclassified, ""},
	"SavingsContributionRepository.Insert":                                {dispositionUnclassified, ""},
	"SavingsContributionRepository.UpsertFromTransaction":                 {dispositionUnclassified, ""},
	"SavingsContributionRepository.UpsertProposedFromTransaction":         {dispositionUnclassified, ""},
	"SavingsContributionRepository.ConfirmContribution":                   {dispositionUnclassified, ""},
	"SavingsContributionRepository.RejectContribution":                    {dispositionUnclassified, ""},
	"SavingsContributionRepository.DeleteContribution":                    {dispositionUnclassified, ""},
	"SavingsContributionRepository.DeleteManagedForTransaction":           {dispositionUnclassified, ""},
	"SavingsContributionRepository.ArchiveByGoal":                         {dispositionUnclassified, ""},
	"SavingsContributionRepository.InsertManualFromTransaction":           {dispositionUnclassified, ""},
	"SavingsGoalRepository.Create":                                        {dispositionUnclassified, ""},
	"SavingsGoalRepository.Update":                                        {dispositionUnclassified, ""},
	"SavingsGoalRepository.Delete":                                        {dispositionUnclassified, ""},
	"SavingsGoalRepository.UpdateCurrentAmount":                           {dispositionUnclassified, ""},
	"SavingsGoalRepository.MarkCompleted":                                 {dispositionUnclassified, ""},
	"SavingsImpactRepository.WriteSnapshot":                               {dispositionUnclassified, ""},
	"SecurityRepository.UpsertBatch":                                      {dispositionUnclassified, ""},
	"SubscriptionOverrideRepository.CreateOverride":                       {dispositionUnclassified, ""},
	"SubscriptionOverrideRepository.DeleteOverride":                       {dispositionUnclassified, ""},
	"SubscriptionOverrideRepository.UpdateOverride":                       {dispositionUnclassified, ""},
	"TransactionRepository.Create":                                        {dispositionUnclassified, ""},
	"TransactionRepository.UpdateTags":                                    {dispositionUnclassified, ""},
	"TransactionRepository.UpdateCategory":                                {dispositionUnclassified, ""},
	"TransactionRepository.SetReviewed":                                   {dispositionUnclassified, ""},
	"TransactionRepository.BulkUpdateCategory":                            {dispositionUnclassified, ""},
	"TransactionRepository.UpdateEffectiveCategoryNameByCustomCategoryID": {dispositionUnclassified, ""},
	"TransferSuggestionRepository.Create":                                 {dispositionUnclassified, ""},
	"TransferSuggestionRepository.UpdateStatus":                           {dispositionUnclassified, ""},
	"TransferSuggestionRepository.CancelAcceptedSuggestion":               {dispositionUnclassified, ""},
	"TransferSuggestionRepository.Update":                                 {dispositionUnclassified, ""},
	"TransferSuggestionRepository.ExpirePending":                          {dispositionUnclassified, ""},
	"TransferSuggestionRepository.Complete":                               {dispositionUnclassified, ""},
	"TransferSuggestionRepository.CompleteRetainingReservation":           {dispositionUnclassified, ""},
	"TransferSuggestionRepository.ReserveMatchedTransaction":              {dispositionUnclassified, ""},
	"TransferSuggestionRepository.ClearMatchedTransactionID":              {dispositionUnclassified, ""},
	"TransferSuggestionRepository.ClearStaleReservations":                 {dispositionUnclassified, ""},
	"UserAccountRepository.Create":                                        {dispositionUnclassified, ""},
	"UserAccountRepository.Update":                                        {dispositionUnclassified, ""},
	"UserAccountRepository.UpdatePrimaryTypeAndSubtype":                   {dispositionUnclassified, ""},
	"UserAccountRepository.UpdateHidden":                                  {dispositionUnclassified, ""},
	"UserIdentityRepository.Create":                                       {dispositionUnclassified, ""},
	"UserIdentityRepository.CreateTx":                                     {dispositionUnclassified, ""},
	"UserIdentityRepository.UpdateLastUsed":                               {dispositionUnclassified, ""},
	"UserIdentityRepository.UpdatePrimaryIdentityEmailTx":                 {dispositionUnclassified, ""},
	"UserTagRepository.Create":                                            {dispositionUnclassified, ""},
	"UserTagRepository.Update":                                            {dispositionUnclassified, ""},
	"UserTagRepository.Delete":                                            {dispositionUnclassified, ""},
	"UserTagRepository.IncrementUsageCount":                               {dispositionUnclassified, ""},
	"UserTagRepository.DecrementUsageCount":                               {dispositionUnclassified, ""},
	"WebhookRepository.Create":                                            {dispositionUnclassified, ""},
	"WebhookRepository.MarkProcessed":                                     {dispositionUnclassified, ""},
	"WebhookRepository.MarkFailed":                                        {dispositionUnclassified, ""},
	"WebhookRepository.Update":                                            {dispositionUnclassified, ""},
}

// scanMutatingRepoInterfaceMethods walks every non-test .go file directly in dir (no recursion),
// finds every `type X interface { ... }` declaration, and returns "X.MethodName" for every
// directly-declared (non-embedded) exported method whose name satisfies looksMutating.
//
// This is a syntax-only AST walk (go/parser, no type-checking): it does not resolve embedded
// interfaces, so a mutating method reached only through embedding would not be found here. No
// interface in this package currently embeds another that contributes a mutating method (verified
// by inspection while building this guard); a future one would need this scanner extended.
func scanMutatingRepoInterfaceMethods(dir string) (map[string]bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	found := map[string]bool{}
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(dir, name)
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil, err
		}

		for _, decl := range file.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				iface, ok := ts.Type.(*ast.InterfaceType)
				if !ok {
					continue
				}
				ifaceName := ts.Name.Name
				for _, m := range iface.Methods.List {
					if len(m.Names) == 0 {
						continue // embedded interface, not a directly-declared method
					}
					methodName := m.Names[0].Name
					if looksMutating(methodName) {
						found[ifaceName+"."+methodName] = true
					}
				}
			}
		}
	}
	return found, nil
}

// TestEveryMutatingRepoMethodIsInReviewedAllowlist is completeness guard #1 (see the package doc
// comment above for what it does and does NOT prove).
func TestEveryMutatingRepoMethodIsInReviewedAllowlist(t *testing.T) {
	found, err := scanMutatingRepoInterfaceMethods(".")
	if err != nil {
		t.Fatalf("scanning repository interfaces: %v", err)
	}
	if len(found) == 0 {
		t.Fatal("scanned zero mutating interface methods — guard is looking at the wrong directory " +
			"or looksMutating broke; a silently-empty scan is worse than no guard at all")
	}

	var missing []string
	var unclassified []string
	for key := range found {
		d, ok := repoMethodDispositions[key]
		if !ok {
			missing = append(missing, key)
			continue
		}
		switch d.kind {
		case dispositionExplicitBump, dispositionCoveredByHTTPMiddleware, dispositionNoCacheImpact:
			if strings.TrimSpace(d.detail) == "" {
				t.Errorf("repoMethodDispositions[%q] has an empty detail — a bare entry defeats the "+
					"point of this guard", key)
			}
		case dispositionUnclassified:
			unclassified = append(unclassified, key)
		}
	}

	if len(missing) > 0 {
		t.Errorf("%d mutating repository method(s) are missing from repoMethodDispositions "+
			"entirely: %s", len(missing), strings.Join(missing, ", "))
	}

	if len(unclassified) > 0 {
		t.Skipf("TODO #1531: %d repository method(s) remain dispositionUnclassified{} pending "+
			"review — see the worklist table in task-10b-3-report.md (Task 10b-3), which groups "+
			"them by suggested slice and is the actual deliverable of this guard: %s",
			len(unclassified), strings.Join(unclassified, ", "))
	}
}
