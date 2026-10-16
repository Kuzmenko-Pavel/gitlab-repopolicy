package tag

import "github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"

// The types below describe TAGPOLICY for schemas/tagpolicy.schema.json.

type schemaFile struct {
	Version schemaVersion `yaml:"version" schema:"required" desc:"Schema version; version 1 is frozen."`
	Rules   []schemaRule  `yaml:"rules" desc:"Up to 50 tag rules; every rule that matches a tag must be satisfied (AND)."`
	Keys    schemaKeys    `yaml:"keys" schema:"required" desc:"1 to 32 public keys by label: an armored OpenPGP public key block or an OpenSSH public key on one line."`
}

type schemaRule struct {
	Name    schemaName    `yaml:"name" schema:"required"`
	Tags    schemaTags    `yaml:"tags" schema:"required" desc:"RE2 regular expression that matches the whole tag name without refs/tags/."`
	Signers schemaSigners `yaml:"signers" schema:"required" desc:"Labels of keys that may sign the matching tags."`
	Message schemaMessage `yaml:"message" desc:"Text shown when a tag is denied, up to 1000 characters."`
}

type (
	schemaVersion int
	schemaName    string
	schemaTags    string
	schemaMessage string
	schemaSigners []string
	schemaKeys    map[string]string
)

const labelPattern = `^[A-Za-z0-9_.-]{1,255}$`

func (schemaVersion) JSONSchema() map[string]any {
	return map[string]any{"type": "integer", "const": 1}
}
func (schemaName) JSONSchema() map[string]any {
	return map[string]any{"type": "string", "pattern": module.RuleNamePattern}
}
func (schemaTags) JSONSchema() map[string]any {
	return map[string]any{"type": "string", "minLength": 1, "maxLength": maxPatternBytes}
}
func (schemaMessage) JSONSchema() map[string]any {
	return map[string]any{"type": "string", "maxLength": maxMessage}
}
func (schemaSigners) JSONSchema() map[string]any {
	return map[string]any{"type": "array", "minItems": 1, "maxItems": maxKeys, "uniqueItems": true,
		"items": map[string]any{"type": "string", "pattern": labelPattern}}
}
func (schemaKeys) JSONSchema() map[string]any {
	return map[string]any{"type": "object", "minProperties": 1, "maxProperties": maxKeys,
		"propertyNames":        map[string]any{"pattern": labelPattern},
		"additionalProperties": map[string]any{"type": "string", "minLength": 1}}
}
