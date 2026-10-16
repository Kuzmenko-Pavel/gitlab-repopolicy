package schema

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/spec"
)

var update = flag.Bool("update", false, "rewrite schemas/ and golden files")

// TestSchemas keeps schemas/ equal to the generator: make code-generate runs
// it with -update, make code-lint without.
func TestSchemas(t *testing.T) {
	dir := filepath.Join("..", "..", "schemas")
	want, err := Files()
	require.NoError(t, err)
	existing, err := filepath.Glob(filepath.Join(dir, "*.schema.json"))
	require.NoError(t, err)
	if *update {
		for _, p := range existing {
			if _, ok := want[filepath.Base(p)]; !ok {
				require.NoError(t, os.Remove(p))
			}
		}
		if len(want) > 0 {
			require.NoError(t, os.MkdirAll(dir, 0o755))
		}
		for name, doc := range want {
			require.NoError(t, os.WriteFile(filepath.Join(dir, name), doc, 0o644))
		}
		return
	}
	for _, p := range existing {
		_, ok := want[filepath.Base(p)]
		assert.True(t, ok, "%s has no module; run make code-generate", p)
	}
	for name, doc := range want {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if !assert.NoError(t, err, "run make code-generate") {
			continue
		}
		assert.True(t, bytes.Equal(doc, got), "schemas/%s differs from the generator; run make code-generate", name)
	}
}

var sample = &spec.Spec{Type: spec.Object, Fields: []spec.Field{
	{Name: "version", Required: true, Spec: &spec.Spec{Type: spec.Integer, Const: 1, Desc: "Schema version of the file."}},
	{Name: "rules", Required: true, Spec: &spec.Spec{Type: spec.Array, MaxItems: 2, Named: "rule", Items: &spec.Spec{Type: spec.Object, Fields: []spec.Field{
		{Name: "name", Required: true, Spec: spec.RuleName()},
		{Name: "level", Spec: &spec.Spec{Type: spec.String, Enum: []string{"error", "warn", "off"}}},
		{Name: "paths", Spec: &spec.Spec{Type: spec.Array, MinItems: 1, Unique: true, Items: &spec.Spec{Type: spec.String, MaxLength: 256, Check: "path-pattern"}}},
		{Name: "enabled", Spec: &spec.Spec{Type: spec.Boolean}},
		{Name: "weight", Spec: &spec.Spec{Type: spec.Integer, Minimum: 1, Maximum: 10}},
	}}}},
	{Name: "labels", Spec: &spec.Spec{Type: spec.Dict, MinItems: 1, KeyPattern: "^[a-z]+$", Values: &spec.Spec{Type: spec.String, MinLength: 1}}},
	{Name: "refs", Spec: spec.Refs("Where the rule applies.")},
}}

func TestGolden(t *testing.T) {
	doc, err := Document("SAMPLEPOLICY", sample)
	require.NoError(t, err)
	path := filepath.Join("testdata", "sample.schema.json")
	if *update {
		require.NoError(t, os.WriteFile(path, doc, 0o600))
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, string(want), string(doc))
}
