package tag

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
)

const oneRule = `  prod:
    match: ['/^(?:v.*)$/']
    message: m
    signers: [lead]
`

func parseErr(t *testing.T, data []byte) string {
	t.Helper()
	_, err := parsePolicy(data, nil)
	require.Error(t, err)
	return err.Error()
}

func TestParse(t *testing.T) {
	lead := newSSH(t, "ed25519").line
	p := mustParse(t, policyYAML(oneRule, map[string]string{"lead": lead}))
	require.Len(t, p.rules, 1)
	assert.True(t, p.rules[0].match.Matches("v1"))
	assert.False(t, p.rules[0].match.Matches("xv1"), "the regex is anchored as written")
	assert.Equal(t, []string{"file"}, p.rules[0].source)

	free := mustParse(t, policyYAML("  prod:\n    match: ['/v\\d+/']\n    signers: [lead]\n    message: m\n", map[string]string{"lead": lead}))
	assert.True(t, free.rules[0].match.Matches("build-v2"), "a regex without anchors is found anywhere, like =~ in GitLab CI")
	all := mustParse(t, policyYAML("  any:\n    match: all\n    signers: [lead]\n    message: m\n", map[string]string{"lead": lead}))
	assert.Len(t, all.matching("anything"), 1)

	rule := func(fields string) []byte {
		return policyYAML("  prod:\n    match: ['/^v/']\n    message: m\n"+fields, map[string]string{"lead": lead})
	}
	cases := map[string]struct {
		data []byte
		want string
	}{
		"version":         {[]byte("version: 2\nkeys:\n  lead: " + lead + "\n"), "version: 2"},
		"rules no keys":   {[]byte("version: 1\nrules:\n  prod: {match: all, signers: [lead], message: m}\n"), `signers: "lead" is not a label in keys of the file`},
		"unknown field":   {[]byte("version: 1\nkey:\n  lead: " + lead + "\n"), "key"},
		"rule name":       {policyYAML("  Prod:\n    match: all\n    signers: [lead]\n    message: m\n", map[string]string{"lead": lead}), `rule name "Prod" must match`},
		"tags are gone":   {rule("    signers: [lead]\n    tags: v.*\n"), `unknown field "tags"`},
		"no match":        {policyYAML("  prod:\n    signers: [lead]\n    message: m\n", map[string]string{"lead": lead}), "match: required"},
		"default":         {policyYAML("  prod:\n    match: default\n    signers: [lead]\n    message: m\n", map[string]string{"lead": lead}), "default is not allowed here"},
		"bad regex":       {policyYAML("  prod:\n    match: ['/v(\\d/']\n    signers: [lead]\n    message: m\n", map[string]string{"lead": lead}), "not a valid RE2 regex"},
		"backreference":   {policyYAML("  prod:\n    match: ['/(a)\\1/']\n    signers: [lead]\n    message: m\n", map[string]string{"lead": lead}), "not a valid RE2 regex"},
		"glob":            {policyYAML("  prod:\n    match: ['v*']\n    signers: [lead]\n    message: m\n", map[string]string{"lead": lead}), "a glob is not supported"},
		"no signers":      {rule("    signers: []\n"), "signers"},
		"unknown signer":  {rule("    signers: [nobody]\n"), `"nobody" is not a label in keys of the file`},
		"signer twice":    {rule("    signers: [lead, lead]\n"), "twice"},
		"no message":      {policyYAML("  prod:\n    match: all\n    signers: [lead]\n", map[string]string{"lead": lead}), "message: required"},
		"label":           {policyYAML("", map[string]string{"lead lead": lead}), "label"},
		"message":         {policyYAML("  prod:\n    match: all\n    signers: [lead]\n    message: "+strings.Repeat("я", 1001)+"\n", map[string]string{"lead": lead}), "message: 1001"},
		"not a key":       {policyYAML(oneRule, map[string]string{"lead": "hello"}), "not an OpenPGP"},
		"key not string":  {[]byte("version: 1\nkeys:\n  lead: [a]\n"), "keys.lead: expected a string, got a list"},
		"duplicate label": {[]byte("version: 1\nkeys:\n  lead: " + lead + "\n  lead: " + lead + "\n"), "lead"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Contains(t, parseErr(t, c.data), c.want)
		})
	}
	_, err := parsePolicy([]byte("version: 1\n"), nil)
	assert.NoError(t, err, "a file without rules needs no keys")
}

func TestParse_Presets(t *testing.T) {
	lead := newSSH(t, "ed25519").line
	s := NewSettings(map[string]any{"org-releases": map[string]any{"rules": map[string]any{
		"prod": map[string]any{"match": []any{`/^v\d+\.\d+\.\d+$/`}, "signers": []any{"release-lead"}, "protected": true, "message": "Prod releases."},
	}}})
	p, err := parsePolicy([]byte("version: 1\nextends: org-releases\nkeys:\n  release-lead: "+lead+"\n"), s)
	require.NoError(t, err, "signers of a preset refer to the keys of the file")
	require.Len(t, p.rules, 1)
	assert.Equal(t, []string{"preset:org-releases"}, p.rules[0].source)
	_, err = parsePolicy([]byte("version: 1\nextends: org-releases\n"), s)
	assert.ErrorContains(t, err, `rule prod: signers: "release-lead" is not a label in keys of the file`)
	_, err = parsePolicy([]byte("version: 1\nextends: org-other\n"), s)
	assert.EqualError(t, err, `extends: preset "org-other" is not defined in the server config`, "a missing preset breaks the file")
	assert.Equal(t, []string{"presets.tagpolicy.bad: rules: expected a mapping, got a number"},
		New(NewSettings(map[string]any{"bad": map[string]any{"rules": 1}})).PresetErrors())
}

