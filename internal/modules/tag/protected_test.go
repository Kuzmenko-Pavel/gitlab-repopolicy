package tag

import (
	"context"
	"crypto"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
)

type fakeRefs struct {
	class  module.RefClass
	status module.Status
	asked  int
}

func (r *fakeRefs) Default() string { return "refs/heads/main" }
func (r *fakeRefs) Class(context.Context, string) (module.RefClass, module.Status) {
	r.asked++
	return r.class, r.status
}
func (r *fakeRefs) ProtectedScope(context.Context) (module.Scope, module.Status) {
	return module.Scope{}, r.status
}

const protectedRules = `  - name: release
    tags: 'v\d+\.\d+\.\d+'
    signers: [release-lead]
    protected: true
    message: Protect v* in Settings > Repository > Protected tags.
  - name: any
    tags: 'v.*'
    signers: [release-lead]
`

// pushTag evaluates one tag update from old to the tag object data with refs.
func pushTag(t *testing.T, p *policy, refs module.RefClassifier, old, ref string, data []byte) *module.Result {
	t.Helper()
	oid := fmt.Sprintf("%040x", sha256.Sum256(data))[:40]
	in := &module.Input{
		Updates: []module.RefUpdate{{Old: old, New: oid, Ref: ref}},
		Objects: fakeObjects{oid: {OID: oid, Type: "tag", Data: data}},
		Refs:    refs,
		Clock:   at(now),
	}
	res, err := p.Evaluate(context.Background(), in)
	require.NoError(t, err)
	return res
}

func TestEvaluate_Protected(t *testing.T) {
	lead, other := newPGP(t, pgpOptions{}), newPGP(t, pgpOptions{})
	p := mustParse(t, policyYAML(protectedRules, map[string]string{"release-lead": armorPublic(t, lead)}))
	signed := signPGP(t, lead, tagPayload("v1.4.0", "commit"), now.Add(-time.Minute), crypto.SHA256)
	zero, ref := strings.Repeat("0", 40), "refs/tags/v1.4.0"

	refs := &fakeRefs{class: module.ClassProtected, status: module.Confirmed()}
	res := pushTag(t, p, refs, zero, ref, signed)
	assert.Equal(t, module.Allow, res.Decision)
	assert.Equal(t, 1, refs.asked)

	refs = &fakeRefs{class: module.ClassOther, status: module.Confirmed()}
	res = pushTag(t, p, refs, zero, ref, signed)
	assert.Equal(t, module.Deny, res.Decision)
	require.Equal(t, []string{module.CodeTagNotProtected}, codes(res))
	v := res.Violations[0]
	assert.Equal(t, "release", v.Rule, "only the rules with protected are named")
	assert.Contains(t, v.Message, "Protect v* in Settings")
	assert.Contains(t, v.Summary, "tag v1.4.0 must be protected (TAGPOLICY rule release)")

	for status, code := range map[module.Status]string{
		module.Unknown("no answer"):   module.CodeAuthorizationBackend,
		module.UnknownLimit("budget"): module.CodeLimitExceeded,
	} {
		res = pushTag(t, p, &fakeRefs{status: status}, zero, ref, signed)
		assert.Equal(t, module.Warn, res.Decision, "without an answer the tag is accepted")
		assert.Equal(t, []string{code}, codes(res))
	}

	refs = &fakeRefs{class: module.ClassOther, status: module.Confirmed()}
	res = pushTag(t, p, refs, zero, "refs/tags/v1.4.0-rc.1", signPGP(t, lead, tagPayload("v1.4.0-rc.1", "commit"), now.Add(-time.Minute), crypto.SHA256))
	assert.Equal(t, module.Allow, res.Decision, "only the rule any matches, it has no protected")
	assert.Zero(t, refs.asked, "rules without protected do not ask GitLab")

	refs = &fakeRefs{class: module.ClassOther, status: module.Confirmed()}
	res = pushTag(t, p, refs, strings.Repeat("1", 40), ref, signed)
	assert.Equal(t, module.Allow, res.Decision, "a moved tag is not checked for protection")
	assert.Zero(t, refs.asked)

	res = pushTag(t, p, &fakeRefs{class: module.ClassOther, status: module.Confirmed()}, zero, ref,
		signPGP(t, other, tagPayload("v1.4.0", "commit"), now.Add(-time.Minute), crypto.SHA256))
	assert.Equal(t, module.Deny, res.Decision)
	assert.ElementsMatch(t, []string{module.CodeTagSignerNotAllowed, module.CodeTagNotProtected}, codes(res), "both problems are reported")
}
