package mock

import (
	_ "embed"
	"fmt"

	"github.com/pb33f/libopenapi"
	libvalidator "github.com/pb33f/libopenapi-validator"
	"github.com/pb33f/libopenapi-validator/requests"
	"github.com/pb33f/libopenapi-validator/responses"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
)

// publicAPISpec is a vendored snapshot of vodopad's docs/fluence-public.yaml
// (the api.fluence.dev OpenAPI 3.1 spec). Refresh with `make openapi-refresh`.
//
//go:embed testdata/fluence-public.yaml
var publicAPISpec []byte

// PublicAPISpec returns the vendored public OpenAPI spec bytes.
func PublicAPISpec() []byte { return publicAPISpec }

// buildPublicAPIModel builds the v3 model from the vendored spec. The returned
// warnings are non-fatal model-build issues (currently vodopad's public spec
// carries a dangling $ref on an unrelated billing endpoint); the model is still
// built for every endpoint that resolves. A nil model with an error means the
// vendored spec itself is unparseable — a broken vendor.
func buildPublicAPIModel() (*v3.Document, []error, error) {
	doc, err := libopenapi.NewDocument(publicAPISpec)
	if err != nil {
		return nil, nil, fmt.Errorf("parse vendored OpenAPI spec: %w", err)
	}
	model, buildErr := doc.BuildV3Model()
	if model == nil {
		return nil, nil, fmt.Errorf("vendored OpenAPI spec produced no model: %w", buildErr)
	}
	var warnings []error
	if buildErr != nil {
		warnings = []error{buildErr}
	}
	return &model.Model, warnings, nil
}

// NewPublicAPIValidator builds a full request/response validator from the
// vendored spec — used by the schema-enforcing mock middleware.
func NewPublicAPIValidator() (libvalidator.Validator, []error, error) {
	model, warnings, err := buildPublicAPIModel()
	if err != nil {
		return nil, warnings, err
	}
	return libvalidator.NewValidatorFromV3Model(model), warnings, nil
}

// NewPublicAPIRequestBodyValidator builds a request-body-only validator — used
// by the standalone contract suite to assert request shapes against the spec
// without security/parameter noise.
func NewPublicAPIRequestBodyValidator() (requests.RequestBodyValidator, []error, error) {
	model, warnings, err := buildPublicAPIModel()
	if err != nil {
		return nil, warnings, err
	}
	return requests.NewRequestBodyValidator(model), warnings, nil
}

// NewPublicAPIResponseBodyValidator builds a response-body-only validator — used
// by the mock middleware to confirm the mock's own responses match the spec, so
// the read path can't silently drift from the real API.
func NewPublicAPIResponseBodyValidator() (responses.ResponseBodyValidator, []error, error) {
	model, warnings, err := buildPublicAPIModel()
	if err != nil {
		return nil, warnings, err
	}
	return responses.NewResponseBodyValidator(model), warnings, nil
}
