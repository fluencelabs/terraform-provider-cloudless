package mock

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"

	validationerrors "github.com/pb33f/libopenapi-validator/errors"
	"github.com/pb33f/libopenapi-validator/requests"
	"github.com/pb33f/libopenapi-validator/responses"
)

// contractMiddleware validates incoming request bodies against the vendored
// public OpenAPI spec before dispatching, so the mock rejects exactly what the
// real API rejects. This turns the hand-written mock from a drift-prone fake
// into a contract-enforced one: every resource.UnitTest that drives the mock
// now exercises request-shape validation for free.
//
// Only genuine request-body schema violations produce a 400. Errors that mean
// "this path/operation isn't in the spec" are skipped, so mock-only or
// spec-omitted endpoints still work.
func (s *Server) contractMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.contractEnforce && s.reqBodyValidator != nil && methodHasBody(r.Method) && r.Body != nil {
			body, err := io.ReadAll(r.Body)
			_ = r.Body.Close()
			if err == nil && len(body) > 0 {
				probe := r.Clone(r.Context())
				probe.Body = io.NopCloser(bytes.NewReader(body))
				ok, valErrs := s.reqBodyValidator.ValidateRequestBody(probe)
				// Always restore the body for the downstream handler.
				r.Body = io.NopCloser(bytes.NewReader(body))
				if !ok && hasBodySchemaViolation(valErrs) {
					s.writeJSON(w, http.StatusBadRequest, map[string]string{
						"error": "Input validation error: " + joinValidationErrors(valErrs),
					})
					return
				}
			} else {
				r.Body = io.NopCloser(bytes.NewReader(body))
			}
		}
		if !s.contractEnforce || s.respBodyValidator == nil {
			next.ServeHTTP(w, r)
			return
		}
		// Capture the response so we can check the mock's own output against the
		// spec — the read path can't silently drift from the real API.
		rec := httptest.NewRecorder()
		next.ServeHTTP(rec, r)
		s.recordResponseViolations(r, rec)
		copyRecorded(w, rec)
	})
}

// recordResponseViolations validates the recorded response against the spec and
// records any genuine response-body schema drift (skipping path-not-found for
// mock-only endpoints).
func (s *Server) recordResponseViolations(r *http.Request, rec *httptest.ResponseRecorder) {
	resp := rec.Result()
	defer resp.Body.Close()
	ok, valErrs := s.respBodyValidator.ValidateResponseBody(r, resp)
	if ok || !hasBodySchemaViolation(valErrs) {
		return
	}
	msg := r.Method + " " + r.URL.Path + ": " + joinValidationErrors(valErrs)
	s.mu.Lock()
	s.contractViolations = append(s.contractViolations, msg)
	s.mu.Unlock()
}

func copyRecorded(w http.ResponseWriter, rec *httptest.ResponseRecorder) {
	for k, vs := range rec.Header() {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(rec.Code)
	_, _ = w.Write(rec.Body.Bytes())
}

func methodHasBody(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		return true
	default:
		return false
	}
}

// hasBodySchemaViolation reports whether any error is a real request-body schema
// failure (as opposed to "path/operation not found in spec", which we skip so
// mock-only endpoints keep working).
func hasBodySchemaViolation(errs []*validationerrors.ValidationError) bool {
	for _, e := range errs {
		if e.ValidationType == helperPathValidation {
			continue
		}
		return true
	}
	return false
}

// helperPathValidation is libopenapi-validator's ValidationType for
// path/operation-resolution failures.
const helperPathValidation = "path"

func joinValidationErrors(errs []*validationerrors.ValidationError) string {
	parts := make([]string, 0, len(errs))
	for _, e := range errs {
		parts = append(parts, e.Message)
	}
	return joinNonEmpty(parts, "; ")
}

func joinNonEmpty(parts []string, sep string) string {
	out := ""
	for _, p := range parts {
		if p == "" {
			continue
		}
		if out != "" {
			out += sep
		}
		out += p
	}
	return out
}

// Aliases for the validator interfaces stored on Server, for clarity at the
// field sites.
type (
	requestBodyValidator  = requests.RequestBodyValidator
	responseBodyValidator = responses.ResponseBodyValidator
)
