package module

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxHelp is the length limit of a help value, in characters.
const MaxHelp = 200

// Help is where a pusher gets help with a rule: printed in the block of its
// violation. The help of the server config works the same way.
type Help struct {
	Contact string `yaml:"contact"`
	DocsURL string `yaml:"docs_url"`
}

// CheckHelpText checks a help value: at most MaxHelp characters, no control
// characters.
func CheckHelpText(v string) error {
	switch {
	case utf8.RuneCountInString(v) > MaxHelp:
		return fmt.Errorf("%d characters, the limit is %d", utf8.RuneCountInString(v), MaxHelp)
	case strings.ContainsFunc(v, unicode.IsControl):
		return fmt.Errorf("control characters are not allowed")
	}
	return nil
}

// CheckHelpURL checks a docs URL: a help value that is an http(s) URL.
func CheckHelpURL(v string) error {
	if err := CheckHelpText(v); err != nil {
		return err
	}
	if v != "" && !strings.HasPrefix(v, "https://") && !strings.HasPrefix(v, "http://") {
		return fmt.Errorf("%q is not an http(s) URL", v)
	}
	return nil
}
