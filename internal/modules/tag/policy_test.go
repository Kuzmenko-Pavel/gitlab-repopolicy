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

const oneRule = `  - name: prod
    tags: 'v.*'
    signers: [lead]
`

func parseErr(t *testing.T, data []byte) string {
	t.Helper()
	_, err := parsePolicy(data)
	require.Error(t, err)
	return err.Error()
}

func TestParse(t *testing.T) {
	lead := newSSH(t, "ed25519").line
	p := mustParse(t, policyYAML(oneRule, map[string]string{"lead": lead}))
	require.Len(t, p.rules, 1)
	assert.True(t, p.rules[0].tags.MatchString("v1"))
	assert.False(t, p.rules[0].tags.MatchString("xv1"), "the pattern is anchored at both ends")
	assert.False(t, p.rules[0].tags.MatchString("v1\n"))

	alt := `  - name: prod
    tags: 'a|b'
    signers: [lead]
`
	p = mustParse(t, policyYAML(alt, map[string]string{"lead": lead}))
	assert.False(t, p.rules[0].tags.MatchString("ab"), "alternatives are grouped before anchoring")

	cases := map[string]struct {
		data []byte
		want string
	}{
		"version":         {[]byte("version: 2\nkeys:\n  lead: " + lead + "\n"), "version: 2"},
		"no keys":         {[]byte("version: 1\nrules: []\n"), "keys: 1 to 32"},
		"unknown field":   {[]byte("version: 1\nkey:\n  lead: " + lead + "\n"), "key"},
		"rule name":       {policyYAML("  - name: Prod\n    tags: v.*\n    signers: [lead]\n", map[string]string{"lead": lead}), "name"},
		"duplicate rule":  {policyYAML(oneRule+oneRule, map[string]string{"lead": lead}), "used twice"},
		"empty tags":      {policyYAML("  - name: prod\n    tags: ''\n    signers: [lead]\n", map[string]string{"lead": lead}), "tags"},
		"bad regex":       {policyYAML("  - name: prod\n    tags: 'v(\\d'\n    signers: [lead]\n", map[string]string{"lead": lead}), "tags"},
		"backreference":   {policyYAML("  - name: prod\n    tags: '(a)\\1'\n    signers: [lead]\n", map[string]string{"lead": lead}), "tags"},
		"no signers":      {policyYAML("  - name: prod\n    tags: v.*\n    signers: []\n", map[string]string{"lead": lead}), "signers"},
		"unknown signer":  {policyYAML("  - name: prod\n    tags: v.*\n    signers: [nobody]\n", map[string]string{"lead": lead}), `"nobody" is not a label`},
		"signer twice":    {policyYAML("  - name: prod\n    tags: v.*\n    signers: [lead, lead]\n", map[string]string{"lead": lead}), "twice"},
		"label":           {policyYAML("", map[string]string{"lead lead": lead}), "label"},
		"message":         {policyYAML("  - name: prod\n    tags: v.*\n    signers: [lead]\n    message: "+strings.Repeat("я", 1001)+"\n", map[string]string{"lead": lead}), "message: 1001"},
		"not a key":       {policyYAML(oneRule, map[string]string{"lead": "hello"}), "not an OpenPGP"},
		"key not string":  {[]byte("version: 1\nkeys:\n  lead: [a]\n"), "cannot unmarshal"},
		"duplicate label": {[]byte("version: 1\nkeys:\n  lead: " + lead + "\n  lead: " + lead + "\n"), "lead"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Contains(t, parseErr(t, c.data), c.want)
		})
	}
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
		fmt.Fprintf(&rules, "  - name: r%d\n    tags: 'v%d'\n    signers: [lead]\n", i, i)
	}
	assert.Contains(t, parseErr(t, policyYAML(rules.String(), lead)), "rules: 51 rules")

	long := "  - name: prod\n    tags: '" + strings.Repeat("a", maxPatternBytes) + "'\n    signers: [lead]\n"
	mustParse(t, policyYAML(long, lead))
	long = "  - name: prod\n    tags: '" + strings.Repeat("a", maxPatternBytes+1) + "'\n    signers: [lead]\n"
	assert.Contains(t, parseErr(t, policyYAML(long, lead)), "257 bytes")

	big := append(policyYAML(oneRule, lead), "# "+strings.Repeat("x", maxPolicySize)+"\n"...)
	assert.Contains(t, parseErr(t, big), "limit")
	assert.Equal(t, 256<<10, module.MaxPolicySize(New()))
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
	assert.Contains(t, msg, "keys.other: the same key as keys.lead")
	msg = parseErr(t, policyYAML(oneRule, map[string]string{"lead": ed.line, "other": strings.TrimSuffix(ed.line, " ed25519") + " another comment"}))
	assert.Contains(t, msg, "the same key as keys.lead")
}

func TestAcceptance_SignersAndKeys(t *testing.T) {
	lead, spare := newSSH(t, "ed25519"), newSSH(t, "ed25519")
	assert.Contains(t, parseErr(t, policyYAML("  - name: prod\n    tags: v.*\n    signers: [nobody]\n", map[string]string{"lead": lead.line})),
		"rule prod: signers: \"nobody\" is not a label in keys")

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
	rules := `  - name: prod
    tags: 'v.*'
    signers: [lead, old, dev]
`
	anchor := mustParse(t, policyYAML(rules, map[string]string{"lead": valid, "old": expired, "dev": ssh1}))

	// The expired key was already there: a warning, the edit goes through.
	edited := mustParse(t, policyYAML(rules+"    message: edited\n", map[string]string{"lead": valid, "old": expired, "dev": ssh1}))
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
