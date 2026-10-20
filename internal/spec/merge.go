package spec

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/strictyaml"
)

// FileLayer is the name of the layer of the policy file itself.
const FileLayer = "file"

// PresetLayer is the name of the layer of a preset.
func PresetLayer(name string) string { return "preset:" + name }

// Layer is one source of rules: a preset of the server config or the file.
type Layer struct {
	Name string
	// Rules is the rules mapping of the layer; nil when the layer has none.
	Rules *strictyaml.Value
}

// Origin tells where merged rules come from. For a rule or another mapping,
// keyed by its dotted path such as "ci", it lists the layers that set
// something in it, in merge order; for a list or a scalar, such as
// "ci.allow", it holds the one layer whose value won.
type Origin map[string][]string

// MergeRules merges the rules of the layers in order, as Helm merges values
// files: mappings merge key by key, a later list or scalar replaces an
// earlier one, and null removes the key. Values that come from presets lose
// their line numbers: they are not lines of the file a person edits.
func MergeRules(layers []Layer) (*strictyaml.Value, Origin) {
	out := &strictyaml.Value{Kind: strictyaml.Map, Fields: map[string]*strictyaml.Value{}, KeyLines: map[string]int{}}
	origin := Origin{}
	for _, l := range layers {
		if l.Rules == nil || l.Rules.Kind != strictyaml.Map {
			continue
		}
		mergeInto(out, l.Rules, "", l.Name, l.Name == FileLayer, origin)
	}
	return out, origin
}

func mergeInto(dst, src *strictyaml.Value, path, layer string, keepLines bool, origin Origin) {
	for _, k := range src.Keys {
		sv, p := src.Fields[k], join(path, k)
		if sv.Kind == strictyaml.Null {
			removeKey(dst, k)
			origin.remove(p)
			continue
		}
		if dv, ok := dst.Fields[k]; ok && dv.Kind == strictyaml.Map && sv.Kind == strictyaml.Map {
			if keepLines {
				dv.Line, dst.KeyLines[k] = sv.Line, src.KeyLines[k]
			}
			origin.add(p, layer)
			mergeInto(dv, sv, p, layer, keepLines, origin)
			continue
		}
		if _, ok := dst.Fields[k]; !ok {
			dst.Keys = append(dst.Keys, k)
		}
		line := 0
		if keepLines {
			line = src.KeyLines[k]
		}
		dst.Fields[k], dst.KeyLines[k] = clone(sv, keepLines), line
		origin.remove(p)
		origin.set(p, sv, layer)
	}
}

func join(path, k string) string {
	if path == "" {
		return k
	}
	return path + "." + k
}

func removeKey(v *strictyaml.Value, k string) {
	if _, ok := v.Fields[k]; !ok {
		return
	}
	delete(v.Fields, k)
	delete(v.KeyLines, k)
	v.Keys = slices.DeleteFunc(v.Keys, func(s string) bool { return s == k })
}

func clone(v *strictyaml.Value, keepLines bool) *strictyaml.Value {
	c := *v
	if !keepLines {
		c.Line = 0
	}
	if v.Items != nil {
		c.Items = make([]*strictyaml.Value, 0, len(v.Items))
		for _, it := range v.Items {
			c.Items = append(c.Items, clone(it, keepLines))
		}
	}
	if v.Fields != nil {
		c.Keys = slices.Clone(v.Keys)
		c.Fields, c.KeyLines = make(map[string]*strictyaml.Value, len(v.Fields)), make(map[string]int, len(v.Fields))
		for _, k := range v.Keys {
			c.Fields[k] = clone(v.Fields[k], keepLines)
			if keepLines {
				c.KeyLines[k] = v.KeyLines[k]
			}
		}
	}
	return &c
}

func (o Origin) add(path, layer string) {
	if !slices.Contains(o[path], layer) {
		o[path] = append(o[path], layer)
	}
}

// set records a value that replaced whatever was at path, with its subtree.
func (o Origin) set(path string, v *strictyaml.Value, layer string) {
	o[path] = []string{layer}
	if v.Kind == strictyaml.Map {
		for _, k := range v.Keys {
			o.set(join(path, k), v.Fields[k], layer)
		}
	}
}

func (o Origin) remove(path string) {
	for p := range o {
		if p == path || strings.HasPrefix(p, path+".") {
			delete(o, p)
		}
	}
}

// WithRules is the file tree with the rules field replaced by rules.
func WithRules(file, rules *strictyaml.Value) *strictyaml.Value {
	c := *file
	c.Fields = make(map[string]*strictyaml.Value, len(file.Fields)+1)
	c.KeyLines = make(map[string]int, len(file.KeyLines)+1)
	for k, v := range file.Fields {
		c.Fields[k], c.KeyLines[k] = v, file.KeyLines[k]
	}
	if _, ok := c.Fields["rules"]; !ok {
		c.Keys = append(slices.Clone(file.Keys), "rules")
	}
	if rules.Line == 0 {
		rules.Line = file.Line
	}
	c.Fields["rules"] = rules
	return &c
}

// Presets are the presets of one module from the server config, by name.
// A broken preset is kept as an error: the config still loads, and a file
// that extends the preset cannot be used.
type Presets struct {
	rules   map[string]*strictyaml.Value
	digests map[string]string
	errs    map[string]error
}

