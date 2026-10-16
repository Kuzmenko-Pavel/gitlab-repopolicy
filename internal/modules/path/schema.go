package path

import "github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"

// The types below describe PATHPOLICY for schemas/pathpolicy.schema.json.

type schemaFile struct {
	Version schemaVersion `yaml:"version" schema:"required" desc:"Schema version; version 1 is frozen."`
	Admins  schemaAdmins  `yaml:"admins" schema:"required" desc:"1 to 5 usernames that may change PATHPOLICY itself."`
	Rules   []schemaRule  `yaml:"rules" desc:"Path rules; every rule that matches a path must be satisfied (AND)."`
}

type schemaRule struct {
	Name    schemaName    `yaml:"name" schema:"required"`
	Paths   schemaPaths   `yaml:"paths" schema:"required" desc:"1 to 50 doublestar patterns anchored at the repository root."`
	Refs    schemaRefs    `yaml:"refs" desc:"all (default), protected, default or a list of ref patterns such as refs/heads/release/*."`
	Allow   schemaAllow   `yaml:"allow" schema:"required" desc:"1 to 50 usernames that may change the paths; no groups or roles."`
	Message schemaMessage `yaml:"message" desc:"Text shown when a change is denied, up to 1000 characters."`
}

type (
	schemaVersion int
	schemaName    string
	schemaMessage string
	schemaAdmins  []string
	schemaAllow   []string
	schemaPaths   []string
	schemaRefs    struct{}
)

func usernameList(lowItems, highItems int) map[string]any {
	return map[string]any{"type": "array", "minItems": lowItems, "maxItems": highItems, "uniqueItems": true,
		"items": map[string]any{"type": "string", "pattern": module.UsernamePattern}}
}

func (schemaVersion) JSONSchema() map[string]any {
	return map[string]any{"type": "integer", "const": 1}
}
func (schemaName) JSONSchema() map[string]any {
	return map[string]any{"type": "string", "pattern": module.RuleNamePattern}
}
func (schemaMessage) JSONSchema() map[string]any {
	return map[string]any{"type": "string", "maxLength": 1000}
}
func (schemaAdmins) JSONSchema() map[string]any { return usernameList(1, 5) }
func (schemaAllow) JSONSchema() map[string]any  { return usernameList(1, 50) }

func (schemaPaths) JSONSchema() map[string]any {
	return map[string]any{"type": "array", "minItems": 1, "maxItems": 50,
		"items": map[string]any{"type": "string", "minLength": 1, "maxLength": 256, "pattern": `^[^/!\\{}]`}}
}

func (schemaRefs) JSONSchema() map[string]any {
	return map[string]any{"oneOf": []any{
		map[string]any{"type": "string", "enum": []string{"all", "default", "protected"}},
		map[string]any{"type": "array", "minItems": 1, "items": map[string]any{"type": "string", "pattern": "^refs/"}},
	}}
}
