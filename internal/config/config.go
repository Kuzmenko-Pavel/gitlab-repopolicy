// Package config parses and validates the server config, config.yaml.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/strictyaml"
)

// MaxSize is the size limit of config.yaml.
const MaxSize = 1 << 20

// Mode is the global switch of the hook.
type Mode string

// Modes of the hook.
const (
	ModeOff     Mode = "off"
	ModeAudit   Mode = "audit"
	ModeEnforce Mode = "enforce"
)

// Module states in the modules section.
const (
	ModuleEnabled  = "enabled"
	ModuleDisabled = "disabled"
)

// Config is the server config. Parse fills in the documented defaults.
type Config struct {
	Mode          Mode              `yaml:"mode"`
	GitLabURL     string            `yaml:"gitlab_url"`
	CAFile        string            `yaml:"ca_file"`
	TokenFile     string            `yaml:"token_file"`
	TokenLifetime Duration          `yaml:"token_lifetime"`
	Audit         Audit             `yaml:"audit"`
	BreakglassDir string            `yaml:"breakglass_dir"`
	Timeouts      Timeouts          `yaml:"timeouts"`
	MaxAPICalls   int               `yaml:"max_api_calls"`
	Modules       map[string]string `yaml:"modules"`
	// DefaultBranch lists the allowed names of the default branch, for example
	// [main, master]; the hook and settings-check use it, empty turns the check off.
	DefaultBranch []string      `yaml:"default_branch"`
	SettingsCheck SettingsCheck `yaml:"settings_check"`
	Help          Help          `yaml:"help"`
	// Presets are rules shared by projects, by module name and preset name;
	// they are kept undecoded and their modules parse them.
	Presets map[string]map[string]any `yaml:"presets"`
}

// Help is where a pusher gets help; both are optional and printed in denials.
// Contact is who to ask about a problem of the server or a broken policy, for
// example "#gitlab-support"; DocsURL is the documentation of the rules.
type Help = module.Help

// Audit configures where audit events go.
type Audit struct {
	File   string `yaml:"file"`
	Stdout bool   `yaml:"stdout"`
}

// Timeouts of a hook invocation and of its parts.
type Timeouts struct {
	Total Duration `yaml:"total"`
	HTTP  Duration `yaml:"http"`
}

// SettingsCheck holds the project settings expected by settings-check.
type SettingsCheck struct {
	ProtectedTags []string `yaml:"protected_tags"`
	JobTokenPush  *bool    `yaml:"job_token_push"`
}

// Duration is a time.Duration that also accepts whole days, for example "364d".
type Duration time.Duration

// UnmarshalText parses a Go duration or a number of days with the suffix "d".
func (d *Duration) UnmarshalText(b []byte) error {
	s := string(b)
	if days, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil || n < 0 {
			return fmt.Errorf("invalid duration %q", s)
		}
		*d = Duration(time.Duration(n) * 24 * time.Hour)
		return nil
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q", s)
	}
	*d = Duration(v)
	return nil
}

// MarshalText writes whole days with the suffix "d" and anything else as a Go duration.
func (d Duration) MarshalText() ([]byte, error) {
	const day = 24 * time.Hour
	if v := time.Duration(d); v > 0 && v%day == 0 {
		return []byte(strconv.FormatInt(int64(v/day), 10) + "d"), nil
	}
	return []byte(time.Duration(d).String()), nil
}

func (d Duration) String() string {
	b, err := d.MarshalText()
	if err != nil {
		return time.Duration(d).String()
	}
	return string(b)
}

// Std returns the value as time.Duration.
func (d Duration) Std() time.Duration { return time.Duration(d) }

// etcDir holds the token and break-glass grants of a package installation.
const etcDir = "/etc/gitlab-repopolicy"

// Defaults returns the config that Parse starts from.
func Defaults() Config {
	return Config{
		Mode:          ModeEnforce,
		TokenFile:     filepath.Join(etcDir, "token"),
		TokenLifetime: Duration(364 * 24 * time.Hour),
		Audit:         Audit{File: "/var/log/gitlab-repopolicy/audit.log"},
		BreakglassDir: filepath.Join(etcDir, "breakglass"),
		Timeouts: Timeouts{
			Total: Duration(10 * time.Second),
			HTTP:  Duration(3 * time.Second),
		},
		MaxAPICalls: 30,
	}
}

// Presets reads only the presets of a server config, for validate and
// lint-commit on a developer machine: the other keys, such as the URL and the
// token of the server, are neither needed nor checked.
func Presets(data []byte) (map[string]map[string]any, error) {
	tree, err := strictyaml.DecodeTree(data, MaxSize)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	if tree.Kind != strictyaml.Map {
		return nil, fmt.Errorf("config: line %d: expected a mapping", tree.Line)
	}
	v, ok := tree.Fields["presets"]
	if !ok || v.Kind == strictyaml.Null {
		return nil, nil
	}
	if v.Kind != strictyaml.Map {
		return nil, fmt.Errorf("config: line %d: presets: expected a mapping", v.Line)
	}
	out := make(map[string]map[string]any, len(v.Keys))
	for _, module := range v.Keys {
		presets, ok := v.Fields[module].Interface().(map[string]any)
		if !ok {
			return nil, fmt.Errorf("config: line %d: presets.%s: expected a mapping", v.Fields[module].Line, module)
		}
		out[module] = presets
	}
	return out, nil
}

