// Package validate checks decoded request payloads at the HTTP boundary
// using struct tags (github.com/go-playground/validator/v10) and turns the
// library's failures into a field-keyed 422 response.
//
// This is deliberately NOT the only validation in the server: the service
// layer (internal/users, internal/chats) keeps its own checks because
// gochactrl calls it directly and never passes through a handler. Validator
// is the outer gate that rejects malformed client input early and reports
// which field was wrong; the service layer is what actually guarantees the
// invariant. When a rule exists in both places, the numbers must agree —
// see TestTagsMatchServiceLimits.
package validate

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strings"

	"github.com/go-playground/validator/v10"
)

// v is shared: the validator caches struct reflection per type, so reusing
// one instance is both cheaper and the documented usage. It is
// goroutine-safe.
var v = newValidator()

func newValidator() *validator.Validate {
	val := validator.New(validator.WithRequiredStructEnabled())

	// Report the JSON field name, not the Go field name: a client that
	// sent `display_name` must not be told that `DisplayName` is invalid.
	val.RegisterTagNameFunc(func(fld reflect.StructField) string {
		name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
		if name == "-" {
			return ""
		}
		if name == "" {
			return fld.Name
		}
		return name
	})

	return val
}

// Errors is the typed result of a failed validation: one message per
// offending field, keyed by the JSON field name.
type Errors struct {
	Fields map[string]string
}

// Error renders every field message in one line, so callers that only have
// room for a single string (logs, the `error` key of the response) still
// get something specific.
func (e *Errors) Error() string {
	parts := make([]string, 0, len(e.Fields))
	for field, msg := range e.Fields {
		parts = append(parts, field+" "+msg)
	}
	// Map iteration is random; sort so the message is stable across calls
	// (tests compare it, and a flapping error string is a bad log).
	slices.Sort(parts)
	return strings.Join(parts, "; ")
}

// Struct validates a decoded request payload. It returns nil when the
// payload is valid, an *Errors when a rule failed, and a plain error only
// when the caller passed something the validator cannot handle (a
// programming mistake, not client input).
func Struct(payload any) error {
	err := v.Struct(payload)
	if err == nil {
		return nil
	}

	var invalid *validator.InvalidValidationError
	if errors.As(err, &invalid) {
		return err
	}

	var fieldErrs validator.ValidationErrors
	if !errors.As(err, &fieldErrs) {
		return err
	}

	fields := make(map[string]string, len(fieldErrs))
	for _, fe := range fieldErrs {
		fields[fe.Field()] = message(fe)
	}
	return &Errors{Fields: fields}
}

// message turns one failed rule into a sentence a user can act on. The
// wording continues the field name, e.g. "email" + "must be a valid ...".
func message(fe validator.FieldError) string {
	switch fe.Tag() {
	case "required":
		return "is required"
	case "email":
		return "must be a valid email address"
	case "min":
		return fmt.Sprintf("must be at least %s characters", fe.Param())
	case "max":
		return fmt.Sprintf("must be at most %s characters", fe.Param())
	case "len":
		return fmt.Sprintf("must be exactly %s characters", fe.Param())
	case "oneof":
		return fmt.Sprintf("must be one of: %s", strings.ReplaceAll(fe.Param(), " ", ", "))
	case "hexadecimal":
		return "must be hexadecimal"
	default:
		return "is invalid"
	}
}

// WriteError writes err as a 422 response and reports whether it did. A nil
// error writes nothing and returns false, so handlers can call it as a
// guard:
//
//	if validate.WriteError(w, validate.Struct(req)) {
//		return
//	}
//
// Non-validation errors are the caller's to handle: WriteError leaves them
// alone and returns false rather than passing an internal failure off as
// client error.
func WriteError(w http.ResponseWriter, err error) bool {
	var verrs *Errors
	if !errors.As(err, &verrs) {
		return false
	}

	// `error` stays a plain string because every existing client (the SPA's
	// errorMessage(), api.rest) reads that key; `fields` is additive.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnprocessableEntity)
	json.NewEncoder(w).Encode(map[string]any{
		"error":  verrs.Error(),
		"fields": verrs.Fields,
	})
	return true
}
