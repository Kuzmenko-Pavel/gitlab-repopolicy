//go:build integration

package integration

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Benchmarks of the hook limits: the hook process is run the way Gitaly
// runs it, on synthetic repositories built once per test binary with
// git fast-import. They use the product binary without -cover.

var (
	benchOnce sync.Once
	benchBin  string
	benchErr  error
)

// productBinary builds cmd/gitlab-repopolicy once.
func productBinary(b *testing.B) string {
	b.Helper()
	benchOnce.Do(func() {
		dir, err := os.MkdirTemp(workDir, "bench-bin")
		if err != nil {
			benchErr = err
			return
		}
		benchBin = filepath.Join(dir, "gitlab-repopolicy")
		cmd := exec.CommandContext(context.Background(), "go", "build", "-o", benchBin, "../../cmd/gitlab-repopolicy")
		if out, err := cmd.CombinedOutput(); err != nil {
			benchErr = fmt.Errorf("build: %v: %s", err, out)
		}
	})
	require.NoError(b, benchErr)
	return benchBin
}

// benchRepo is a bare repository with a base commit on main and a new commit
// in refs/bench/new, and the config of the hook. gitlab_url points to a socket
// that does not exist: a scenario that needs the API fails instead of passing.
type benchRepo struct {
	dir, bare, config string
	base, new         string
}

var (
	repoMu     sync.Mutex
	benchRepos = map[string]*benchRepo{}
)

// fastImport feeds a fast-import stream into a new bare repository.
func fastImport(b *testing.B, name string, stream func(w *bytes.Buffer)) *benchRepo {
	b.Helper()
	repoMu.Lock()
	defer repoMu.Unlock()
	if r, ok := benchRepos[name]; ok {
		return r
	}
	dir, err := os.MkdirTemp(workDir, "bench-"+name)
	require.NoError(b, err)
	r := &benchRepo{dir: dir, bare: filepath.Join(dir, "repo.git"), config: filepath.Join(dir, "config.yaml")}
	env := []string{"PATH=" + gitPath(), "HOME=" + dir, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "LC_ALL=C"}
	git := func(stdin []byte, args ...string) string {
		cmd := exec.CommandContext(context.Background(), gitExe(b), args...)
		cmd.Env, cmd.Dir = env, dir
		if stdin != nil {
			cmd.Stdin = bytes.NewReader(stdin)
		}
		out, err := cmd.CombinedOutput()
		require.NoError(b, err, "git %v: %s", args, out)
		return strings.TrimSpace(string(out))
	}
	git(nil, "init", "-q", "--bare", "-b", "main", r.bare)
	var w bytes.Buffer
	stream(&w)
	git(w.Bytes(), "--git-dir", r.bare, "fast-import", "--quiet")
	r.base = git(nil, "--git-dir", r.bare, "rev-parse", "refs/heads/main")
	r.new = git(nil, "--git-dir", r.bare, "rev-parse", "refs/bench/new")
	cfg := fmt.Sprintf("mode: enforce\ngitlab_url: http+unix://%s\ntoken_file: %s\naudit:\n  file: %s\n",
		strings.ReplaceAll(filepath.Join(dir, "no-api.sock"), "/", "%2F"), filepath.Join(dir, "token"), filepath.Join(dir, "audit.log"))
	require.NoError(b, os.WriteFile(filepath.Join(dir, "token"), []byte("glpat-bench\n"), 0o600))
	require.NoError(b, os.WriteFile(r.config, []byte(cfg), 0o600))
	benchRepos[name] = r
	return r
}

// commit writes one fast-import commit; files maps a path to a blob mark.
func commit(w *bytes.Buffer, ref, from, msg string, files func(w *bytes.Buffer)) {
	fmt.Fprintf(w, "commit %s\ncommitter dev <dev@example.com> 1700000000 +0000\ndata %d\n%s\n", ref, len(msg), msg)
	if from != "" {
		fmt.Fprintf(w, "from %s\n", from)
	}
	files(w)
	w.WriteString("\n")
}

func blob(w *bytes.Buffer, mark int, content string) {
	fmt.Fprintf(w, "blob\nmark :%d\ndata %d\n%s\n", mark, len(content), content)
}

func inline(w *bytes.Buffer, path, content string) {
	fmt.Fprintf(w, "M 100644 inline %s\ndata %d\n%s\n", path, len(content), content)
}

// largePathPolicy has 100 rules, about 15 KiB: the largest policy of the limits.
func largePathPolicy() string {
	var b strings.Builder
	b.WriteString("version: 1\nadmins: [alice]\nrules:\n")
	for i := range 100 {
		fmt.Fprintf(&b, "  service-%03d:\n    match: all\n    paths: [\"services/svc-%03d/deploy/**\", \"services/svc-%03d/ci/*.yml\"]\n    allow: [alice, ops-lead-%03d]\n    message: m\n", i, i, i, i)
	}
	return b.String()
}

