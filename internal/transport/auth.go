package transport

import (
	"encoding/json"
	"net/http"
	"strings"

	api "github.com/flightctl/flightctl/api/v1alpha1"
	"github.com/flightctl/flightctl/internal/auth"
)

// (POST /api/v1/auth/token)
func (h *TransportHandler) AuthToken(w http.ResponseWriter, r *http.Request) {
	var req api.TokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		SetParseFailureResponse(w, err)
		return
	}

	// Get OIDC provider from service (this would need to be added to service interface)
	// For now, we'll assume it's available through the service handler
	// This is a placeholder - the actual implementation would depend on how
	// the OIDC provider is integrated into the service layer
	body, status := h.serviceHandler.AuthToken(r.Context(), req)
	SetResponse(w, body, status)
}

// (GET /api/v1/auth/userinfo)
func (h *TransportHandler) AuthUserInfo(w http.ResponseWriter, r *http.Request) {
	// Get access token from Authorization header
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		SetResponse(w, nil, api.StatusUnauthorized("invalid_token"))
		return
	}

	accessToken := strings.TrimPrefix(authHeader, "Bearer ")

	body, status := h.serviceHandler.AuthUserInfo(r.Context(), accessToken)
	SetResponse(w, body, status)
}

// (GET /api/v1/auth/jwks)
func (h *TransportHandler) AuthJWKS(w http.ResponseWriter, r *http.Request) {
	body, status := h.serviceHandler.AuthJWKS(r.Context())
	SetResponse(w, body, status)
}

// (GET /api/v1/auth/.well-known/openid_configuration)
func (h *TransportHandler) AuthOpenIDConfiguration(w http.ResponseWriter, r *http.Request) {
	body, status := h.serviceHandler.AuthOpenIDConfiguration(r.Context())
	SetResponse(w, body, status)
}

// (GET /api/v1/auth/authorize)
func (h *TransportHandler) AuthAuthorize(w http.ResponseWriter, r *http.Request, params api.AuthAuthorizeParams) {
	body, status := h.serviceHandler.AuthAuthorize(r.Context(), params)
	SetResponse(w, body, status)
}

// (GET /api/v1/auth/config)
func (h *TransportHandler) AuthConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if _, ok := h.authN.(auth.NilAuth); ok {
		SetResponse(w, nil, api.StatusAuthNotConfigured("Auth not configured"))
		return
	}

	authConfig := h.authN.GetAuthConfig()
	body, status := h.serviceHandler.GetAuthConfig(r.Context(), authConfig)
	SetResponse(w, body, status)
}

// (GET /api/v1/auth/validate)
func (h *TransportHandler) AuthValidate(w http.ResponseWriter, r *http.Request, params api.AuthValidateParams) {
	// auth middleware already checked the token validity
	SetResponse(w, nil, api.StatusOK())
}
