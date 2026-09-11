package queue

import (
	"reflect"
	"strings"
)

// JobArgsShape declares the JSON wire shape of one River job kind's args.
//
// Why this exists (Issue #1490, worked example #1486): `internal/services` cannot import
// `internal/queue/jobs` — jobs imports services, so that would be an import cycle. Services
// therefore MIRRORS seven args types locally (plaidSyncJobArgs, investmentHoldingsSyncJobArgs,
// investmentTransactionsSyncJobArgs, itemCleanupJobArgs, effectiveCategoryRefreshJobArgs,
// budgetHierarchyResyncJobArgs, emailChangePropagationArgs), each re-declaring the same JSON tags
// and returning the same Kind() string as its jobs.*Args counterpart. River pairs a producer with
// a worker by Kind()
// string and then deserializes river_job.args into the WORKER's args type — so if a mirror's
// JSON tags ever drift from the real type's, nothing catches it at compile time. It surfaces in
// production, as a worker reading a zero value for a field the producer thought it had set.
//
// Neither side can see the other's types, so both sides pin against THIS table instead:
//
//   - internal/queue/jobs/job_arg_shapes_pin_test.go reflects over the real jobs.*Args types.
//   - internal/services/job_arg_shapes_pin_test.go reflects over the unexported mirrors.
//
// A drift on either side reddens one of those two tests. This package is the natural home
// because it is the ONLY package both of them already import, and because it already owns the
// "a job kind is just a string" abstraction (see RawJobArgs.Kind).
type JobArgsShape struct {
	// Fields is the complete, exact set of JSON field names on the canonical
	// internal/queue/jobs args type for this kind — the shape the WORKER deserializes into.
	Fields []string

	// OmitEmpty is the subset of Fields tagged `omitempty`. It is DECLARED here rather than
	// derived from either struct, and that is the whole reason it exists: a test that reads a
	// type's omitempty tags and then checks that type's marshalled output against them is
	// tautological — it passes no matter which way the tag moves. Both pin tests compare their
	// side's real omitempty set against THIS list, so adding or removing an omitempty on either
	// the canonical type or its mirror fails.
	//
	// It is load-bearing, not cosmetic: omitempty decides whether a producer that leaves a field
	// unset writes `{"a":1,"b":""}` or `{"a":1}` into river_job.args. Two producers of one kind
	// disagreeing on that means one of them cannot be deduplicated against the other by
	// UniqueOpts{ByArgs: true}, which keys on the marshalled args.
	OmitEmpty []string

	// MirrorMayOmit lists fields present on the canonical type that the internal/services
	// mirror deliberately does NOT carry. Every field named here must also appear in OmitEmpty
	// (both pin tests enforce that): only then does the mirror's marshalled JSON stay
	// byte-identical to what the canonical type would produce with the field left at its zero
	// value, which is what makes the omission safe rather than a silent shape change.
	MirrorMayOmit []string
}

// MirroredJobArgsShapes is keyed by River job KIND STRING — not by Go type name. That
// distinction is the whole point (#1486): a type-name grep for producers of a kind silently
// misses the internal/services mirrors, and misses queue.RawJobArgs entirely, because
// RawJobArgs.Kind() returns a stored string rather than a constant.
//
// Only the seven mirrored kinds appear here. Kinds with no services-side mirror (account_deletion,
// item_cleanup_check, subscription_webhook, …) have exactly one Go type and cannot drift, so
// there is nothing for a cross-package pin to compare.
var MirroredJobArgsShapes = map[string]JobArgsShape{
	"plaid_sync": {
		Fields:    []string{"user_id", "item_id", "webhook_id"},
		OmitEmpty: []string{"webhook_id"},
	},
	"investment_holdings_sync": {
		Fields:    []string{"user_id", "item_id", "webhook_id"},
		OmitEmpty: []string{"webhook_id"},
		// The sole producer is accountService.enqueueInitialSync, which runs after Plaid Link
		// and has no webhook to attribute the sync to. No worker for this kind reads
		// WebhookID at all.
		MirrorMayOmit: []string{"webhook_id"},
	},
	"investment_transactions_sync": {
		Fields:        []string{"user_id", "item_id", "webhook_id"},
		OmitEmpty:     []string{"webhook_id"},
		MirrorMayOmit: []string{"webhook_id"},
	},
	"item_cleanup": {
		Fields: []string{"linked_item_id", "user_id", "reason"},
	},
	"effective_category_refresh": {
		Fields: []string{"user_id", "custom_category_id"},
	},
	"budget_hierarchy_resync": {
		Fields: []string{
			"user_id", "custom_category_id",
			"old_parent_custom_category_id", "old_parent_pfc_primary",
		},
		OmitEmpty: []string{"old_parent_custom_category_id", "old_parent_pfc_primary"},
	},
	"email_change_propagation": {
		Fields: []string{"user_id", "clerk_user_id"},
	},
}

// JSONFieldNames reports the JSON field names of args' struct type, in declaration order, plus
// the subset tagged `omitempty`. Fields tagged `json:"-"` are skipped; an untagged exported field
// is reported under its Go name, which is what encoding/json would actually emit.
//
// Both pin tests call THIS function rather than each reflecting over struct tags themselves. That
// is deliberate: the two sides must agree on what "the field set" means, and this branch has
// already shipped three different answers to one enumeration question because three people used
// three different definitions.
func JSONFieldNames(args any) (names []string, omitEmpty map[string]bool) {
	t := reflect.TypeOf(args)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	omitEmpty = map[string]bool{}
	if t.Kind() != reflect.Struct {
		return nil, omitEmpty
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" {
			continue // unexported: encoding/json ignores it
		}
		name, opts, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "-" && opts == "" {
			continue
		}
		if name == "" {
			name = f.Name
		}
		names = append(names, name)
		for _, o := range strings.Split(opts, ",") {
			if o == "omitempty" {
				omitEmpty[name] = true
			}
		}
	}
	return names, omitEmpty
}
