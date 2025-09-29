package transport

import (
	"encoding/json"
	"net/http"

	api "github.com/flightctl/flightctl/api/v1alpha1"
)

// (POST /api/v1/oidcproviders)
func (h *TransportHandler) CreateOIDCProvider(w http.ResponseWriter, r *http.Request) {
	var oidcProvider api.OIDCProvider
	if err := json.NewDecoder(r.Body).Decode(&oidcProvider); err != nil {
		SetParseFailureResponse(w, err)
		return
	}

	body, status := h.serviceHandler.CreateOIDCProvider(r.Context(), oidcProvider)
	SetResponse(w, body, status)
}

// (GET /api/v1/oidcproviders)
func (h *TransportHandler) ListOIDCProviders(w http.ResponseWriter, r *http.Request, params api.ListOIDCProvidersParams) {
	body, status := h.serviceHandler.ListOIDCProviders(r.Context(), params)
	SetResponse(w, body, status)
}

// (GET /api/v1/oidcproviders/{name})
func (h *TransportHandler) GetOIDCProvider(w http.ResponseWriter, r *http.Request, name string) {
	body, status := h.serviceHandler.GetOIDCProvider(r.Context(), name)
	SetResponse(w, body, status)
}

// (PUT /api/v1/oidcproviders/{name})
func (h *TransportHandler) ReplaceOIDCProvider(w http.ResponseWriter, r *http.Request, name string) {
	var oidcProvider api.OIDCProvider
	if err := json.NewDecoder(r.Body).Decode(&oidcProvider); err != nil {
		SetParseFailureResponse(w, err)
		return
	}

	body, status := h.serviceHandler.ReplaceOIDCProvider(r.Context(), name, oidcProvider)
	SetResponse(w, body, status)
}

// (DELETE /api/v1/oidcproviders/{name})
func (h *TransportHandler) DeleteOIDCProvider(w http.ResponseWriter, r *http.Request, name string) {
	status := h.serviceHandler.DeleteOIDCProvider(r.Context(), name)
	SetResponse(w, nil, status)
}
