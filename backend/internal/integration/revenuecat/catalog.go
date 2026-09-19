package revenuecat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

// ValidateCatalog is a read-only prelaunch check against the expected Paddle
// price identifiers. Grant-only entitlements must have no products attached.
func (c *Client) ValidateCatalog(ctx context.Context, expected map[string][]string, appID string) error {
	if appID == "" {
		return errors.New("RevenueCat Paddle app ID is required")
	}
	raw, err := c.request(ctx, http.MethodGet, c.projectPath()+"/apps/"+url.PathEscape(appID), c.secretAPIKey, nil)
	if err != nil {
		return err
	}
	var app struct {
		ID     string `json:"id"`
		Type   string `json:"type"`
		Paddle struct {
			Sandbox *bool `json:"paddle_is_sandbox"`
		} `json:"paddle"`
	}
	if json.Unmarshal(raw, &app) != nil || app.ID != appID || app.Type != "paddle" || app.Paddle.Sandbox == nil || *app.Paddle.Sandbox != (c.environment == "sandbox") {
		return errors.New("RevenueCat app is not the matching Paddle environment")
	}
	catalog, err := c.entitlementCatalog(ctx, nil)
	if err != nil {
		return err
	}
	if len(catalog.lookup) != len(expected) || len(expected) == 0 {
		return errors.New("RevenueCat entitlement catalog does not match expected keys")
	}
	seen := map[string]bool{}
	for id, key := range catalog.lookup {
		prices, ok := expected[key]
		if !ok || seen[key] {
			return errors.New("RevenueCat entitlement key mismatch")
		}
		seen[key] = true
		products, err := c.list(ctx, c.projectPath()+"/entitlements/"+url.PathEscape(id)+"/products")
		if err != nil {
			return err
		}
		wanted := map[string]bool{}
		for _, price := range prices {
			if price == "" || wanted[price] {
				return errors.New("invalid expected Paddle price mapping")
			}
			wanted[price] = true
		}
		if len(products) != len(wanted) {
			return fmt.Errorf("RevenueCat product count mismatch for %s", key)
		}
		for _, raw := range products {
			var product struct {
				StoreIdentifier string `json:"store_identifier"`
				AppID           string `json:"app_id"`
				Type            string `json:"type"`
			}
			if json.Unmarshal(raw, &product) != nil || !wanted[product.StoreIdentifier] || product.AppID != appID || product.Type != "subscription" {
				return fmt.Errorf("RevenueCat product mapping mismatch for %s", key)
			}
			delete(wanted, product.StoreIdentifier)
		}
	}
	return nil
}
