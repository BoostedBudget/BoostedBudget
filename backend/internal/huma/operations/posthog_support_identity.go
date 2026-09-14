package operations

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
)

// PostHogSupportIdentityHandler signs the authenticated Clerk subject for
// PostHog Support. The secret token never crosses the API boundary.
type PostHogSupportIdentityHandler struct {
	projectToken string
	secretToken  string
	authority    PostHogSupportAuthorityReader
}

type PostHogSupportAuthorityReader interface {
	CanIssueSupportIdentity(context.Context, string, string) (bool, error)
}

type GetPostHogSupportIdentityInput struct {
	inputs.AuthenticatedInput
}

type PostHogSupportIdentity struct {
	DistinctID   string `json:"distinct_id" doc:"Canonical authenticated Clerk user ID"`
	IdentityHash string `json:"identity_hash" doc:"Server-computed PostHog identity signature; valid until secret rotation"`
}

type GetPostHogSupportIdentityOutput struct {
	CacheControl string `header:"Cache-Control"`
	Body         PostHogSupportIdentity
}

func NewPostHogSupportIdentityHandler(projectToken, secretToken string, authority ...PostHogSupportAuthorityReader) *PostHogSupportIdentityHandler {
	var reader PostHogSupportAuthorityReader
	if len(authority) > 0 {
		reader = authority[0]
	}
	return &PostHogSupportIdentityHandler{projectToken: projectToken, secretToken: secretToken, authority: reader}
}

func computePostHogIdentityHash(distinctID, secretToken string) string {
	hash := hmac.New(sha256.New, []byte(secretToken))
	_, _ = hash.Write([]byte(distinctID))
	return hex.EncodeToString(hash.Sum(nil))
}

func (h *PostHogSupportIdentityHandler) Get(ctx context.Context, input *GetPostHogSupportIdentityInput) (*GetPostHogSupportIdentityOutput, error) {
	if h.projectToken == "" || h.secretToken == "" {
		return nil, huma.Error503ServiceUnavailable("Support is unavailable")
	}
	clerkUserID, ok := ctx.Value(inputs.ClerkUserIDKey).(string)
	if !ok || clerkUserID == "" {
		return nil, huma.Error401Unauthorized("User not authenticated")
	}
	if h.authority == nil {
		return nil, huma.Error503ServiceUnavailable("Support is unavailable")
	}
	allowed, err := h.authority.CanIssueSupportIdentity(ctx, input.UserID, clerkUserID)
	if err != nil || !allowed {
		return nil, huma.Error503ServiceUnavailable("Support is unavailable")
	}
	return &GetPostHogSupportIdentityOutput{
		CacheControl: "no-store",
		Body: PostHogSupportIdentity{
			DistinctID:   clerkUserID,
			IdentityHash: computePostHogIdentityHash(clerkUserID, h.secretToken),
		},
	}, nil
}

func RegisterPostHogSupportIdentityRoutes(api huma.API, handler *PostHogSupportIdentityHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "getPostHogSupportIdentity",
		Method:      http.MethodGet,
		Path:        "/api/v1/user/posthog-support-identity",
		Summary:     "Get a verified PostHog Support identity",
		Description: "Returns the authenticated Clerk subject and its server-generated HMAC for PostHog Support.",
		Tags:        []string{"Support"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.Get)
}
