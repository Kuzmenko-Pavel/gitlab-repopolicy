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

// Evaluate checks every created or moved tag whose name matches a rule. Deleting
// a tag is not regulated.
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

// check verifies one tag: the tag object, its signature and its signer.
func (p *policy) check(ctx context.Context, in *module.Input, u module.RefUpdate, rules []*rule, push time.Time, res *module.Result) error {
	name := tagName(u.Ref)
	deny := func(v module.Violation, reason string) {
		v.Rule, v.Ref, v.Reason = ruleNames(rules), u.Ref, reason
		res.Violations = append(res.Violations, v)
	}
	obj, err := in.Objects.Read(ctx, u.New, maxTagObject)
	switch {
	case errors.Is(err, module.ErrTooLarge):
		deny(module.Violation{Code: module.CodeTagObjectInvalid, Title: fmt.Sprintf("the tag object is larger than %d bytes", maxTagObject)}, "tag object too large")
		return nil
	case err != nil:
		return err
	}
	if obj.Type != "tag" {
		deny(p.unsigned(rules, name, in.Protocol, "it is a lightweight tag, it has no signature"), "lightweight tag to a "+obj.Type)
		return nil
	}
	t, err := parseTag(obj.Data)
	if err != nil {
		deny(module.Violation{Code: module.CodeTagObjectInvalid, Title: "the tag object cannot be read", Facts: []string{err.Error()}}, err.Error())
		return nil
	}
	if t.tag != name {
		deny(module.Violation{Code: module.CodeTagObjectInvalid, Title: fmt.Sprintf("the tag was signed as %q but is pushed as %q", t.tag, name),
			Hint: fmt.Sprintf("sign a tag under its own name: git tag -s %s <commit>", name)}, "tag header "+t.tag)
		return nil
	}
	if t.typ != "commit" {
		deny(module.Violation{Code: module.CodeTagObjectInvalid, Title: fmt.Sprintf("the tag points to a %s, not a commit", t.typ),
			Hint: fmt.Sprintf("tag a commit: git tag -s %s <commit>", name)}, "tag of a "+t.typ)
		return nil
	}
	switch t.kind {
	case sigNone:
		deny(p.unsigned(rules, name, in.Protocol, "the tag has no signature"), "no signature")
		return nil
	case sigX509:
		v := p.unsigned(rules, name, in.Protocol, "the tag has an X.509 signature; OpenPGP and SSH signatures are supported")
		deny(v, "x509 signature")
		return nil
	}
	k, err := p.verify(t, push)
	var unknown errUnknownSigner
	switch {
	case errors.As(err, &unknown):
		deny(p.notAllowed(rules, "a key that is not in TAGPOLICY ("+unknown.keyID+")"), err.Error())
		return nil
	case err != nil:
		reason := strings.TrimPrefix(err.Error(), errBadSignature.Error()+": ")
		v := module.Violation{Code: module.CodeTagSignatureInvalid, Facts: []string{reason}}
		if strings.Contains(reason, "expired") {
			v.Hint = "extend the key (gpg --quick-set-expire), update its entry in TAGPOLICY and sign the tag again"
		}
		deny(v, err.Error())
		return nil
	}
	res.Signatures = append(res.Signatures, module.Signature{Ref: u.Ref, Rules: ruleList(rules), Signer: k.label, Fingerprint: k.fingerprint})
	for _, r := range rules {
		if !r.allows(k.label) {
			v := p.notAllowed([]*rule{r}, fmt.Sprintf("key %q from TAGPOLICY (%s, %s)", k.label, k.format, k.fingerprint))
			v.Rule, v.Ref = r.name, u.Ref
			v.Reason = fmt.Sprintf("signer %s (%s) not in signers of %s", k.label, k.fingerprint, r.name)
			res.Violations = append(res.Violations, v)
		}
	}
	return nil
}

// unsigned is the denial of a tag without an accepted signature. GitLab
// cannot sign a tag that is created in the UI or through the API.
func (p *policy) unsigned(rules []*rule, name, protocol, why string) module.Violation {
	v := module.Violation{Code: module.CodeTagNotSigned, Facts: []string{why, "allowed signing keys: " + p.keysText(allowedSigners(rules))},
		Message: messages(rules),
		Hint:    fmt.Sprintf("how to fix: git tag -d %[1]s && git tag -s %[1]s -m %[1]q && git push origin %[1]s", name)}
	if protocol == "web" {
		v.Title = "tags of this rule must be signed, and GitLab cannot sign a tag created in the UI or API"
		v.Hint = fmt.Sprintf("create it with git tag -s %s and git push; then create the release for the existing tag", name)
	}
	return v
}

// notAllowed is the denial of a signature by a key the rules do not list.
func (p *policy) notAllowed(rules []*rule, signer string) module.Violation {
	return module.Violation{Code: module.CodeTagSignerNotAllowed,
		Facts:   []string{"signed with: " + signer, "allowed keys: " + p.keysText(allowedSigners(rules))},
		Message: messages(rules),
		Hint:    "ask the owner of an allowed key to sign the tag, or ask the people who may change TAGPOLICY to add your key"}
}

// allowedSigners are the labels every rule allows; empty when they have none in common.
func allowedSigners(rules []*rule) []string {
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
	return allowed
}

// keysText lists labels with the format of their keys, which the file states.
func (p *policy) keysText(labels []string) string {
	if len(labels) == 0 {
		return "none, the matching rules have no signer in common"
	}
	out := make([]string, 0, len(labels))
	for _, l := range labels {
		if k, ok := p.byLabel[l]; ok {
			out = append(out, fmt.Sprintf("%s (%s)", l, k.format))
		} else {
			out = append(out, l)
		}
	}
	return strings.Join(out, ", ")
}

func messages(rules []*rule) string {
	var out []string
	for _, r := range rules {
		if r.message != "" {
			out = append(out, r.message)
		}
	}
	return strings.Join(out, "\n")
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

func ruleList(rules []*rule) []string {
	names := make([]string, len(rules))
	for i, r := range rules {
		names[i] = r.name
	}
	return names
}

func ruleNames(rules []*rule) string { return strings.Join(ruleList(rules), ", ") }
