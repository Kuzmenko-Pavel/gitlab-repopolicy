package schema

import (
	"bytes"
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/modules"
)

var update = flag.Bool("update", false, "rewrite schemas/ and golden files")

// TestSchemas keeps schemas/ equal to the generator: make code-generate runs
// it with -update, make code-lint without.
func TestSchemas(t *testing.T) {
	dir := filepath.Join("..", "..", "schemas")
	want := map[string][]byte{}
	for _, m := range modules.Default(modules.Settings{}).All() {
		doc, ok, err := ForModule(m)
		require.NoError(t, err, m.Name())
		if ok {
			want[FileName(m)] = doc
		}
	}
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

type level string

func (level) JSONSchema() map[string]any {
	return map[string]any{"type": "string", "enum": []string{"error", "warn", "off"}}
}

type sample struct {
	Version int               `yaml:"version" schema:"required" desc:"Schema version of the file."`
	Rules   []sampleRule      `yaml:"rules" schema:"required"`
	Labels  map[string]string `yaml:"labels,omitempty"`
	Extra   any               `yaml:"extra"`
	Skip    string            `yaml:"-"`
	hidden  string
}

type sampleRule struct {
	Name    string   `yaml:"name" schema:"required"`
	Level   level    `yaml:"level"`
	Paths   []string `yaml:"paths"`
	Enabled *bool    `yaml:"enabled"`
	Weight  float64  `yaml:"weight"`
}

func TestGolden(t *testing.T) {
	doc, err := Document("SAMPLEPOLICY", sample{hidden: "x"})
	require.NoError(t, err)
	path := filepath.Join("testdata", "sample.schema.json")
	if *update {
		require.NoError(t, os.WriteFile(path, doc, 0o600))
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, string(want), string(doc))
}

type recursive struct {
	Next *recursive `yaml:"next"`
}

type noName struct {
	Field string
}

func TestDocument_Errors(t *testing.T) {
	_, err := Document("X", recursive{})
	assert.ErrorContains(t, err, "recursive type")
	_, err = Document("X", noName{})
	assert.ErrorContains(t, err, "has no yaml name")
	_, err = Document("X", map[int]string{})
	assert.ErrorContains(t, err, "map key")
	_, err = Document("X", make(chan int))
	assert.ErrorContains(t, err, "unsupported type")
}

type providing struct{}

func (providing) Name() string                        { return "samplepolicy" }
func (providing) File() string                        { return "SAMPLEPOLICY" }
func (providing) OnInvalid() module.FailMode          { return module.FailOpen }
func (providing) Parse([]byte) (module.Policy, error) { return nil, nil }
func (providing) SchemaType() any                     { return sample{} }
func (providing) MaxPolicySize() int                  { return module.DefaultMaxPolicySize }
func (providing) Needs() module.Needs                 { return module.Needs{} }
func (providing) EvaluateInvalid(context.Context, *module.Input, error) (*module.Result, error) {
	return nil, nil
}

func TestForModule(t *testing.T) {
	doc, ok, err := ForModule(providing{})
	require.NoError(t, err)
	assert.True(t, ok)
	assert.True(t, strings.HasPrefix(string(doc), "{\n"))
	assert.Equal(t, "samplepolicy.schema.json", FileName(providing{}))
}