// runHook runs the hook for main base..new and returns the duration.
func (r *benchRepo) runHook(b *testing.B, bin, repository string) time.Duration {
	cmd := exec.CommandContext(context.Background(), bin, "hook", "pre-receive", "--config", r.config)
	cmd.Env = []string{"PATH=" + gitPath(), "GIT_DIR=" + r.bare, "GL_ID=user-2", "GL_USERNAME=alice", "GL_PROTOCOL=http",
		"GL_REPOSITORY=" + repository, "GL_PROJECT_PATH=acme/platform/app"}
	cmd.Stdin = strings.NewReader(r.base + " " + r.new + " refs/heads/main\n")
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	start := time.Now()
	err := cmd.Run()
	d := time.Since(start)
	if err != nil {
		b.Fatalf("hook: %v: %s", err, out.String())
	}
	return d
}

// measure runs the hook b.N times and reports p50 and p99 in milliseconds.
func measure(b *testing.B, r *benchRepo, repository string) {
	bin := productBinary(b)
	r.runHook(b, bin, repository) // warm the page cache
	durations := make([]time.Duration, 0, b.N)
	b.ResetTimer()
	for range b.N {
		durations = append(durations, r.runHook(b, bin, repository))
	}
	b.StopTimer()
	slices.Sort(durations)
	pct := func(p float64) float64 {
		return float64(durations[int(float64(len(durations)-1)*p)].Microseconds()) / 1000
	}
	b.ReportMetric(pct(0.50), "p50-ms")
	b.ReportMetric(pct(0.99), "p99-ms")
}

// BenchmarkLargeRepo: 100k files and PATHPOLICY with 100 rules; the push
// changes one file outside the rules.
func BenchmarkLargeRepo(b *testing.B) {
	r := fastImport(b, "large-repo", func(w *bytes.Buffer) {
		blob(w, 1, "x")
		commit(w, "refs/heads/main", "", "chore: base", func(w *bytes.Buffer) {
			for i := range 100_000 {
				fmt.Fprintf(w, "M 100644 :1 data/d%03d/f%05d.txt\n", i/1000, i)
			}
			inline(w, "PATHPOLICY", largePathPolicy())
		})
		commit(w, "refs/bench/new", "refs/heads/main", "docs: readme", func(w *bytes.Buffer) { inline(w, "README.md", "changed") })
	})
	measure(b, r, "project-1")
}

// BenchmarkLargeDiff: a push that changes 50k paths, none under a rule.
func BenchmarkLargeDiff(b *testing.B) {
	r := fastImport(b, "large-diff", func(w *bytes.Buffer) {
		blob(w, 1, "x")
		blob(w, 2, "y")
		commit(w, "refs/heads/main", "", "chore: base", func(w *bytes.Buffer) {
			for i := range 50_000 {
				fmt.Fprintf(w, "M 100644 :1 data/d%03d/f%05d.txt\n", i/1000, i)
			}
			inline(w, "PATHPOLICY", largePathPolicy())
		})
		commit(w, "refs/bench/new", "refs/heads/main", "chore: regenerate", func(w *bytes.Buffer) {
			for i := range 50_000 {
				fmt.Fprintf(w, "M 100644 :2 data/d%03d/f%05d.txt\n", i/1000, i)
			}
		})
	})
	measure(b, r, "project-1")
}

// BenchmarkMR500Commits: 500 commits enter main and COMMITPOLICY of this
// repository checks every message.
func BenchmarkMR500Commits(b *testing.B) {
	policy, err := os.ReadFile("../../COMMITPOLICY")
	require.NoError(b, err)
	r := fastImport(b, "mr-500", func(w *bytes.Buffer) {
		commit(w, "refs/heads/main", "", "chore: base\n\nSigned-off-by: dev <dev@example.com>", func(w *bytes.Buffer) {
			inline(w, "COMMITPOLICY", string(policy))
		})
		from := "refs/heads/main"
		for i := range 500 {
			msg := fmt.Sprintf("feat(app): change number %d\n\nA body that explains the change.\n\nSigned-off-by: dev <dev@example.com>", i)
			commit(w, "refs/bench/new", from, msg, func(w *bytes.Buffer) { inline(w, "app.txt", fmt.Sprint(i)) })
			from = ""
		}
	})
	measure(b, r, "project-1")
}

// BenchmarkFastPath: the paths of a push without API calls.
func BenchmarkFastPath(b *testing.B) {
	noPolicy := func(w *bytes.Buffer) {
		commit(w, "refs/heads/main", "", "chore: base", func(w *bytes.Buffer) { inline(w, "README.md", "x") })
		commit(w, "refs/bench/new", "refs/heads/main", "docs: readme", func(w *bytes.Buffer) { inline(w, "README.md", "y") })
	}
	b.Run("NotProject", func(b *testing.B) { measure(b, fastImport(b, "fast-none", noPolicy), "wiki-1") })
	b.Run("NoPolicy", func(b *testing.B) { measure(b, fastImport(b, "fast-none", noPolicy), "project-1") })
	b.Run("RulesNotTouched", func(b *testing.B) {
		r := fastImport(b, "fast-untouched", func(w *bytes.Buffer) {
			commit(w, "refs/heads/main", "", "chore: base", func(w *bytes.Buffer) {
				inline(w, "README.md", "x")
				inline(w, "PATHPOLICY", largePathPolicy())
			})
			commit(w, "refs/bench/new", "refs/heads/main", "docs: readme", func(w *bytes.Buffer) { inline(w, "README.md", "y") })
		})
		measure(b, r, "project-1")
	})
}
