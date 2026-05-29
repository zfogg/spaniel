package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"
)

// validate is the shared struct validator for request DTOs. It reports the
// JSON field name (not the Go field name) so client-facing errors match the
// shape the client sent.
var validate = newValidator()

func newValidator() *validator.Validate {
	v := validator.New()
	v.RegisterTagNameFunc(func(fld reflect.StructField) string {
		name, _, _ := strings.Cut(fld.Tag.Get("json"), ",")
		if name == "-" {
			return ""
		}
		return name
	})
	return v
}

// decodeAndValidate decodes the JSON request body into dst and runs struct
// validation against its `validate` tags. An empty body is treated as an
// all-zero value rather than an error, so endpoints whose fields are all
// optional still accept an empty request (preserving prior behaviour). On
// malformed JSON or a failed constraint it writes a 400 (with request ID via
// respondErr) and returns false, so handlers can:
//
//	if !decodeAndValidate(w, req, &body) { return }
func decodeAndValidate(w http.ResponseWriter, req *http.Request, dst any) bool {
	if err := json.NewDecoder(req.Body).Decode(dst); err != nil && !errors.Is(err, io.EOF) {
		respondErr(w, req, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return false
	}
	if err := validate.Struct(dst); err != nil {
		respondErr(w, req, http.StatusBadRequest, validationMessage(err))
		return false
	}
	return true
}

// validationMessage renders the first validation failure into a concise,
// client-facing message keyed by JSON field name.
func validationMessage(err error) string {
	var verrs validator.ValidationErrors
	if errors.As(err, &verrs) && len(verrs) > 0 {
		e := verrs[0]
		switch e.Tag() {
		case "max":
			return fmt.Sprintf("field %q must be at most %s characters", e.Field(), e.Param())
		case "min":
			return fmt.Sprintf("field %q must be at least %s characters", e.Field(), e.Param())
		case "required":
			return fmt.Sprintf("field %q is required", e.Field())
		case "oneof":
			return fmt.Sprintf("field %q must be one of: %s", e.Field(), e.Param())
		default:
			return fmt.Sprintf("field %q failed %q validation", e.Field(), e.Tag())
		}
	}
	return "validation failed: " + err.Error()
}
