// Package strictyaml is the only YAML decoder of the project, used for the
// server config and for policy files. Policy files are untrusted input.
package strictyaml

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"go.yaml.in/yaml/v3"
)

// maxDepth bounds the nesting of collections in a document.
const maxDepth = 32

// Error is a decoding error; Line is 1-based and 0 when unknown.
type Error struct {
	Line int
	Msg  string
}

func (e *Error) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("line %d: %s", e.Line, e.Msg)
	}
	return e.Msg
}

// Decode decodes data into out. Besides YAML syntax it rejects documents
// larger than maxBytes, more than one document, an empty document, anchors,
// aliases, merge keys, explicit tags, duplicate keys, nesting deeper than 32
// and keys that out does not define.
func Decode(data []byte, out any, maxBytes int) error {
	if len(data) > maxBytes {
		return &Error{Msg: fmt.Sprintf("document is %d bytes, the limit is %d", len(data), maxBytes)}
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return &Error{Msg: "document is empty"}
		}
		return syntaxError(err)
	}
	var next yaml.Node
	if err := dec.Decode(&next); !errors.Is(err, io.EOF) {
		if err != nil {
			return syntaxError(err)
		}
		return &Error{Line: next.Line, Msg: "only one YAML document is allowed"}
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind == yaml.ScalarNode && doc.Content[0].Tag == "!!null" {
		return &Error{Msg: "document is empty"}
	}
	if err := check(&doc, 0); err != nil {
		return err
	}
	strict := yaml.NewDecoder(bytes.NewReader(data))
	strict.KnownFields(true)
	if err := strict.Decode(out); err != nil {
		return syntaxError(err)
	}
	return nil
}

func check(n *yaml.Node, depth int) error {
	if depth > maxDepth {
		return &Error{Line: n.Line, Msg: fmt.Sprintf("nesting deeper than %d levels", maxDepth)}
	}
	switch {
	case n.Kind == yaml.AliasNode:
		return &Error{Line: n.Line, Msg: "aliases are not allowed"}
	case n.Anchor != "":
		return &Error{Line: n.Line, Msg: "anchors are not allowed"}
	case n.Style&yaml.TaggedStyle != 0:
		return &Error{Line: n.Line, Msg: fmt.Sprintf("explicit tag %s is not allowed", n.Tag)}
	}
	if n.Kind == yaml.MappingNode {
		seen := make(map[string]bool, len(n.Content)/2)
		for i := 0; i+1 < len(n.Content); i += 2 {
			k := n.Content[i]
			if k.Tag == "!!merge" {
				return &Error{Line: k.Line, Msg: "merge keys are not allowed"}
			}
			if k.Kind != yaml.ScalarNode {
				return &Error{Line: k.Line, Msg: "keys must be scalars"}
			}
			if seen[k.Value] {
				return &Error{Line: k.Line, Msg: fmt.Sprintf("duplicate key %q", k.Value)}
			}
			seen[k.Value] = true
		}
	}
	for _, c := range n.Content {
		if err := check(c, depth+1); err != nil {
			return err
		}
	}
	return nil
}

// syntaxError converts a yaml error, which may hold several messages, into *Error.
func syntaxError(err error) error {
	var te *yaml.TypeError
	if errors.As(err, &te) && len(te.Errors) > 0 {
		return &Error{Msg: te.Errors[0]}
	}
	return &Error{Msg: err.Error()}
}
