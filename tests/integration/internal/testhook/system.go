package main

import (
	"errors"
	"fmt"
	"os"
)

// testSystem lets the integration tests run root commands as an ordinary
// user: the test user is root and the trusted owner, and chown is recorded
// in REPOPOLICY_TEST_CHOWN_LOG instead of being done.
type testSystem struct{}

func (testSystem) IsRoot() bool { return os.Getenv("REPOPOLICY_TEST_NOT_ROOT") == "" }

func (testSystem) LookupUser(name string) (int, int, error) {
	if name == "no-such-user" {
		return 0, 0, errors.New("unknown user " + name)
	}
	return os.Getuid(), os.Getgid(), nil
}

func (testSystem) CurrentUser() string { return "root" }

func (testSystem) Chown(path string, uid, gid int) error {
	log := os.Getenv("REPOPOLICY_TEST_CHOWN_LOG")
	if log == "" {
		return nil
	}
	f, err := os.OpenFile(log, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600) //nolint:gosec // G304: test log path
	if err != nil {
		return err
	}
	_, werr := fmt.Fprintf(f, "%s %d:%d\n", path, uid, gid)
	return errors.Join(werr, f.Close())
}

func (testSystem) TrustedUID() int { return os.Getuid() }
