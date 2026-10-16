package tag

import (
	"bytes"
	"errors"
	"fmt"
)

// sigKind is the format of the signature block of a tag object.
type sigKind int

const (
	sigNone sigKind = iota
	sigOpenPGP
	sigSSH
	sigX509
)

// signatureMarkers are the first lines of signature blocks that git recognises
// (gpg-interface.c); git takes the last such line in the object.
var signatureMarkers = []struct {
	prefix string
	kind   sigKind
}{
	{"-----BEGIN PGP SIGNATURE-----", sigOpenPGP},
	{"-----BEGIN PGP MESSAGE-----", sigOpenPGP},
	{"-----BEGIN SSH SIGNATURE-----", sigSSH},
	{"-----BEGIN SIGNED MESSAGE-----", sigX509},
}

// tagObject is a parsed annotated tag.
type tagObject struct {
	object  string
	typ     string
	tag     string
	kind    sigKind
	payload []byte
	sig     []byte
}

// parseTag parses the raw object of an annotated tag. The headers object,
// type and tag come first and in this order, as git writes and fsck checks them.
func parseTag(data []byte) (*tagObject, error) {
	t := &tagObject{}
	rest := data
	for _, h := range []struct {
		name string
		dst  *string
	}{{"object", &t.object}, {"type", &t.typ}, {"tag", &t.tag}} {
		line, tail, ok := bytes.Cut(rest, []byte{'\n'})
		if !ok {
			return nil, fmt.Errorf("the %s header is missing", h.name)
		}
		value, found := bytes.CutPrefix(line, []byte(h.name+" "))
		if !found || len(value) == 0 {
			return nil, fmt.Errorf("the %s header is missing", h.name)
		}
		*h.dst = string(value)
		rest = tail
	}
	if bytes.IndexByte([]byte(t.tag), 0) >= 0 {
		return nil, errors.New("the tag header holds a NUL byte")
	}
	t.kind, t.payload, t.sig = splitSignature(data)
	return t, nil
}

// splitSignature finds the last line that starts a signature block; the signed
// payload is everything before it, as with git verify-tag.
func splitSignature(data []byte) (sigKind, []byte, []byte) {
	kind, at := sigNone, -1
	for off := 0; off < len(data); {
		for _, m := range signatureMarkers {
			if bytes.HasPrefix(data[off:], []byte(m.prefix)) {
				kind, at = m.kind, off
			}
		}
		eol := bytes.IndexByte(data[off:], '\n')
		if eol < 0 {
			break
		}
		off += eol + 1
	}
	if at < 0 {
		return sigNone, data, nil
	}
	return kind, data[:at], data[at:]
}