func TestParse_Limits(t *testing.T) {
	keys := map[string]string{}
	for i := range maxKeys + 1 {
		keys[fmt.Sprintf("k%02d", i)] = newSSH(t, "ed25519").line
	}
	assert.Contains(t, parseErr(t, policyYAML("", keys)), "keys: 1 to 32")
	delete(keys, "k32")
	mustParse(t, policyYAML("", keys))

	lead := map[string]string{"lead": newSSH(t, "ed25519").line}
	var rules strings.Builder
	for i := range maxRules + 1 {
		fmt.Fprintf(&rules, "  r%d:\n    match: [v%d]\n    signers: [lead]\n    message: m\n", i, i)
	}
	assert.Contains(t, parseErr(t, policyYAML(rules.String(), lead)), "rules: 51 rules")

	big := append(policyYAML(oneRule, lead), "# "+strings.Repeat("x", maxPolicySize)+"\n"...)
	assert.Contains(t, parseErr(t, big), "limit")
	assert.Equal(t, 256<<10, New(nil).MaxPolicySize())
}

func TestAcceptance_PrivateKey(t *testing.T) {
	e := newPGP(t, pgpOptions{})
	for name, text := range map[string]string{
		"openpgp":        armorPrivate(t, e),
		"openssh":        fakePrivateKey("OPENSSH", "b3BlbnNzaC1rZXktdjEAAAAA"),
		"pem":            fakePrivateKey("RSA", "MIIBOgIBAAJBAK"),
		"after a public": armorPublic(t, e) + "\n" + armorPrivate(t, e),
	} {
		t.Run(name, func(t *testing.T) {
			msg := parseErr(t, policyYAML(oneRule, map[string]string{"lead": text}))
			assert.Contains(t, msg, "keys.lead: this is a private key")
			assert.Contains(t, msg, "treat the private key as exposed")
		})
	}
}

// fakePrivateKey builds a truncated private key block at run time, so that
// secret scanners do not take it for a real key.
func fakePrivateKey(typ, body string) string {
	const suffix = " PRIVATE" + " KEY-----"
	return "-----BEGIN " + typ + suffix + "\n" + body + "\n-----END " + typ + suffix
}

func TestAcceptance_Keys(t *testing.T) {
	e, other := newPGP(t, pgpOptions{}), newPGP(t, pgpOptions{})
	// The wire format of an ssh-dss key; only the sizes of P and Q matter.
	one := big.NewInt(1)
	dsaPub, err := ssh.ParsePublicKey(ssh.Marshal(struct {
		Name       string
		P, Q, G, Y *big.Int
	}{"ssh-dss", new(big.Int).Lsh(one, 1023), new(big.Int).Lsh(one, 159), one, one}))
	require.NoError(t, err)
	ed := newSSH(t, "ed25519")
	cert := &ssh.Certificate{Key: ed.signer.PublicKey(), CertType: ssh.UserCert, ValidBefore: ssh.CertTimeInfinity}
	require.NoError(t, cert.SignCert(rand.Reader, ed.signer))

	cases := map[string]struct {
		key, want string
	}{
		"two keys in a block": {armorTwo(t, e, other), "exactly one"},
		"two blocks":          {armorPublic(t, e) + "\n" + armorPublic(t, other), "exactly one armored public key block"},
		"text after block":    {armorPublic(t, e) + "\ntrailing", "exactly one armored public key block"},
		"signature block":     {"-----BEGIN PGP SIGNATURE-----\nabc\n-----END PGP SIGNATURE-----", "armored public key block"},
		"ssh-dss":             {strings.TrimSpace(string(ssh.MarshalAuthorizedKey(dsaPub))), "ssh-dss is not supported"},
		"certificate":         {strings.TrimSpace(string(ssh.MarshalAuthorizedKey(cert))), "certificates are not supported"},
		"options":             {`no-pty ` + ed.line, "without options"},
		"two lines":           {ed.line + "\n" + ed.line, "one line"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Contains(t, parseErr(t, policyYAML(oneRule, map[string]string{"lead": c.key})), c.want)
		})
	}

	// sk keys are accepted: their public keys come from testdata/sk.
	for _, f := range []string{"sk-ed25519.pub", "sk-ecdsa.pub"} {
		k, err := parseKey("lead", readTestdata(t, "sk", f))
		require.NoError(t, err, f)
		assert.Equal(t, formatSSH, k.format)
	}
}

