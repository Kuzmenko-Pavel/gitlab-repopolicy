package main

import "testing"

func TestNotes(t *testing.T) {
	got := notes("1.1.0", []commit{
		{"a1", "feat(cli): add explain", ""},
		{"b2", "fix: keep the token", ""},
		{"c3", "feat!: drop version 0", ""},
		{"d4", "refactor(core): one root", "BREAKING CHANGE: Deps.Root\n"},
		{"e5", "build(deps): bump yaml", ""},
		{"f6", "perf(gitx): one ls-tree", ""},
		{"g7", "docs: install", ""},
	})
	want := `gitlab-repopolicy 1.1.0

Breaking changes

- feat!: drop version 0 (c3)
- refactor(core): one root (d4)

Features

- feat(cli): add explain (a1)
- feat!: drop version 0 (c3)

Fixes

- fix: keep the token (b2)

Performance

- perf(gitx): one ls-tree (f6)

Dependencies

- build(deps): bump yaml (e5)

Other changes

- refactor(core): one root (d4)
- docs: install (g7)
`
	if got != want {
		t.Fatalf("notes:\n%s\nwant:\n%s", got, want)
	}
	if got := notes("1.0.0", nil); got != "gitlab-repopolicy 1.0.0\n" {
		t.Fatalf("empty notes: %q", got)
	}
}

func TestParseLog(t *testing.T) {
	got := parseLog("a1\x00feat: x\x00feat: x\n\nbody\n\x1e\nb2\x00fix: y\x00fix: y\n\x1e\n")
	if len(got) != 2 || got[0].hash != "a1" || got[0].body != "feat: x\n\nbody\n" || got[1].subject != "fix: y" {
		t.Fatalf("parseLog: %+v", got)
	}
}

func TestPreviousMinor(t *testing.T) {
	tags := []string{"v1.0.0", "v1.0.1", "v1.1.0-rc.1", "v1.1.0", "v1.2.0", "v0.9.9", "v1.0.10", "vx", "v01.0.0"}
	for version, want := range map[string]string{
		"1.2.0":      "1.1.0",
		"1.2.0-rc.1": "1.1.0",
		"1.1.3":      "1.0.10",
		"1.0.0":      "0.9.9",
		"0.9.0":      "",
		"2.0.0":      "1.2.0",
	} {
		got, err := previousMinor(version, tags)
		if err != nil || got != want {
			t.Errorf("previousMinor(%s) = %q, %v; want %q", version, got, err, want)
		}
	}
	if _, err := previousMinor("1.2", tags); err == nil {
		t.Error("a version that is not X.Y.Z is an error")
	}
}
