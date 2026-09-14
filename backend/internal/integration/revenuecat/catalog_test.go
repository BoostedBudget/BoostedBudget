package revenuecat

import (
	"context"
	"encoding/json"
	"github.com/stretchr/testify/require"
	"net/http"
	"strings"
	"testing"
)

func TestValidateCatalogRequiresExactEntitlementsAndIsolatedProductMappings(t *testing.T) {
	for _, bad := range []string{"", "missing", "comp_product", "wrong_price", "wrong_store", "wrong_app", "one_time", "wrong_environment", "duplicate_price"} {
		t.Run(bad, func(t *testing.T) {
			expected := map[string][]string{"trial_premium": {}}
			items := []map[string]string{{"id": "trial_premium", "lookup_key": "trial_premium"}}
			for _, tier := range []string{"premium", "household_2", "household_3", "household_4", "household_5"} {
				expected["paid_"+tier] = []string{"pri_" + tier, "pri_" + tier + "_annual"}
				expected["comp_"+tier] = nil
				for _, prefix := range []string{"paid_", "comp_"} {
					key := prefix + tier
					items = append(items, map[string]string{"id": key, "lookup_key": key})
				}
			}
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/apps/app_paddle") {
					kind := "paddle"
					if bad == "wrong_store" {
						kind = "test_store"
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"id": "app_paddle", "type": kind, "paddle": map[string]bool{"paddle_is_sandbox": bad == "wrong_environment"}})
					return
				}
				if strings.HasSuffix(r.URL.Path, "/products") {
					key := strings.Split(r.URL.Path, "/")[5]
					products := []map[string]string{}
					for _, price := range expected[key] {
						if bad == "duplicate_price" {
							price = expected[key][0]
						}
						if bad == "wrong_price" {
							price = "pri_wrong"
						}
						appID, kind := "app_paddle", "subscription"
						if bad == "wrong_app" {
							appID = "different"
						}
						if bad == "one_time" {
							kind = "one_time"
						}
						products = append(products, map[string]string{"store_identifier": price, "app_id": appID, "type": kind})
					}
					if bad == "comp_product" && key == "comp_premium" {
						products = append(products, map[string]string{"store_identifier": "pri_unexpected"})
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "items": products, "next_page": nil})
					return
				}
				list := items
				if bad == "missing" {
					list = items[:len(items)-1]
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "items": list, "next_page": nil})
			})
			err := c.ValidateCatalog(context.Background(), expected, "app_paddle")
			require.Equal(t, bad != "", err != nil)
		})
	}
}
