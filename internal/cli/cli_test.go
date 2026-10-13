package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func run(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = Run(context.Background(), args, nil, Stdio{In: strings.NewReader(""), Out: &out, Err: &errOut})
	return code, out.String(), errOut.String()
}

func TestRun(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantOut    string
		wantErrOut string
	}{
		{name: "no arguments", args: nil, wantCode: ExitUsage, wantErrOut: "Usage: gitlab-repopolicy"},
		{name: "help", args: []string{"help"}, wantCode: ExitOK, wantOut: "Usage: gitlab-repopolicy"},
		{name: "unknown command", args: []string{"frobnicate"}, wantCode: ExitUsage, wantErrOut: `unknown command "frobnicate"`},
		{name: "version", args: []string{"version"}, wantCode: ExitOK, wantOut: "gitlab-repopolicy "},
		{name: "version with arguments", args: []string{"version", "extra"}, wantCode: ExitUsage, wantErrOut: "unexpected arguments"},
		{name: "version with unknown flag", args: []string{"version", "-x"}, wantCode: ExitUsage, wantErrOut: "flag provided but not defined"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, out, errOut := run(t, tc.args...)
			assert.Equal(t, tc.wantCode, code)
			if tc.wantOut != "" {
				assert.Contains(t, out, tc.wantOut)
			}
			if tc.wantErrOut != "" {
				assert.Contains(t, errOut, tc.wantErrOut)
			}
		})
	}
}

func TestPrintVersion(t *testing.T) {
	var out bytes.Buffer
	err := printVersion(&out, Build{Version: "1.0.0-rc.1", Commit: "0123456789ab", Date: "2026-10-13T12:00:00+03:00", Go: "go1.27.2"})
	require.NoError(t, err)
	want := "gitlab-repopolicy 1.0.0-rc.1\ncommit: 0123456789ab\ndate: 2026-10-13T12:00:00+03:00\ngo: go1.27.2\n"
	require.Equal(t, want, out.String())
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestRun_OutputWriteFailure(t *testing.T) {
	stdio := Stdio{In: strings.NewReader(""), Out: failingWriter{}, Err: failingWriter{}}
	for _, args := range [][]string{{"version"}, {"help"}, nil, {"frobnicate"}, {"version", "extra"}} {
		assert.Equal(t, ExitError, Run(context.Background(), args, nil, stdio), "args %q", args)
	}
}
