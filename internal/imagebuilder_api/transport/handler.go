package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	api "github.com/flightctl/flightctl/api/v1beta1/imagebuilder"
	"github.com/flightctl/flightctl/internal/imagebuilder_api/api/server"
	"github.com/flightctl/flightctl/internal/imagebuilder_api/service"
	"github.com/flightctl/flightctl/internal/store"
	"github.com/flightctl/flightctl/internal/util"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/sirupsen/logrus"
)

// TransportHandler implements the generated ServerInterface for ImageBuilder API
type TransportHandler struct {
	service service.ImageBuildService
	log     logrus.FieldLogger
}

// Make sure we conform to ServerInterface
var _ server.ServerInterface = (*TransportHandler)(nil)

// NewTransportHandler creates a new TransportHandler
func NewTransportHandler(svc service.ImageBuildService, log logrus.FieldLogger) *TransportHandler {
	return &TransportHandler{
		service: svc,
		log:     log,
	}
}

// OrgIDFromContext extracts the organization ID from the context.
// Falls back to the default organization ID if not present.
func OrgIDFromContext(ctx context.Context) uuid.UUID {
	if orgID, ok := util.GetOrgIdFromContext(ctx); ok {
		return orgID
	}
	return store.NullOrgId
}

// ListImageBuilds handles GET /api/v1/imagebuilds
func (h *TransportHandler) ListImageBuilds(w http.ResponseWriter, r *http.Request, params api.ListImageBuildsParams) {
	body, status := h.service.List(r.Context(), OrgIDFromContext(r.Context()), params)
	SetResponse(w, body, status)
}

// CreateImageBuild handles POST /api/v1/imagebuilds
func (h *TransportHandler) CreateImageBuild(w http.ResponseWriter, r *http.Request) {
	var imageBuild api.ImageBuild
	if err := json.NewDecoder(r.Body).Decode(&imageBuild); err != nil {
		SetParseFailureResponse(w, err)
		return
	}

	body, status := h.service.Create(r.Context(), OrgIDFromContext(r.Context()), imageBuild)
	SetResponse(w, body, status)
}

// ReadImageBuild handles GET /api/v1/imagebuilds/{name}
func (h *TransportHandler) ReadImageBuild(w http.ResponseWriter, r *http.Request, name string) {
	body, status := h.service.Get(r.Context(), OrgIDFromContext(r.Context()), name)
	SetResponse(w, body, status)
}

// ReplaceImageBuild handles PUT /api/v1/imagebuilds/{name}
func (h *TransportHandler) ReplaceImageBuild(w http.ResponseWriter, r *http.Request, name string) {
	var imageBuild api.ImageBuild
	if err := json.NewDecoder(r.Body).Decode(&imageBuild); err != nil {
		SetParseFailureResponse(w, err)
		return
	}

	body, status := h.service.Update(r.Context(), OrgIDFromContext(r.Context()), name, imageBuild)
	SetResponse(w, body, status)
}

// DeleteImageBuild handles DELETE /api/v1/imagebuilds/{name}
func (h *TransportHandler) DeleteImageBuild(w http.ResponseWriter, r *http.Request, name string) {
	status := h.service.Delete(r.Context(), OrgIDFromContext(r.Context()), name)
	SetResponse(w, nil, status)
}

// SetResponse writes the response body and status to the response writer
func SetResponse(w http.ResponseWriter, body any, status api.Status) {
	code := int(lo.FromPtr(status.Code))

	// Never write a body for 204/304 (and generally 1xx), per RFC 7231
	if code == http.StatusNoContent || code == http.StatusNotModified || (code >= 100 && code < 200) {
		w.WriteHeader(code)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	// Encode body into a buffer first to catch encoding errors before writing the response
	var buf bytes.Buffer
	var err error

	if body != nil && code >= 200 && code < 300 {
		err = json.NewEncoder(&buf).Encode(body)
	} else {
		err = json.NewEncoder(&buf).Encode(status)
	}

	if err != nil {
		// If encoding fails, send an internal server error response
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Now that encoding is successful, write the status and response
	w.WriteHeader(code)
	_, _ = w.Write(buf.Bytes())
}

// SetParseFailureResponse writes a parse failure response
func SetParseFailureResponse(w http.ResponseWriter, err error) {
	status := api.Status{
		Code:    lo.ToPtr(int32(http.StatusBadRequest)),
		Message: lo.ToPtr(fmt.Sprintf("can't decode JSON body: %v", err)),
	}
	SetResponse(w, nil, status)
}
