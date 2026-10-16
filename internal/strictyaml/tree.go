package strictyaml

import (
	"fmt"

	"go.yaml.in/yaml/v3"
)

// Kind is the kind of a decoded YAML value.
type Kind int

// Kinds of values.
const (
	Null Kind = iota
	Bool
	Int
	Float
	String
	Seq
	Map
)

func (k Kind) String() string {
	return [...]string{"null", "a boolean", "a number", "a number", "a string", "a list", "a mapping"}[k]
}

// Value is a decoded YAML value with its line, for validation against a spec.
type Value struct {
	Kind Kind
	// Line is 1-based.
	Line  int
	Bool  bool
	Int   int
	Float float64
	// Str is the text of a scalar.
	Str   string
	Items []*Value
	// Keys are the keys of a mapping in document order; Fields holds their values.
	Keys   []string
	Fields map[string]*Value
	// KeyLines are the lines of the keys of a mapping.
	KeyLines map[string]int
}

// DecodeTree decodes data into a tree with the checks of Decode; unknown keys
// are left to the caller, which knows the expected structure.
func DecodeTree(data []byte, maxBytes int) (*Value, error) {
	n, err := document(data, maxBytes)
	if err != nil {
		return nil, err
	}
	return tree(n)
}

// DecodeValueTree applies DecodeTree to a value that an earlier Decode left
// undecoded, such as a section of the server config.
func DecodeValueTree(v any, maxBytes int) (*Value, error) {
	data, err := yaml.Marshal(v)
	if err != nil {
		return nil, &Error{Msg: err.Error()}
	}
	return DecodeTree(data, maxBytes)
}

func tree(n *yaml.Node) (*Value, error) {
	v := &Value{Line: n.Line, Str: n.Value}
	switch n.Kind {
	case yaml.SequenceNode:
		v.Kind = Seq
		for _, c := range n.Content {
			item, err := tree(c)
			if err != nil {
				return nil, err
			}
			v.Items = append(v.Items, item)
		}
	case yaml.MappingNode:
		v.Kind = Map
		v.Fields, v.KeyLines = map[string]*Value{}, map[string]int{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			k := n.Content[i]
			val, err := tree(n.Content[i+1])
			if err != nil {
				return nil, err
			}
			v.Keys = append(v.Keys, k.Value)
			v.Fields[k.Value], v.KeyLines[k.Value] = val, k.Line
		}
	case yaml.ScalarNode:
		switch n.Tag {
		case "!!null":
			v.Kind = Null
		case "!!bool":
			v.Kind = Bool
			if err := n.Decode(&v.Bool); err != nil {
				return nil, &Error{Line: n.Line, Msg: err.Error()}
			}
		case "!!int":
			v.Kind = Int
			if err := n.Decode(&v.Int); err != nil {
				return nil, &Error{Line: n.Line, Msg: fmt.Sprintf("%s is not a number in range", n.Value)}
			}
		case "!!float":
			v.Kind = Float
			if err := n.Decode(&v.Float); err != nil {
				return nil, &Error{Line: n.Line, Msg: err.Error()}
			}
		default:
			v.Kind = String
		}
	default:
		return nil, &Error{Line: n.Line, Msg: "unsupported YAML node"}
	}
	return v, nil
}

// Interface returns the value as the YAML decoder would decode it into any.
func (v *Value) Interface() any {
	switch v.Kind {
	case Bool:
		return v.Bool
	case Int:
		return v.Int
	case Float:
		return v.Float
	case String:
		return v.Str
	case Seq:
		out := make([]any, 0, len(v.Items))
		for _, it := range v.Items {
			out = append(out, it.Interface())
		}
		return out
	case Map:
		out := make(map[string]any, len(v.Keys))
		for _, k := range v.Keys {
			out[k] = v.Fields[k].Interface()
		}
		return out
	}
	return nil
}