// armorTwo is one armored block with two public keys.
func armorTwo(t *testing.T, a, b *openpgp.Entity) string {
	t.Helper()
	var buf bytes.Buffer
	w, err := armor.Encode(&buf, openpgp.PublicKeyType, nil)
	require.NoError(t, err)
	require.NoError(t, a.Serialize(w))
	require.NoError(t, b.Serialize(w))
	require.NoError(t, w.Close())
	return buf.String()
}

func readTestdata(t *testing.T, elem ...string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(append([]string{"testdata"}, elem...)...))
	require.NoError(t, err)
	return string(data)
}

func TestAcceptance_NotSigning(t *testing.T) {
	e := newPGP(t, pgpOptions{})
	self, _ := e.PrimarySelfSignature()
	self.FlagSign = false
	assert.False(t, canSign(e))
}

func TestAcceptance_DuplicateFingerprint(t *testing.T) {
	e := newPGP(t, pgpOptions{})
	ed := newSSH(t, "ed25519")
	msg := parseErr(t, policyYAML(oneRule, map[string]string{"lead": armorPublic(t, e), "other": armorPublic(t, e)}))
	assert.Contains(t, msg, "keys: keys.other is the same key as keys.lead")
	msg = parseErr(t, policyYAML(oneRule, map[string]string{"lead": ed.line, "other": strings.TrimSuffix(ed.line, " ed25519") + " another comment"}))
	assert.Contains(t, msg, "the same key as keys.lead")
}

func TestAcceptance_SignersAndKeys(t *testing.T) {
	lead, spare := newSSH(t, "ed25519"), newSSH(t, "ed25519")
	assert.Contains(t, parseErr(t, policyYAML("  prod:\n    match: ['/^(?:v.*)$/']\n    message: m\n    signers: [nobody]\n", map[string]string{"lead": lead.line})),
		"rule prod: signers: \"nobody\" is not a label in keys of the file")

	p := mustParse(t, policyYAML(oneRule, map[string]string{"lead": lead.line, "spare": spare.line}))
	problems := p.ValidateVersion(nil, now)
	require.Len(t, problems, 1)
	assert.True(t, problems[0].Warning)
	assert.Equal(t, "keys.spare is not used by any rule", problems[0].Msg)
}

func TestAcceptance_ExpiredKey(t *testing.T) {
	expired := armorPublic(t, newPGP(t, pgpOptions{created: now.Add(-48 * time.Hour), lifetime: 24 * time.Hour}))
	valid := armorPublic(t, newPGP(t, pgpOptions{}))
	ssh1 := newSSH(t, "ed25519").line
	rules := `  prod:
    match: ['/^(?:v.*)$/']
    message: m
    signers: [lead, old, dev]
`
	anchor := mustParse(t, policyYAML(rules, map[string]string{"lead": valid, "old": expired, "dev": ssh1}))

	// The expired key was already there: a warning, the edit goes through.
	edited := mustParse(t, policyYAML(strings.Replace(rules, "message: m", "message: edited", 1), map[string]string{"lead": valid, "old": expired, "dev": ssh1}))
	problems := edited.ValidateVersion(anchor, now)
	require.Len(t, problems, 1)
	assert.True(t, problems[0].Warning)
	assert.Contains(t, problems[0].Msg, "keys.old: the key")
	assert.Contains(t, problems[0].Msg, "expired")

	// Added under a new label or changed under the old one: denied.
	other := armorPublic(t, newPGP(t, pgpOptions{created: now.Add(-48 * time.Hour), lifetime: time.Hour}))
	for name, keys := range map[string]map[string]string{
		"added":   {"lead": valid, "old": expired, "dev": ssh1, "new": other},
		"changed": {"lead": valid, "old": other, "dev": ssh1},
	} {
		t.Run(name, func(t *testing.T) {
			r := rules
			if name == "added" {
				r = strings.Replace(rules, "dev]", "dev, new]", 1)
			}
			problems := mustParse(t, policyYAML(r, keys)).ValidateVersion(anchor, now)
			var errs int
			for _, pr := range problems {
				if !pr.Warning {
					errs++
				}
			}
			assert.Equal(t, 1, errs, problems)
		})
	}

	// Without an anchor version every expired key is new; on the anchor
	// expiry does not make the policy invalid.
	assert.False(t, anchor.ValidateVersion(nil, now)[0].Warning)
	mustParse(t, policyYAML(rules, map[string]string{"lead": valid, "old": expired, "dev": ssh1}))
}

func TestHelpsAndSources(t *testing.T) {
	ci := module.Help{Contact: "#release"}
	rules := []*rule{
		{help: ci, source: []string{"file"}},
		{help: ci, source: []string{"preset:b", "file"}},
		{source: []string{"preset:a"}},
	}
	assert.Equal(t, []module.Help{ci}, helps(rules), "the same help once")
	assert.Equal(t, []string{"preset:b", "preset:a", "file"}, sources(rules), "presets first, then the file")
	assert.Nil(t, helps(rules[2:]))
}

// notTag is a policy of another module.
type notTag struct{ module.Policy }

func TestRulesFor_OtherPolicy(t *testing.T) {
	_, ok := RulesFor(notTag{}, "v1.0.0")
	assert.False(t, ok)
}
