package audit

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", false, "rewrite golden files")

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		require.NoError(t, os.WriteFile(path, got, 0o600))
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, string(want), string(got))
}

func fixedClock() time.Time { return time.Date(2026, 10, 8, 9, 14, 2, 418_000_000, time.UTC) }

func sampleEvent() Event {
	return Event{
		Event: EventDecision, Module: "pathpolicy", Decision: "deny", Code: "protected_path_violation",
		ProjectID: 812, ProjectPath: "example/payments/deployment", UserID: 314, Username: "alice",
		GLID: "user-314", Protocol: "web", Ref: "refs/heads/main",
		OldRev: "9f1c", NewRev: "b77a", AnchorRev: "9f1c", PolicyBlob: "3e5d",
		Violations: []Violation{{Rule: "ci-config", Paths: []string{".gitlab-ci.yml"}, Reason: "not in allow: dev-1",
			Source: []string{"preset:org-ci", "file"}}},
		Presets:  []Preset{{Name: "org-ci", SHA256: "5b0d7e3a1c9f4e2b8a6d0c3f7e1b9a4d2c8f6e0b3a7d1c5f9e2b4a8d6c0f3e7b"}},
		APICalls: 2, DurationMS: 143,
	}
}

func TestGolden(t *testing.T) {
	var out bytes.Buffer
	w := NewWriter(Options{Stdout: &out, Enforced: true, Version: "1.0.0", Git: "2.55.0", Node: "gitaly-01", Clock: fixedClock})
	require.NoError(t, w.Write(sampleEvent()))
	require.NoError(t, NewWriter(Options{Stdout: &out, Version: "1.0.0", Git: "2.55.0", Node: "gitaly-01", Clock: fixedClock}).Write(sampleEvent()))
	golden(t, "decision.jsonl", out.Bytes())
}

func TestWriter_AppendsToFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	w := NewWriter(Options{File: path, Version: "1.0.0", Clock: fixedClock})
	w.SetGit("2.55.0")
	for range 3 {
		require.NoError(t, w.Write(sampleEvent()))
	}
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	require.Len(t, lines, 3)
	var e Event
	require.NoError(t, json.Unmarshal([]byte(lines[2]), &e))
	assert.False(t, e.Enforced, "mode: audit writes enforced: false")
	assert.Equal(t, "2.55.0", e.Git)
	assert.Equal(t, "1.0.0", e.Version)
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o640), info.Mode().Perm())
}

func TestWriter_FileError(t *testing.T) {
	w := NewWriter(Options{File: filepath.Join(t.TempDir(), "missing", "audit.log"), Clock: fixedClock})
	assert.Error(t, w.Write(sampleEvent()))
}

func TestEncode_Truncates(t *testing.T) {
	e := sampleEvent()
	items := make([]string, 500)
	for i := range items {
		items[i] = strings.Repeat("p", 40)
	}
	e.Violations = []Violation{{Rule: "a", Paths: items}, {Rule: "b", Commits: items}, {Rule: "c", Reason: strings.Repeat("r", 5000)}}
	line, err := Encode(e)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(line), MaxEventSize)
	var got Event
	require.NoError(t, json.Unmarshal(line, &got))
	assert.True(t, got.Truncated)
	assert.Equal(t, "pathpolicy", got.Module)

	e = sampleEvent()
	e.Violations = nil
	e.Reason = strings.Repeat("x", 10000)
	line, err = Encode(e)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(line), MaxEventSize)

	e = sampleEvent()
	e.ProjectPath = strings.Repeat("x", 10000)
	_, err = Encode(e)
	assert.Error(t, err)
}