// NewPresets validates raw presets against presetSpec, the module spec of a
// preset: a mapping with the field rules, every rule complete.
func NewPresets(raw map[string]any, presetSpec *Spec, checks Checks, maxBytes int) Presets {
	p := Presets{rules: map[string]*strictyaml.Value{}, digests: map[string]string{}, errs: map[string]error{}}
	for name, v := range raw {
		tree, err := strictyaml.DecodeValueTree(v, maxBytes)
		if err == nil {
			// Lines of a re-encoded preset are not lines of the server config.
			tree = clone(tree, false)
			err = ValidateMerged(presetSpec, tree, checks)
		}
		if err != nil {
			p.errs[name] = err
			continue
		}
		p.rules[name] = tree.Fields["rules"]
		data, err := json.Marshal(tree.Interface())
		if err != nil {
			p.errs[name] = err
			continue
		}
		sum := sha256.Sum256(data)
		p.digests[name] = hex.EncodeToString(sum[:])
	}
	return p
}

// Layers are the layers of the presets named by extends, followed by the file.
func (p Presets) Layers(extends []string, fileRules *strictyaml.Value) ([]Layer, error) {
	layers := make([]Layer, 0, len(extends)+1)
	for _, name := range extends {
		if err, broken := p.errs[name]; broken {
			return nil, fmt.Errorf("extends: preset %q of the server config is invalid: %w", name, err)
		}
		rules, ok := p.rules[name]
		if !ok {
			return nil, fmt.Errorf("extends: preset %q is not defined in the server config", name)
		}
		layers = append(layers, Layer{Name: PresetLayer(name), Rules: rules})
	}
	return append(layers, Layer{Name: FileLayer, Rules: fileRules}), nil
}

// Digest is the SHA-256 of a preset as canonical JSON, for the audit log; it
// does not depend on the formatting of the server config.
func (p Presets) Digest(name string) string { return p.digests[name] }

// Errors lists the broken presets as "name: problem", sorted.
func (p Presets) Errors() []string {
	out := make([]string, 0, len(p.errs))
	for name, err := range p.errs {
		out = append(out, fmt.Sprintf("%s: %v", name, err))
	}
	sort.Strings(out)
	return out
}

// Resolved is a policy file merged with its presets.
type Resolved struct {
	// Doc is the file with its rules replaced by the merge result.
	Doc *strictyaml.Value
	// Origin tells where every rule and field comes from.
	Origin Origin
	// Extends are the preset names of the file in merge order.
	Extends []string
}

// PresetError is a problem of a preset that a file extends: missing from the
// server config or invalid. Each module decides whether the file is broken
// or the module is skipped.
type PresetError struct{ Err error }

func (e *PresetError) Error() string { return e.Err.Error() }
func (e *PresetError) Unwrap() error { return e.Err }

// Resolve validates a policy file, merges the presets it extends and the file
// as Helm merges values files, and validates the result, where the fields of
// rules that only the result must have are required.
func Resolve(data []byte, maxBytes int, s *Spec, checks Checks, presets Presets) (*Resolved, error) {
	file, err := strictyaml.DecodeTree(data, maxBytes)
	if err != nil {
		return nil, err
	}
	if err := Validate(s, file, checks); err != nil {
		return nil, err
	}
	extends, err := ExtendsNames(file.Fields["extends"])
	if err != nil {
		return nil, err
	}
	layers, err := presets.Layers(extends, file.Fields["rules"])
	if err != nil {
		return nil, &PresetError{Err: err}
	}
	rules, origin := MergeRules(layers)
	doc := WithRules(file, rules)
	if err := ValidateMerged(s, doc, checks); err != nil {
		return nil, err
	}
	return &Resolved{Doc: doc, Origin: origin, Extends: extends}, nil
}

// Rules are the merged rules in merge order: preset rules first, then the new
// rules of the file.
func (r *Resolved) Rules() []string { return r.Doc.Fields["rules"].Keys }

// Rule is the merged value of a rule.
func (r *Resolved) Rule(name string) *strictyaml.Value { return r.Doc.Fields["rules"].Fields[name] }

// DecodeRule decodes a merged rule into out, a struct with yaml tags.
func (r *Resolved) DecodeRule(name string, out any) error {
	return strictyaml.DecodeValue(r.Rule(name).Interface(), out, 1<<22)
}

// Refs are the presets the file extends with their digests, for the audit log.
func (r *Resolved) Refs(presets Presets) []module.PresetRef {
	out := make([]module.PresetRef, 0, len(r.Extends))
	for _, name := range r.Extends {
		out = append(out, module.PresetRef{Name: name, SHA256: presets.Digest(name)})
	}
	return out
}

// RuleOrigins are the merged rules with the layers of each rule and field;
// a mapping field such as checks also lists its keys, one level deep.
func (r *Resolved) RuleOrigins() []module.RuleOrigin {
	out := make([]module.RuleOrigin, 0, len(r.Rules()))
	for _, name := range r.Rules() {
		o := module.RuleOrigin{Rule: name, Layers: r.Origin[name]}
		rule := r.Rule(name)
		for _, f := range rule.Keys {
			o.Fields = append(o.Fields, module.FieldOrigin{Field: f, Layers: r.Origin[name+"."+f]})
			if v := rule.Fields[f]; v.Kind == strictyaml.Map {
				for _, k := range v.Keys {
					o.Fields = append(o.Fields, module.FieldOrigin{Field: f + "." + k, Layers: r.Origin[name+"."+f+"."+k]})
				}
			}
		}
		out = append(out, o)
	}
	return out
}
