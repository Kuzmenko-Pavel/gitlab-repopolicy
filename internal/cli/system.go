package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
)

// System is what the commands that run as root need from the operating
// system. The integration tests replace it to run them without root.
type System interface {
	IsRoot() bool
	// LookupUser returns the uid and gid of a user name.
	LookupUser(name string) (uid, gid int, err error)
	// CurrentUser is the name of the user running the process.
	CurrentUser() string
	Chown(path string, uid, gid int) error
	// TrustedUID owns the files that only root may write, such as break-glass grants.
	TrustedUID() int
}

type osSystem struct{}

func (osSystem) IsRoot() bool { return os.Geteuid() == 0 }

func (osSystem) LookupUser(name string) (int, int, error) {
	u, err := user.Lookup(name)
	if err != nil {
		return 0, 0, err
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return 0, 0, fmt.Errorf("user %s: uid %q", name, u.Uid)
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return 0, 0, fmt.Errorf("user %s: gid %q", name, u.Gid)
	}
	return uid, gid, nil
}

func (osSystem) CurrentUser() string {
	u, err := user.Current()
	if err != nil {
		return "uid " + strconv.Itoa(os.Getuid())
	}
	return u.Username
}

func (osSystem) Chown(path string, uid, gid int) error { return os.Lchown(path, uid, gid) }

func (osSystem) TrustedUID() int { return 0 }

func (a App) system() System {
	if a.System == nil {
		return osSystem{}
	}
	return a.System
}

// errNotRoot is returned by commands that only root may run.
var errNotRoot = errors.New("this command must run as root")

// logger is the diagnostic log of commands other than the hook (design §7.4).
func logger(w io.Writer) *slog.Logger {
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

// operator is the person behind a root command: SUDO_USER under sudo.
func (a App) operator(env []string) string {
	if u := envValue(env, "SUDO_USER"); u != "" {
		return u
	}
	return a.system().CurrentUser()
}

// prepareAuditFile creates a missing audit log with the owner of its
// directory when root runs a command: a file created by root would stop the
// hook, which runs as the git user, from appending to it.
func (a App) prepareAuditFile(path string) error {
	if path == "" || !a.system().IsRoot() {
		return nil
	}
	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	dir, err := os.Stat(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("audit: %w", err)
	}
	st, ok := dir.Sys().(*syscall.Stat_t)
	if !ok {
		return errors.New("audit: cannot read the owner of the log directory")
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640) //nolint:gosec // G304: path comes from the server config
	if err != nil {
		return fmt.Errorf("audit: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("audit: %w", err)
	}
	return a.system().Chown(path, int(st.Uid), int(st.Gid))
}
