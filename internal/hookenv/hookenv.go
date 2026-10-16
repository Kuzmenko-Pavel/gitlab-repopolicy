// Package hookenv parses the pre-receive environment and stdin that Gitaly passes
// to custom hooks. Push options are never read.
package hookenv

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
)

// MaxStdin bounds pre-receive stdin; about 100 000 ref updates.
const MaxStdin = 16 << 20

// Env is the part of the hook environment the binary uses.
type Env struct {
	Principal   module.Principal
	Protocol    string
	Repository  string
	ProjectPath string
	// ProjectID is set when Repository is project-<id>.
	ProjectID int64
	// Git holds the variables passed to child git processes.
	Git GitEnv
}

// GitEnv is the git part of the Gitaly hook environment.
type GitEnv struct {
	Dir                 string
	ObjectDirectory     string
	AlternateObjectDirs string
	Path                string
}

// IsProject reports whether the repository is a project repository; wiki,
// design and snippet repositories are not.
func (e Env) IsProject() bool { return e.ProjectID > 0 }

// ParseEnv reads the hook environment from KEY=VALUE pairs.
func ParseEnv(environ []string) (Env, error) {
	vars := make(map[string]string, len(environ))
	for _, kv := range environ {
		if k, v, ok := strings.Cut(kv, "="); ok {
			vars[k] = v
		}
	}
	var missing []string
	for _, k := range []string{"GL_ID", "GL_USERNAME", "GL_PROTOCOL", "GL_REPOSITORY", "GL_PROJECT_PATH", "GIT_DIR"} {
		if vars[k] == "" {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return Env{}, fmt.Errorf("hook environment: %s not set; the hook runs only under Gitaly", strings.Join(missing, ", "))
	}
	e := Env{
		Principal:   module.Principal{GLID: vars["GL_ID"], Username: vars["GL_USERNAME"]},
		Protocol:    vars["GL_PROTOCOL"],
		Repository:  vars["GL_REPOSITORY"],
		ProjectPath: vars["GL_PROJECT_PATH"],
		Git: GitEnv{
			Dir:                 vars["GIT_DIR"],
			ObjectDirectory:     vars["GIT_OBJECT_DIRECTORY"],
			AlternateObjectDirs: vars["GIT_ALTERNATE_OBJECT_DIRECTORIES"],
			Path:                vars["PATH"],
		},
	}
	if rest, ok := strings.CutPrefix(e.Repository, "project-"); ok {
		id, err := strconv.ParseInt(rest, 10, 64)
		if err != nil || id <= 0 || strconv.FormatInt(id, 10) != rest {
			return Env{}, fmt.Errorf("hook environment: GL_REPOSITORY %q is not project-<id>", e.Repository)
		}
		e.ProjectID = id
	}
	return e, nil
}

// ParseUpdates reads pre-receive stdin: one "<old> <new> <ref>" line per ref.
func ParseUpdates(r io.Reader) ([]module.RefUpdate, error) {
	sc := bufio.NewScanner(io.LimitReader(r, MaxStdin+1))
	sc.Buffer(make([]byte, 0, 4096), 64<<10)
	var updates []module.RefUpdate
	seen := map[string]bool{}
	read := 0
	for n := 1; sc.Scan(); n++ {
		read += len(sc.Bytes()) + 1
		if read > MaxStdin {
			return nil, fmt.Errorf("stdin: more than %d bytes", MaxStdin)
		}
		u, err := parseLine(sc.Text())
		if err != nil {
			return nil, fmt.Errorf("stdin line %d: %w", n, err)
		}
		if seen[u.Ref] {
			return nil, fmt.Errorf("stdin line %d: ref %s repeated", n, u.Ref)
		}
		seen[u.Ref] = true
		updates = append(updates, u)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("stdin: %w", err)
	}
	if len(updates) == 0 {
		return nil, errors.New("stdin: no ref updates")
	}
	return updates, nil
}

func parseLine(line string) (module.RefUpdate, error) {
	f := strings.Split(line, " ")
	if len(f) != 3 {
		return module.RefUpdate{}, errors.New(`expected "<old> <new> <ref>"`)
	}
	u := module.RefUpdate{Old: f[0], New: f[1], Ref: f[2]}
	if !isOID(u.Old) || !isOID(u.New) || len(u.Old) != len(u.New) {
		return module.RefUpdate{}, errors.New("revisions must be 40 or 64 lowercase hex digits of the same length")
	}
	if module.IsZero(u.Old) && module.IsZero(u.New) {
		return module.RefUpdate{}, errors.New("both revisions are null")
	}
	if !strings.HasPrefix(u.Ref, "refs/") || strings.ContainsAny(u.Ref, "\x00\t\r") {
		return module.RefUpdate{}, fmt.Errorf("invalid ref %q", u.Ref)
	}
	return u, nil
}

func isOID(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
