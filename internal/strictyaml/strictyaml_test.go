package strictyaml

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type doc struct {
	Version int      `yaml:"version"`
	Name    string   `yaml:"name"`
	Items   []string `yaml:"items"`
	Nested  struct {
		Value string `yaml:"value"`
	} `yaml:"nested"`
	Any map[string]any `yaml:"any"`
}

func TestDecode(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		max     int
		wantErr string
	}{
		{name: "valid", in: "version: 1\nname: a\nitems: [x, y]\nnested:\n  value: v\n"},
		{name: "comment only", in: "# nothing\n", wantErr: "document is empty"},
		{name: "empty", in: "", wantErr: "document is empty"},
		{name: "null document", in: "~\n", wantErr: "document is empty"},
		{name: "unknown key", in: "version: 1\nextra: 2\n", wantErr: "field extra not found"},
		{name: "unknown nested key", in: "nested:\n  other: 1\n", wantErr: "field other not found"},
		{name: "duplicate key", in: "name: a\nname: b\n", wantErr: `line 2: duplicate key "name"`},
		{name: "anchor", in: "name: &a x\nitems: [y]\n", wantErr: "line 1: anchors are not allowed"},
		{name: "alias", in: "any:\n  a: &x 1\n  b: *x\n", wantErr: "anchors are not allowed"},
		{name: "merge key", in: "any:\n  <<: {a: 1}\n", wantErr: "line 2: merge keys are not allowed"},
		{name: "explicit tag", in: "name: !!str 1\n", wantErr: "explicit tag !!str is not allowed"},
		{name: "custom tag", in: "name: !secret x\n", wantErr: "explicit tag !secret is not allowed"},
		{name: "two documents", in: "name: a\n---\nname: b\n", wantErr: "line 2: only one YAML document is allowed"},
		{name: "syntax error", in: "name: [a\n", wantErr: "did not find expected"},
		{name: "wrong type", in: "version: abc\n", wantErr: "cannot unmarshal"},
		{name: "too large", in: "name: abcdef\n", max: 5, wantErr: "document is 13 bytes, the limit is 5"},
		{name: "complex key", in: "any:\n  ? [a]\n  : 1\n", wantErr: "keys must be scalars"},
		{name: "too deep", in: "any: " + strings.Repeat("[", 40) + strings.Repeat("]", 40) + "\n", wantErr: "nesting deeper than 32 levels"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			maxBytes := tt.max
			if maxBytes == 0 {
				maxBytes = 64 << 10
			}
			var out doc
			err := Decode([]byte(tt.in), &out, maxBytes)
			if tt.wantErr == "" {
				require.NoError(t, err)
				assert.Equal(t, 1, out.Version)
				assert.Equal(t, []string{"x", "y"}, out.Items)
				assert.Equal(t, "v", out.Nested.Value)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func FuzzDecode(f *testing.F) {
	for _, s := range []string{
		"version: 1\nname: a\n",
		"items: [a, b]\nnested: {value: x}\n",
		"any:\n  a: &x 1\n  b: *x\n",
		"name: a\n---\nname: b\n",
		"any: {<<: {a: 1}}\n",
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var out doc
		err := Decode(data, &out, 4096)
		if err == nil && len(data) > 4096 {
			t.Fatalf("accepted %d bytes over the limit", len(data))
		}
	})
}

func TestDecodeValue(t *testing.T) {
	var out doc
	require.NoError(t, DecodeValue(map[string]any{"version": 1, "items": []any{"x", "y"}, "nested": map[string]any{"value": "v"}}, &out, 1024))
	assert.Equal(t, []string{"x", "y"}, out.Items)
	assert.ErrorContains(t, DecodeValue(map[string]any{"bogus": 1}, &out, 1024), "field bogus not found")
	assert.ErrorContains(t, DecodeValue(map[string]any{"name": strings.Repeat("x", 100)}, &out, 10), "the limit is 10")
}
