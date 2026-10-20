package spec

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

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
