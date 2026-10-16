package commit

import "github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"

// The types below describe COMMITPOLICY for schemas/commitpolicy.schema.json;
// the module decodes the file through fileYAML and checks rules by hand.

type schemaFile struct {
	Version  schemaVersion   `yaml:"version" schema:"required" desc:"Schema version of the file."`
	Mode     schemaMode      `yaml:"mode" desc:"warn turns every error into a warning; the default is enforce."`
	Extends  string          `yaml:"extends" desc:"A preset from commitpolicy_presets of the server config; rules of the file override it."`
	Rulesets []schemaRuleset `yaml:"rulesets" schema:"required" desc:"Rule sets; every ruleset that matches a ref applies (AND)."`
}

type schemaRuleset struct {
	Name    schemaName   `yaml:"name" schema:"required"`
	Refs    schemaRefs   `yaml:"refs" desc:"all (default), default, protected or a list of ref patterns such as refs/heads/release/*."`
	Format  schemaFormat `yaml:"format" desc:"conventional (default): type(scope)!: subject; free: the header is the subject."`
	Except  schemaExcept `yaml:"except"`
	Rules   schemaRules  `yaml:"rules"`
	Message string       `yaml:"message" desc:"Text shown to the author of a rejected commit."`
}

type schemaExcept struct {
	MergeCommits  bool        `yaml:"merge_commits" desc:"Skip merge commits."`
	GitReverts    bool        `yaml:"git_reverts" desc:"Skip messages of git revert and of the GitLab revert button."`
	MergedCommits bool        `yaml:"merged_commits" desc:"Check only the first-parent chain: commits that a merge commit brings in are not checked."`
	Users         schemaUsers `yaml:"users" desc:"Usernames whose pushes skip this ruleset, for example a release bot."`
}

type schemaRules struct {
	TypeEnum          *ruleList      `yaml:"type-enum"`
	Scope             *ruleScope     `yaml:"scope"`
	ScopeEnum         *ruleList      `yaml:"scope-enum"`
	BreakingBang      *ruleAllow     `yaml:"breaking-bang"`
	HeaderMaxLength   *ruleNumber    `yaml:"header-max-length"`
	SubjectCase       *ruleCase      `yaml:"subject-case"`
	SubjectFullStop   *ruleFullStop  `yaml:"subject-full-stop"`
	BodyRequired      *ruleBase      `yaml:"body-required"`
	BodyMaxLineLength *ruleNumber    `yaml:"body-max-line-length"`
	Reference         *ruleReference `yaml:"reference"`
	TrailerRequired   *ruleList      `yaml:"trailer-required"`
	Pattern           *rulePattern   `yaml:"pattern"`
}

type ruleBase struct {
	Level schemaLevel `yaml:"level" schema:"required"`
	Types []string    `yaml:"types" desc:"Apply the rule only to these types."`
}

type ruleList struct {
	Level schemaLevel `yaml:"level" schema:"required"`
	Types []string    `yaml:"types"`
	Value []string    `yaml:"value" schema:"required"`
}

type ruleNumber struct {
	Level schemaLevel `yaml:"level" schema:"required"`
	Types []string    `yaml:"types"`
	Value int         `yaml:"value" schema:"required"`
}

type ruleScope struct {
	Level schemaLevel `yaml:"level" schema:"required"`
	Types []string    `yaml:"types"`
	Value enumScope   `yaml:"value" schema:"required"`
}

type ruleAllow struct {
	Level schemaLevel `yaml:"level" schema:"required"`
	Types []string    `yaml:"types"`
	Value enumAllow   `yaml:"value" schema:"required"`
}

type ruleCase struct {
	Level schemaLevel `yaml:"level" schema:"required"`
	Types []string    `yaml:"types"`
	Value enumCase    `yaml:"value" schema:"required"`
}

type ruleFullStop struct {
	Level schemaLevel  `yaml:"level" schema:"required"`
	Types []string     `yaml:"types"`
	Value enumFullStop `yaml:"value" schema:"required"`
}

type ruleReference struct {
	Level    schemaLevel  `yaml:"level" schema:"required"`
	Types    []string     `yaml:"types"`
	Location enumLocation `yaml:"location" schema:"required"`
	Pattern  string       `yaml:"pattern" schema:"required" desc:"RE2 regular expression of the reference, for example group/project#123."`
	Keys     []string     `yaml:"keys" desc:"Trailer keys to look in, for location: trailer."`
}

type rulePattern struct {
	Level    schemaLevel    `yaml:"level" schema:"required"`
	Types    []string       `yaml:"types"`
	Value    string         `yaml:"value" schema:"required" desc:"regexp2 expression in RE2 mode, matched at the start of the message."`
	Examples schemaExamples `yaml:"examples"`
}

type schemaExamples struct {
	Valid   []string `yaml:"valid"`
	Invalid []string `yaml:"invalid"`
}

type (
	schemaUsers   []string
	schemaVersion int
	schemaName    string
	schemaMode    string
	schemaFormat  string
	schemaLevel   string
	schemaRefs    struct{}
	enumScope     string
	enumAllow     string
	enumCase      string
	enumFullStop  string
	enumLocation  string
)

func enum(values ...string) map[string]any {
	return map[string]any{"type": "string", "enum": values}
}

func (schemaVersion) JSONSchema() map[string]any {
	return map[string]any{"type": "integer", "const": 1}
}
func (schemaName) JSONSchema() map[string]any {
	return map[string]any{"type": "string", "pattern": module.RuleNamePattern}
}
func (schemaMode) JSONSchema() map[string]any   { return enum("warn", "enforce") }
func (schemaFormat) JSONSchema() map[string]any { return enum("conventional", "free") }
func (schemaLevel) JSONSchema() map[string]any  { return enum("error", "warn", "off") }
func (enumScope) JSONSchema() map[string]any    { return enum("optional", "required", "forbidden") }
func (enumAllow) JSONSchema() map[string]any    { return enum("allow", "forbid") }
func (enumCase) JSONSchema() map[string]any     { return enum("lower-first", "sentence", "any") }
func (enumFullStop) JSONSchema() map[string]any { return enum("forbid", "require") }
func (enumLocation) JSONSchema() map[string]any { return enum("trailer", "body", "any") }

func (schemaRefs) JSONSchema() map[string]any {
	return map[string]any{"oneOf": []any{
		enum("all", "default", "protected"),
		map[string]any{"type": "array", "minItems": 1, "items": map[string]any{"type": "string", "pattern": "^refs/"}},
	}}
}

func (schemaUsers) JSONSchema() map[string]any {
	return map[string]any{"type": "array", "maxItems": 50,
		"items": map[string]any{"type": "string", "pattern": module.UsernamePattern}}
}
