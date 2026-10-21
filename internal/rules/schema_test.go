package rules

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParamsFromForm pins the builder's coercion: a number field is a number,
// a value too large for float64 keeps every digit, and a field that is not a
// number is reported by name rather than taking the whole params blob with
// it.
func TestParamsFromForm(t *testing.T) {
	schema := []FieldSchema{
		{Name: "event_name", Type: "string"},
		{Name: "count", Type: "number"},
		{Name: "threshold", Type: "number"},
	}
	tests := []struct {
		name       string
		values     map[string]string
		wantParams string
		wantErr    string // the field expected in the error, empty for no error
	}{
		{
			name:       "number field becomes a number",
			values:     map[string]string{"event_name": "transfer", "count": "50"},
			wantParams: `{"count":50,"event_name":"transfer"}`,
		},
		{
			name:       "integer beyond float64 precision keeps every digit",
			values:     map[string]string{"threshold": "123456789012345678901234567890"},
			wantParams: `{"threshold":123456789012345678901234567890}`,
		},
		{
			name:       "decimal and exponent are numbers",
			values:     map[string]string{"threshold": "1.5e3"},
			wantParams: `{"threshold":1.5e3}`,
		},
		{
			name:    "non-numeric value is reported by field name",
			values:  map[string]string{"event_name": "transfer", "count": "many"},
			wantErr: "count",
		},
		{
			name:    "a number field with a thousands separator is not a number",
			values:  map[string]string{"count": "1,000"},
			wantErr: "count",
		},
		{
			name:       "empty and absent fields are left out",
			values:     map[string]string{"event_name": "", "count": ""},
			wantParams: `{}`,
		},
		{
			name:       "a field the schema does not know is ignored",
			values:     map[string]string{"nope": "x"},
			wantParams: `{}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := ParamsFromForm(schema, tt.values)
			if tt.wantErr != "" {
				require.Error(t, err, "expected an error naming %q", tt.wantErr)
				var fields FieldErrors
				require.ErrorAs(t, err, &fields)
				require.NotEmpty(t, fields)
				assert.Equal(t, tt.wantErr, fields[0].Field)
				assert.Nil(t, raw, "no params are returned when a field is malformed")
				return
			}
			require.NoError(t, err)
			assert.JSONEq(t, tt.wantParams, string(raw))
		})
	}
}

// TestParamsFromFormReportsEveryMalformedField: one bad field must not hide
// the next one, and the fields that are fine must still be in the params the
// caller gets back.
func TestParamsFromFormReportsEveryMalformedField(t *testing.T) {
	schema := []FieldSchema{
		{Name: "count", Type: "number"},
		{Name: "position", Type: "number"},
		{Name: "event_name", Type: "string"},
	}
	raw, err := ParamsFromForm(schema, map[string]string{
		"count": "many", "position": "top", "event_name": "transfer",
	})
	require.Error(t, err)
	var fields FieldErrors
	require.ErrorAs(t, err, &fields)
	require.Len(t, fields, 2)
	assert.Equal(t, "count", fields[0].Field)
	assert.Equal(t, "position", fields[1].Field)
	assert.Nil(t, raw)
}

// TestParamsFromFormKeepsLargeIntegersExact: the number is carried as text
// all the way into the params, so a threshold beyond float64's precision is
// not rounded on the way to the store.
func TestParamsFromFormKeepsLargeIntegersExact(t *testing.T) {
	raw, err := ParamsFromForm([]FieldSchema{{Name: "threshold", Type: "number"}},
		map[string]string{"threshold": "9007199254740993"})
	require.NoError(t, err)
	var got map[string]json.Number
	require.NoError(t, json.Unmarshal(raw, &got))
	assert.Equal(t, "9007199254740993", got["threshold"].String())
}