// Parse decodes data over Defaults and validates the result.
func Parse(data []byte) (*Config, error) {
	cfg := Defaults()
	if err := strictyaml.Decode(data, &cfg, MaxSize); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	return &cfg, nil
}

// validateDefaultBranch accepts short branch names: the hook compares them
// with the name HEAD points to, without refs/heads/.
func validateDefaultBranch(names []string) error {
	var errs []error
	seen := map[string]bool{}
	for _, name := range names {
		switch {
		case strings.TrimSpace(name) == "":
			errs = append(errs, errors.New("default_branch: an empty name"))
		case strings.Contains(name, "*"):
			errs = append(errs, fmt.Errorf("default_branch: %q: a name, not a pattern", name))
		case strings.HasPrefix(name, "refs/"):
			errs = append(errs, fmt.Errorf("default_branch: %q: a short name without refs/heads/", name))
		case seen[name]:
			errs = append(errs, fmt.Errorf("default_branch: %q is listed twice", name))
		}
		seen[name] = true
	}
	return errors.Join(errs...)
}

// ModuleEnabled reports whether the module is enabled; modules are enabled unless disabled explicitly.
func (c *Config) ModuleEnabled(name string) bool {
	return c.Modules[name] != ModuleDisabled
}

func (c *Config) validate() error {
	var errs []error
	switch c.Mode {
	case ModeOff, ModeAudit, ModeEnforce:
	default:
		errs = append(errs, fmt.Errorf("mode: %q is not one of off, audit, enforce", c.Mode))
	}
	errs = append(errs, c.validateURL())
	errs = append(errs, absolute("token_file", c.TokenFile, false))
	errs = append(errs, absolute("audit.file", c.Audit.File, true))
	errs = append(errs, absolute("breakglass_dir", c.BreakglassDir, false))
	if c.Audit.File == "" && !c.Audit.Stdout {
		errs = append(errs, errors.New("audit: set file, stdout or both"))
	}
	if c.TokenLifetime.Std() < 24*time.Hour {
		errs = append(errs, errors.New("token_lifetime: must be at least 1d"))
	}
	for name, d := range map[string]Duration{"timeouts.total": c.Timeouts.Total, "timeouts.http": c.Timeouts.HTTP} {
		if d <= 0 {
			errs = append(errs, fmt.Errorf("%s: must be positive", name))
		}
	}
	if c.Timeouts.HTTP > c.Timeouts.Total {
		errs = append(errs, errors.New("timeouts.http: must not exceed timeouts.total"))
	}
	if c.MaxAPICalls < 1 {
		errs = append(errs, errors.New("max_api_calls: must be at least 1"))
	}
	for name, state := range c.Modules {
		if state != ModuleEnabled && state != ModuleDisabled {
			errs = append(errs, fmt.Errorf("modules.%s: %q is not one of enabled, disabled", name, state))
		}
	}
	errs = append(errs, validateDefaultBranch(c.DefaultBranch))
	if err := module.CheckHelpText(c.Help.Contact); err != nil {
		errs = append(errs, fmt.Errorf("help.contact: %w", err))
	}
	if err := module.CheckHelpURL(c.Help.DocsURL); err != nil {
		errs = append(errs, fmt.Errorf("help.docs_url: %w", err))
	}
	return errors.Join(errs...)
}

// Endpoint is where the GitLab API is reached.
type Endpoint struct {
	// Socket is the Workhorse unix socket for http+unix, empty for https.
	Socket string
	// BaseURL is the URL that API paths are appended to, without a trailing slash.
	BaseURL string
}

const unixScheme = "http+unix://"

// Endpoint parses gitlab_url. For http+unix the host part is the socket path
// escaped with %2F, as in the GitLab Workhorse convention.
func (c *Config) Endpoint() (Endpoint, error) {
	if rest, ok := strings.CutPrefix(c.GitLabURL, unixScheme); ok {
		host, prefix, _ := strings.Cut(rest, "/")
		socket, err := url.PathUnescape(host)
		if err != nil || !filepath.IsAbs(socket) {
			return Endpoint{}, errors.New("gitlab_url: http+unix needs an absolute socket path escaped as the host, for example " +
				unixScheme + "%2Fvar%2Fopt%2Fgitlab%2Fgitlab-workhorse%2Fsockets%2Fsocket")
		}
		base := "http://gitlab"
		if prefix = strings.TrimSuffix(prefix, "/"); prefix != "" {
			base += "/" + prefix
		}
		return Endpoint{Socket: socket, BaseURL: base}, nil
	}
	u, err := url.Parse(c.GitLabURL)
	if err != nil {
		return Endpoint{}, fmt.Errorf("gitlab_url: %w", err)
	}
	if u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return Endpoint{}, fmt.Errorf("gitlab_url: %q is not an http+unix:// or https://host URL", c.GitLabURL)
	}
	return Endpoint{BaseURL: strings.TrimSuffix(u.String(), "/")}, nil
}

func (c *Config) validateURL() error {
	if c.GitLabURL == "" {
		return errors.New("gitlab_url: required")
	}
	ep, err := c.Endpoint()
	if err != nil {
		return err
	}
	if ep.Socket != "" && c.CAFile != "" {
		return errors.New("ca_file: only used with https://")
	}
	return absolute("ca_file", c.CAFile, true)
}

func absolute(name, p string, optional bool) error {
	if p == "" {
		if optional {
			return nil
		}
		return fmt.Errorf("%s: required", name)
	}
	if !filepath.IsAbs(p) {
		return fmt.Errorf("%s: %q is not an absolute path", name, p)
	}
	return nil
}
