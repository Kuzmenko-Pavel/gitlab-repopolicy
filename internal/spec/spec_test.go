package spec

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/strictyaml"
)

var sample = &Spec{Type: Object, Fields: []Field{
	{Name: "version", Required: true, Spec: &Spec{Type: Integer, Const: 1}},
	{Name: "mode", Spec: &Spec{Type: String, Enum: []string{"warn", "enforce"}}},
	{Name: "limit", Spec: &Spec{Type: Integer, Minimum: 1, Maximum: 10}},
	{Name: "admins", Required: true, Spec: &Spec{Type: Array, MinItems: 1, MaxItems: 2, Noun: "usernames", Unique: true,
		Items: &Spec{Type: String, Check: "lower"}}},
	{Name: "rules", Spec: &Spec{Type: Array, MaxItems: 2, Noun: "rules", Named: "rule", Items: &Spec{Type: Object, Fields: []Field{
		{Name: "name", Required: true, Spec: &Spec{Type: String, Pattern: "^[a-z0-9-]{1,63}$", Format: "[a-z0-9-]{1,63}"}},
		{Name: "message", Spec: &Spec{Type: String, MaxLength: 3}},
		{Name: "tags", Spec: &Spec{Type: String, MinLength: 1, MaxLength: 4, Bytes: true}},
		{Name: "except", Spec: &Spec{Type: Object, Fields: []Field{{Name: "merges", Spec: &Spec{Type: Boolean}}}}},
	}}}},
	{Name: "keys", Spec: &Spec{Type: Dict, MaxItems: 1, Noun: "keys", KeyPattern: "^[a-z]+$", KeyFormat: "[a-z]+",
		Values: &Spec{Type: String}}},
	{Name: "match", Spec: Match("", true)},
}}

var lower = Checks{
	"lower": func(v *strictyaml.Value) error {
		if v.Str != "a" && v.Str != "b" && v.Str != "c" {
			return errors.New("only a, b or c")
		}
		return nil
	},
}

func TestValidate(t *testing.T) {
	for in, want := range map[string]string{
		"version: 1\nadmins: [a]\nrules: [{name: r}]\nkeys: {k: x}\nmatch: [main]\n": "",
		"version: 2\nadmins: [a]\n":                                            "line 1: version: 2 is not supported, use 1",
		"admins: [a]\n":                                                        "line 1: version: required",
		"version: 1\n":                                                         "line 1: admins: 1 to 2 usernames are required",
		"version: 1\nadmins: [a, b, c]\n":                                      "line 2: admins: 1 to 2 usernames are required",
		"version: 1\nadmins: [a, a]\n":                                         `line 2: admins: "a" is listed twice`,
		"version: 1\nadmins: [d]\n":                                            "line 2: admins: only a, b or c",
		"version: 1\nadmins: a\n":                                              "line 2: admins: expected a list, got a string",
		"version: 1\nadmins: [a]\nmode: strict\n":                              `line 3: mode: "strict" is not one of warn, enforce`,
		"version: 1\nadmins: [a]\nlimit: 11\n":                                 "line 3: limit: a number from 1 to 10 is expected",
		"version: 1\nadmins: [a]\nlimit: x\n":                                  "line 3: limit: expected a number, got a string",
		"version: 1\nadmins: [a]\nadmin: [a]\n":                                `line 3: unknown field "admin" (did you mean "admins"?)`,
		"version: 1\nadmins: [a]\nzzz: 1\n":                                    `line 3: unknown field "zzz"`,
		"version: 1\nadmins: [a]\nrules: [{name: R}]\n":                        `line 3: rule #1: name: "R" must match [a-z0-9-]{1,63}`,
		"version: 1\nadmins: [a]\nrules: [{}]\n":                               "line 3: rule #1: name: required",
		"version: 1\nadmins: [a]\nrules: [{name: r}, {name: r}]\n":             "line 3: rule r: the name is used twice",
		"version: 1\nadmins: [a]\nrules: [{name: a}, {name: b}, {name: c}]\n":  "line 3: rules: 3 rules, the limit is 2",
		"version: 1\nadmins: [a]\nrules: [{name: r, message: abcd}]\n":         "line 3: rule r: message: 4 characters, the limit is 3",
		"version: 1\nadmins: [a]\nrules: [{name: r, message: ыыы}]\n":          "",
		"version: 1\nadmins: [a]\nrules: [{name: r, tags: ыыы}]\n":             "line 3: rule r: tags: 6 bytes, the limit is 4",
		"version: 1\nadmins: [a]\nrules: [{name: r, tags: ''}]\n":              "line 3: rule r: tags: required",
		"version: 1\nadmins: [a]\nrules: [{name: r, except: {merge: true}}]\n": `line 3: rule r: except: unknown field "merge" (did you mean "merges"?)`,
		"version: 1\nadmins: [a]\nrules: [{name: r, except: {merges: 1}}]\n":   "line 3: rule r: except.merges: expected true or false, got a number",
		"version: 1\nadmins: [a]\nkeys: {K: x}\n":                              `line 3: keys: label "K" must match [a-z]+`,
		"version: 1\nadmins: [a]\nkeys: {a: x, b: y}\n":                        "line 3: keys: 2 keys, the limit is 1",
		"version: 1\nadmins: [a]\nkeys: {a: 1}\n":                              "line 3: keys.a: expected a string, got a number",
		"version: 1\nadmins: [a]\nmatch: protected\n":                          `line 3: match: "protected" is not all, default or a list; write a single name as [protected]`,
	} {
		tree, err := strictyaml.DecodeTree([]byte(in), 1<<10)
		require.NoError(t, err, in)
		err = Validate(sample, tree, lower)
		if want == "" {
			assert.NoError(t, err, in)
			continue
		}
		assert.EqualError(t, err, want, in)
	}
}

func TestValidate_UnknownCheck(t *testing.T) {
	tree, err := strictyaml.DecodeTree([]byte("x\n"), 1<<10)
	require.NoError(t, err)
	assert.ErrorContains(t, Validate(&Spec{Type: String, Check: "nope"}, tree, nil), `check "nope" is not implemented`)
}

func TestOpenObject(t *testing.T) {
	tree, err := strictyaml.DecodeTree([]byte("known: x\nnewer: y\n"), 1<<10)
	require.NoError(t, err)
	open := &Spec{Type: Object, Open: true, Fields: []Field{{Name: "known", Spec: &Spec{Type: String}}}}
	assert.NoError(t, Validate(open, tree, nil), "the parser handles keys of a newer version")
}
