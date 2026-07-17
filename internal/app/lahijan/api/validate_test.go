package api

import (
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/require"
)

func TestValidateBody_RejectsInvalid(t *testing.T) {
	t.Parallel()

	schema := &openapi3.Schema{
		Type:     &openapi3.Types{"object"},
		Required: []string{"email"},
		Properties: openapi3.Schemas{
			"email": &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}},
		},
	}

	// Missing required field.
	require.Error(t, ValidateBody(schema, map[string]any{}))

	// Wrong type.
	require.Error(t, ValidateBody(schema, map[string]any{"email": 123}))

	// Valid.
	require.NoError(t, ValidateBody(schema, map[string]any{"email": "a@b.c"}))
}

func TestValidateBody_NilSchemaIsNoOp(t *testing.T) {
	t.Parallel()
	// A nil schema means "no constraints" -> everything passes.
	require.NoError(t, ValidateBody(nil, map[string]any{"anything": 1}))
}

func TestValidateBody_ErrorWrapsReason(t *testing.T) {
	t.Parallel()

	strType := openapi3.Types{"string"}
	schema := &openapi3.Schema{Type: &strType}

	err := ValidateBody(schema, 42)
	require.Error(t, err)
	// The error is namespaced so handlers can surface it as a bad_request detail.
	require.Contains(t, err.Error(), "body:")
}
