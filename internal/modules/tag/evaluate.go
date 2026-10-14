package tag

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
)

// maxTagObject is the largest tag object that is read; a larger one is not a release tag.
const maxTagObject = 1 << 20

// Evaluate checks every created or moved tag whose name matches a rule
// (architecture §11.2). Deleting a tag is not regulated.
func (p *policy) Evaluate(ctx context.Context, in *module.Input) (*module.Result, error) {
	res := &module.Result{}
	push := in.Clock()
	for _, u := range in.Updates {
		if !u.IsTag() || u.Kind() == module.Delete {
			continue
		}
		rules := p.matching(tagName(u.Ref))
		if len(rules) == 0 {
			continue
		}
		res.Touched = true
		if err := p.check(ctx, in, u, rules, push, res); err != nil {
			return nil, err
		}
	}
	if len(res.Violations) > 0 {
		res.Decision = module.Deny
	}
	return res, nil
}

// check runs steps 1-6 of architecture §11.2 for one tag.
func (p *policy) check(ctx context.Context, in *module.Input, u module.RefUpdate, rules []*rule, push time.Time, res *module.Result) error {
	name := tagName(u.Ref)
	deny := func(code, msg, reason string) {
		res.Violations = append(res.Violations, module.Violation{Code: code, Rule: ruleNames(rules), Ref: u.Ref, Message: msg, Reason: reason})
	}
	obj, err := in.Objects.Read(ctx, u.New, maxTagObject)
	switch {
	case errors.Is(err, module.ErrTooLarge):
		deny(module.CodeTagObjectInvalid, fmt.Sprintf("The tag object is larger than %d bytes.", maxTagObject), "tag object too large")
		return nil
	case err != nil:
		return err
	}
	if obj.Type != "tag" {
		deny(module.CodeTagNotSigned, "A lightweight tag has no signature. Create a signed tag: git tag -s "+name, "lightweight tag to a "+obj.Type)
		return nil
	}
	t, err := parseTag(obj.Data)
	if err != nil {
		deny(module.CodeTagObjectInvalid, "The tag object cannot be parsed: "+err.Error()+".", err.Error())
		return nil
	}
	if t.tag != name {
		deny(module.CodeTagObjectInvalid, fmt.Sprintf("The tag object is named %q, but it is pushed as %q. Push a tag signed under its own name.", t.tag, name),
			"tag header "+t.tag)
		return nil
	}
	if t.typ != "commit" {
		deny(module.CodeTagObjectInvalid, fmt.Sprintf("The tag points to a %s; a release tag points to a commit.", t.typ), "tag of a "+t.typ)
		return nil
	}
	switch t.kind {
	case sigNone:
		deny(module.CodeTagNotSigned, "The tag is not signed. Create it with git tag -s "+name, "no signature")
		return nil
	case sigX509:
		deny(module.CodeTagNotSigned, "The tag has an X.509 signature; OpenPGP and SSH signatures are supported.", "x509 signature")
		return nil
	}
	k, err := p.verify(t, push)
	var unknown errUnknownSigner
	switch {
	case errors.As(err, &unknown):
		deny(module.CodeTagSignerNotAllowed, signerText(rules, "a key that is not in TAGPOLICY", unknown.keyID), err.Error())
		return nil
	case err != nil:
		deny(module.CodeTagSignatureInvalid, "The signature cannot be accepted: "+strings.TrimPrefix(err.Error(), errBadSignature.Error()+": ")+".", err.Error())
		return nil
	}
	res.Signatures = append(res.Signatures, module.Signature{Ref: u.Ref, Rules: ruleList(rules), Signer: k.label, Fingerprint: k.fingerprint})
	for _, r := range rules {
		if !r.allows(k.label) {
			res.Violations = append(res.Violations, module.Violation{Code: module.CodeTagSignerNotAllowed, Rule: r.name, Ref: u.Ref,
				Message: signerText([]*rule{r}, k.label, k.fingerprint),
				Reason:  fmt.Sprintf("signer %s (%s) not in signers of %s", k.label, k.fingerprint, r.name)})
		}
	}
	return nil
}

// verify checks the signature against the keys of its format and returns the entry of the signer.
func (p *policy) verify(t *tagObject, push time.Time) (*key, error) {
	if t.kind == sigSSH {
		s, err := parseSSHSig(t.sig)
		if err != nil {
			return nil, err
		}
		k := p.bySSH(s.pub)
		if k == nil {
			return nil, errUnknownSigner{keyID: ssh256(s)}
		}
		if err := s.verify(k.ssh, t.payload); err != nil {
			return nil, err
		}
		return k, nil
	}
	e, err := verifyOpenPGP(p.ring(), t.payload, t.sig, push)
	if err != nil {
		return nil, err
	}
	k := p.byOpenPGP(e)
	if k == nil {
		return nil, errUnknownSigner{keyID: fmt.Sprintf("%X", e.PrimaryKey.Fingerprint)}
	}
	return k, nil
}

// signerText is the user text of a signer that the rules do not allow.
func signerText(rules []*rule, signer, fingerprint string) string {
	var b strings.Builder
	for _, r := range rules {
		if r.message != "" {
			b.WriteString(r.message + "\n")
		}
	}
	fmt.Fprintf(&b, "signed by: %s (%s)\n", signer, fingerprint)
	allowed := rules[0].signers
	for _, r := range rules[1:] {
		var both []string
		for _, s := range allowed {
			if r.allows(s) {
				both = append(both, s)
			}
		}
		allowed = both
	}
	if len(allowed) == 0 {
		b.WriteString("allowed:   nobody, the matching rules have no signer in common")
	} else {
		b.WriteString("allowed:   " + strings.Join(allowed, ", "))
	}
	return b.String()
}

func ruleList(rules []*rule) []string {
	names := make([]string, len(rules))
	for i, r := range rules {
		names[i] = r.name
	}
	return names
}

func ruleNames(rules []*rule) string { return strings.Join(ruleList(rules), ", ") }
