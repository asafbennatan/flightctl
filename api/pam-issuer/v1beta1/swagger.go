package pam_issuer

import (
	"github.com/getkin/kin-openapi/openapi3"
)

// GetSwagger loads the embedded OpenAPI document for kin-openapi based validation and routing.
func GetSwagger() (*openapi3.T, error) {
	raw, err := GetOpenAPISpecJSON()
	if err != nil {
		return nil, err
	}
	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true
	return loader.LoadFromData(raw)
}
