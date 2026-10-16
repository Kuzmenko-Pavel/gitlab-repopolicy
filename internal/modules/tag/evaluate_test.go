package tag

import (
	"bytes"
	"context"
	"crypto"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
)

const twoRules = `  - name: staging
    tags: 'v\d+\.\d+\.\d+-staging'
    signers: [release-lead, dev-lead]
  - name: prod
    tags: 'v\d+\.\d+\.\d+-prod'
    signers: [release-lead]
    message: Prod releases are signed by release-lead.
`

func TestVerify_OpenPGPAllowed(t *testing.T) {
	lead := newPGP(t, pgpOptions{})
	p := mustParse(t, policyYAML(twoRules, map[string]string{"release-lead": armorPublic(t, lead), "dev-lead": newSSH(t, "ed25519").line}))
	res := push(t, p, "refs/tags/v1.2.3-prod", "tag", signPGP(t, lead, tagPayload("v1.2.3-prod", "commit"), now.Add(-time.Minute), crypto.SHA256))
	assert.Equal(t, module.Allow, res.Decision)
	assert.True(t, res.Touched)
	require.Len(t, res.Signatures, 1)
	assert.Equal(t, module.Signature{Ref: "refs/tags/v1.2.3-prod", Rules: []string{"prod"}, Signer: "release-lead",
		Fingerprint: fmt.Sprintf("%X", lead.PrimaryKey.Fingerprint)}, res.Signatures[0])
}

func TestVerify_SignerFromOtherRule(t *testing.T) {
	lead, dev := newPGP(t, pgpOptions{}), newPGP(t, pgpOptions{})
	p := mustParse(t, policyYAML(twoRules, map[string]string{"release-lead": armorPublic(t, lead), "dev-lead": armorPublic(t, dev)}))
	res := push(t, p, "refs/tags/v1.2.3-prod", "tag", signPGP(t, dev, tagPayload("v1.2.3-prod", "commit"), now, crypto.SHA256))
	assert.Equal(t, module.Deny, res.Decision)
	require.Len(t, res.Violations, 1)
	v := res.Violations[0]
	assert.Equal(t, module.CodeTagSignerNotAllowed, v.Code)
	assert.Equal(t, "prod", v.Rule)
	assert.Contains(t, v.Message, "Prod releases are signed by release-lead.")
	assert.Contains(t, v.Message, fmt.Sprintf("signed by: dev-lead (%X)", dev.PrimaryKey.Fingerprint))
	assert.Contains(t, v.Message, "allowed:   release-lead")
	require.Len(t, res.Signatures, 1, "the signer goes to audit also on a denial")
	assert.Equal(t, "dev-lead", res.Signatures[0].Signer)

	// The same signer is allowed by the staging rule.
	res = push(t, p, "refs/tags/v1.2.3-staging", "tag", signPGP(t, dev, tagPayload("v1.2.3-staging", "commit"), now, crypto.SHA256))
	assert.Equal(t, module.Allow, res.Decision)
}

func TestVerify_KeyNotInFile(t *testing.T) {
	lead, stranger := newPGP(t, pgpOptions{}), newPGP(t, pgpOptions{})
	p := mustParse(t, policyYAML(twoRules, map[string]string{"release-lead": armorPublic(t, lead), "dev-lead": newSSH(t, "ed25519").line}))
	res := push(t, p, "refs/tags/v1.2.3-prod", "tag", signPGP(t, stranger, tagPayload("v1.2.3-prod", "commit"), now, crypto.SHA256))
	assert.Equal(t, []string{module.CodeTagSignerNotAllowed}, codes(res))
	assert.Contains(t, res.Violations[0].Message, "a key that is not in TAGPOLICY")
	assert.Empty(t, res.Signatures)

	other := newSSH(t, "ed25519")
	res = push(t, p, "refs/tags/v1.2.3-prod", "tag", append(tagPayload("v1.2.3-prod", "commit"), sshSign(t, other, tagPayload("v1.2.3-prod", "commit"), "git", "sha512", "")...))
	assert.Equal(t, []string{module.CodeTagSignerNotAllowed}, codes(res))
	assert.Contains(t, res.Violations[0].Message, ssh.FingerprintSHA256(other.signer.PublicKey()))
}

func TestTagObject(t *testing.T) {
	lead := newPGP(t, pgpOptions{})
	p := mustParse(t, policyYAML(twoRules, map[string]string{"release-lead": armorPublic(t, lead), "dev-lead": newSSH(t, "ed25519").line}))
	signed := func(name, typ string) []byte { return signPGP(t, lead, tagPayload(name, typ), now, crypto.SHA256) }

	t.Run("lightweight", func(t *testing.T) {
		res := push(t, p, "refs/tags/v1.2.3-prod", "commit", []byte("tree 4b825dc642cb6eb9a060e54bf8d69288fbee4904\n\nmsg\n"))
		assert.Equal(t, []string{module.CodeTagNotSigned}, codes(res))
	})
	t.Run("unsigned", func(t *testing.T) {
		res := push(t, p, "refs/tags/v1.2.3-prod", "tag", tagPayload("v1.2.3-prod", "commit"))
		assert.Equal(t, []string{module.CodeTagNotSigned}, codes(res))
	})
	t.Run("name mismatch", func(t *testing.T) {
		// A signed staging tag object pushed under a prod name.
		res := push(t, p, "refs/tags/v1.2.3-prod", "tag", signed("v1.2.3-staging", "commit"))
		assert.Equal(t, []string{module.CodeTagObjectInvalid}, codes(res))
		assert.Contains(t, res.Violations[0].Message, `named "v1.2.3-staging"`)
	})
	t.Run("tag of tag", func(t *testing.T) {
		res := push(t, p, "refs/tags/v1.2.3-prod", "tag", signed("v1.2.3-prod", "tag"))
		assert.Equal(t, []string{module.CodeTagObjectInvalid}, codes(res))
	})
	t.Run("malformed", func(t *testing.T) {
		res := push(t, p, "refs/tags/v1.2.3-prod", "tag", []byte("type commit\nobject x\ntag v1.2.3-prod\n\n"))
		assert.Equal(t, []string{module.CodeTagObjectInvalid}, codes(res))
	})
	t.Run("too large", func(t *testing.T) {
		res := push(t, p, "refs/tags/v1.2.3-prod", "tag", append(tagPayload("v1.2.3-prod", "commit"), make([]byte, maxTagObject)...))
		assert.Equal(t, []string{module.CodeTagObjectInvalid}, codes(res))
	})
	t.Run("payload changed", func(t *testing.T) {
		data := signed("v1.2.3-prod", "commit")
		data = []byte(strings.Replace(string(data), "Release v1.2.3-prod", "Release v9.9.9-prod", 1))
		res := push(t, p, "refs/tags/v1.2.3-prod", "tag", data)
		assert.Equal(t, []string{module.CodeTagSignatureInvalid}, codes(res))
	})
	t.Run("text after signature", func(t *testing.T) {
		res := push(t, p, "refs/tags/v1.2.3-prod", "tag", append(signed("v1.2.3-prod", "commit"), "unsigned\n"...))
		assert.Equal(t, []string{module.CodeTagSignatureInvalid}, codes(res))
	})
	t.Run("quoted block in the message", func(t *testing.T) {
		// git takes the last signature block; a quoted one belongs to the payload.
		payload := append(tagPayload("v1.2.3-prod", "commit"), "-----BEGIN SSH SIGNATURE-----\nquoted\n-----END SSH SIGNATURE-----\n"...)
		res := push(t, p, "refs/tags/v1.2.3-prod", "tag", signPGPPayload(t, lead, payload))
		assert.Equal(t, module.Allow, res.Decision, res.Violations)
	})
	t.Run("outside rules", func(t *testing.T) {
		res := push(t, p, "refs/tags/v1.2.3", "tag", tagPayload("v1.2.3", "commit"))
		assert.Equal(t, module.Allow, res.Decision)
		assert.False(t, res.Touched)
	})
	t.Run("missing object", func(t *testing.T) {
		in := &module.Input{Updates: []module.RefUpdate{{Old: strings.Repeat("0", 40), New: strings.Repeat("a", 40), Ref: "refs/tags/v1.2.3-prod"}},
			Objects: fakeObjects{}, Clock: at(now)}
		_, err := p.Evaluate(context.Background(), in)
		assert.True(t, errors.Is(err, module.ErrMissing), "a read failure is an error, the core denies (FailClosed)")
	})
}

func TestTagObject_MoveAndDelete(t *testing.T) {
	lead := newPGP(t, pgpOptions{})
	p := mustParse(t, policyYAML(twoRules, map[string]string{"release-lead": armorPublic(t, lead), "dev-lead": newSSH(t, "ed25519").line}))
	in := &module.Input{
		Updates: []module.RefUpdate{
			{Old: strings.Repeat("b", 40), New: strings.Repeat("c", 40), Ref: "refs/tags/v1.0.0-prod"},
			{Old: strings.Repeat("b", 40), New: strings.Repeat("0", 40), Ref: "refs/tags/v0.9.0-prod"},
			{Old: strings.Repeat("0", 40), New: strings.Repeat("d", 40), Ref: "refs/heads/v1.0.0-prod"},
		},
		Objects: fakeObjects{strings.Repeat("c", 40): {Type: "tag", Data: tagPayload("v1.0.0-prod", "commit")}},
		Clock:   at(now),
	}
	res, err := p.Evaluate(context.Background(), in)
	require.NoError(t, err)
	require.Len(t, res.Violations, 1, "a move is checked like a creation; deletion and branches are not regulated")
	assert.Equal(t, "refs/tags/v1.0.0-prod", res.Violations[0].Ref)
	assert.Equal(t, module.CodeTagNotSigned, res.Violations[0].Code)
}

func TestVerify_X509(t *testing.T) {
	p := mustParse(t, policyYAML(twoRules, map[string]string{"release-lead": newSSH(t, "ed25519").line, "dev-lead": newSSH(t, "ed25519").line}))
	data := append(tagPayload("v1.2.3-prod", "commit"), "-----BEGIN SIGNED MESSAGE-----\nMIIB\n-----END SIGNED MESSAGE-----\n"...)
	res := push(t, p, "refs/tags/v1.2.3-prod", "tag", data)
	assert.Equal(t, []string{module.CodeTagNotSigned}, codes(res))
	assert.Contains(t, res.Violations[0].Message, "OpenPGP and SSH signatures are supported")
}

func TestVerify_ExpiredKey(t *testing.T) {
	cases := []struct {
		name string
		opts pgpOptions
	}{
		{"primary", pgpOptions{created: now.Add(-48 * time.Hour), lifetime: 24 * time.Hour}},
		{"subkey", pgpOptions{subkey: true, subLifetime: time.Hour}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newPGP(t, c.opts)
			p := mustParse(t, policyYAML(twoRules, map[string]string{"release-lead": armorPublic(t, e), "dev-lead": newSSH(t, "ed25519").line}))
			created := c.opts.created
			if created.IsZero() {
				created = now.Add(-24 * time.Hour)
			}
			res := push(t, p, "refs/tags/v1.2.3-prod", "tag", signPGP(t, e, tagPayload("v1.2.3-prod", "commit"), created.Add(time.Minute), crypto.SHA256))
			require.Equal(t, []string{module.CodeTagSignatureInvalid}, codes(res))
			assert.Contains(t, res.Violations[0].Message, "expired")
		})
	}
}

// TestVerify_KeyExpiryAtPushTime: the key must be valid at the push, or at the
// signature when it is made up to five minutes after the push.
func TestVerify_KeyExpiryAtPushTime(t *testing.T) {
	payload := tagPayload("v1.2.3-prod", "commit")
	expiring := func(after time.Duration) *openpgp.Entity {
		return newPGP(t, pgpOptions{created: now.Add(-24 * time.Hour), lifetime: 24*time.Hour + after})
	}
	cases := []struct {
		name   string
		key    *openpgp.Entity
		signed time.Time
		want   string
	}{
		{"valid two minutes after the push", expiring(2 * time.Minute), now.Add(-time.Minute), ""},
		{"expired a minute before the push", expiring(-time.Minute), now.Add(-2 * time.Minute), "the signing key is expired"},
		{"signed after the push, valid then", expiring(5 * time.Minute), now.Add(4 * time.Minute), ""},
		{"signed more than five minutes after the push", expiring(time.Hour), now.Add(6 * time.Minute), "made after the push"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data := signPGP(t, c.key, payload, c.signed, crypto.SHA256)
			_, err := verifyOpenPGP(openpgp.EntityList{c.key}, payload, data[len(payload):], now)
			if c.want == "" {
				assert.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, c.want)
		})
	}
}

// A revoked key cannot be accepted into TAGPOLICY, so the revocation is
// checked on the verification itself.
func TestVerify_RevokedKey(t *testing.T) {
	e := newPGP(t, pgpOptions{})
	payload := tagPayload("v1.2.3-prod", "commit")
	data := signPGP(t, e, payload, now.Add(-time.Hour), crypto.SHA256)
	require.NoError(t, e.RevokeKey(packet.KeyCompromised, "compromised", pgpConfig(now.Add(-30*time.Minute), 0)))
	_, err := verifyOpenPGP(openpgp.EntityList{e}, payload, data[len(payload):], now)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "revoked")

	_, err = parseKey("release-lead", armorPublic(t, e))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "revoked")
}

func TestVerify_Subkey(t *testing.T) {
	e := newPGP(t, pgpOptions{subkey: true})
	p := mustParse(t, policyYAML(twoRules, map[string]string{"release-lead": armorPublic(t, e), "dev-lead": newSSH(t, "ed25519").line}))
	data := signPGP(t, e, tagPayload("v1.2.3-prod", "commit"), now, crypto.SHA256)
	assert.Contains(t, string(data), "-----BEGIN PGP SIGNATURE-----")
	key, ok := e.SigningKey(now)
	require.True(t, ok)
	require.NotEqual(t, e.PrimaryKey.KeyId, key.PublicKey.KeyId, "signed by the subkey")
	res := push(t, p, "refs/tags/v1.2.3-prod", "tag", data)
	assert.Equal(t, module.Allow, res.Decision)
	assert.Equal(t, fmt.Sprintf("%X", e.PrimaryKey.Fingerprint), res.Signatures[0].Fingerprint, "the fingerprint of the primary key")
}

func TestVerify_OpenPGPTimeAndHash(t *testing.T) {
	e := newPGP(t, pgpOptions{})
	p := mustParse(t, policyYAML(twoRules, map[string]string{"release-lead": armorPublic(t, e), "dev-lead": newSSH(t, "ed25519").line}))
	payload := tagPayload("v1.2.3-prod", "commit")

	res := push(t, p, "refs/tags/v1.2.3-prod", "tag", signPGP(t, e, payload, now.Add(4*time.Minute), crypto.SHA256))
	assert.Equal(t, module.Allow, res.Decision, "a clock up to 5 minutes ahead is accepted")

	res = push(t, p, "refs/tags/v1.2.3-prod", "tag", signPGP(t, e, payload, now.Add(6*time.Minute), crypto.SHA256))
	assert.Equal(t, []string{module.CodeTagSignatureInvalid}, codes(res))

	res = push(t, p, "refs/tags/v1.2.3-prod", "tag", signPGP(t, e, payload, now, crypto.SHA512))
	assert.Equal(t, module.Allow, res.Decision)

	// go-crypto does not make SHA-1 signatures: TestVerify_RealTools covers
	// one made by gpg. MD5 signatures it does not even parse.
	sig := signPGP(t, e, payload, now, crypto.SHA256)[len(payload):]
	block, err := armor.Decode(bytes.NewReader(sig))
	require.NoError(t, err)
	raw, err := io.ReadAll(block.Body)
	require.NoError(t, err)
	require.Equal(t, byte(4), raw[2], "a v4 signature packet with a one-byte length")
	raw[5] = 1 // MD5
	var b bytes.Buffer
	w, err := armor.Encode(&b, "PGP SIGNATURE", nil)
	require.NoError(t, err)
	_, err = w.Write(raw)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	res = push(t, p, "refs/tags/v1.2.3-prod", "tag", append(payload, b.Bytes()...))
	require.Equal(t, []string{module.CodeTagSignatureInvalid}, codes(res))
	assert.Contains(t, res.Violations[0].Message, "hash function 1")
}

func TestVerify_SSH(t *testing.T) {
	payload := tagPayload("v1.2.3-prod", "commit")
	allowed := []struct{ kind, algo string }{
		{"ed25519", ""}, {"p256", ""}, {"p384", ""}, {"p521", ""},
		{"rsa", ssh.KeyAlgoRSASHA256}, {"rsa", ssh.KeyAlgoRSASHA512},
	}
	for _, c := range allowed {
		name := c.kind
		if c.algo != "" {
			name = c.algo
		}
		t.Run(name, func(t *testing.T) {
			k := newSSH(t, c.kind)
			p := mustParse(t, policyYAML(twoRules, map[string]string{"release-lead": k.line, "dev-lead": newSSH(t, "ed25519").line}))
			for _, hash := range []string{"sha256", "sha512"} {
				res := push(t, p, "refs/tags/v1.2.3-prod", "tag", append(payload, sshSign(t, k, payload, "git", hash, c.algo)...))
				assert.Equal(t, module.Allow, res.Decision, hash)
				assert.Equal(t, ssh.FingerprintSHA256(k.signer.PublicKey()), res.Signatures[0].Fingerprint)
			}
		})
	}

	k := newSSH(t, "ed25519")
	rsaKey := newSSH(t, "rsa")
	p := mustParse(t, policyYAML(twoRules, map[string]string{"release-lead": k.line, "dev-lead": rsaKey.line}))
	denied := []struct {
		name string
		data []byte
		want string
	}{
		{"namespace", append(payload, sshSign(t, k, payload, "file", "sha512", "")...), "namespace"},
		{"sha1", append(tagPayload("v1.2.3-staging", "commit"), sshSign(t, rsaKey, tagPayload("v1.2.3-staging", "commit"), "git", "sha512", ssh.KeyAlgoRSA)...), "rsa-sha2-256"},
		{"hash", append(payload, sshSign(t, k, payload, "git", "sha1", "")...), "sha1"},
		{"other payload", append(payload, sshSign(t, k, tagPayload("v9.9.9-prod", "commit"), "git", "sha512", "")...), "does not match"},
		{"not base64", append(payload, "-----BEGIN SSH SIGNATURE-----\n!!!\n-----END SSH SIGNATURE-----\n"...), "base64"},
		{"truncated", append(payload, "-----BEGIN SSH SIGNATURE-----\nU1NIU0lH\n"...), "malformed"},
	}
	for _, c := range denied {
		t.Run(c.name, func(t *testing.T) {
			ref := "refs/tags/v1.2.3-prod"
			if c.name == "sha1" {
				ref = "refs/tags/v1.2.3-staging"
			}
			res := push(t, p, ref, "tag", c.data)
			require.Equal(t, []string{module.CodeTagSignatureInvalid}, codes(res))
			assert.Contains(t, res.Violations[0].Message, c.want)
		})
	}
}

func TestEvaluate_OverlapAnd(t *testing.T) {
	a, b := newSSH(t, "ed25519"), newSSH(t, "ed25519")
	rules := `  - name: all-releases
    tags: 'v.*'
    signers: [a, b]
  - name: prod
    tags: 'v.*-prod'
    signers: [b]
`
	p := mustParse(t, policyYAML(rules, map[string]string{"a": a.line, "b": b.line}))
	payload := tagPayload("v1-prod", "commit")
	res := push(t, p, "refs/tags/v1-prod", "tag", append(payload, sshSign(t, a, payload, "git", "sha512", "")...))
	require.Equal(t, []string{module.CodeTagSignerNotAllowed}, codes(res), "a is allowed by one rule only")
	assert.Equal(t, "prod", res.Violations[0].Rule)
	assert.Equal(t, []string{"all-releases", "prod"}, res.Signatures[0].Rules)

	res = push(t, p, "refs/tags/v1-prod", "tag", append(payload, sshSign(t, b, payload, "git", "sha512", "")...))
	assert.Equal(t, module.Allow, res.Decision)

	payload = tagPayload("v1", "commit")
	res = push(t, p, "refs/tags/v1", "tag", append(payload, sshSign(t, a, payload, "git", "sha512", "")...))
	assert.Equal(t, module.Allow, res.Decision)
}

func TestEvaluateInvalid(t *testing.T) {
	in := &module.Input{Updates: []module.RefUpdate{
		{Old: strings.Repeat("0", 40), New: strings.Repeat("a", 40), Ref: "refs/tags/v1"},
		{Old: strings.Repeat("a", 40), New: strings.Repeat("b", 40), Ref: "refs/heads/main"},
		{Old: strings.Repeat("a", 40), New: strings.Repeat("0", 40), Ref: "refs/tags/v0"},
	}}
	res, err := New().EvaluateInvalid(context.Background(), in, errors.New("line 2: bad"))
	require.NoError(t, err)
	assert.Equal(t, module.Deny, res.Decision)
	require.Len(t, res.Violations, 1)
	assert.Equal(t, module.CodeInvalidPolicy, res.Violations[0].Code)
	assert.Equal(t, "refs/tags/v1", res.Violations[0].Ref)
	assert.Equal(t, "deny", res.Events[0].Decision)

	in.Updates = in.Updates[1:]
	res, err = New().EvaluateInvalid(context.Background(), in, errors.New("line 2: bad"))
	require.NoError(t, err)
	assert.Equal(t, module.Allow, res.Decision, "branches and tag deletions pass")
	assert.Len(t, res.Events, 1, "the invalid policy is still audited")
}

// TestVerify_RealTools checks objects made once by git tag -s with gpg and
// ssh-keygen and a SHA-1 signature made by gpg. The private keys existed only
// while the objects were made; the repository holds the public keys.
func TestVerify_RealTools(t *testing.T) {
	read := func(name string) string {
		data, err := os.ReadFile(filepath.Join("testdata", "tools", name))
		require.NoError(t, err)
		return string(data)
	}
	rules := `  - name: all
    tags: 'v.*'
    signers: [gpg, ed25519, rsa]
`
	p := mustParse(t, policyYAML(rules, map[string]string{"gpg": read("openpgp.pub"), "ed25519": strings.TrimSpace(read("ed25519.pub")),
		"rsa": strings.TrimSpace(read("rsa.pub"))}))
	later := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	for name, signer := range map[string]string{"v1.0.0-gpg": "gpg", "v1.0.0-ssh-ed25519": "ed25519", "v1.0.0-ssh-rsa": "rsa"} {
		t.Run(name, func(t *testing.T) {
			res := pushAt(t, p, "refs/tags/"+name, []byte(read(name+".tag")), later)
			assert.Equal(t, module.Allow, res.Decision, res.Violations)
			require.Len(t, res.Signatures, 1)
			assert.Equal(t, signer, res.Signatures[0].Signer)
		})
	}
	t.Run("sha1", func(t *testing.T) {
		res := pushAt(t, p, "refs/tags/v1.0.0-sha1", []byte(read("v1.0.0-sha1.tag")), later)
		require.Equal(t, []string{module.CodeTagSignatureInvalid}, codes(res))
		assert.Contains(t, res.Violations[0].Message, "SHA-1")
	})
	t.Run("renamed", func(t *testing.T) {
		data := strings.Replace(read("v1.0.0-ssh-rsa.tag"), "tag v1.0.0-ssh-rsa", "tag v1.0.0-ssh-rsb", 1)
		res := pushAt(t, p, "refs/tags/v1.0.0-ssh-rsb", []byte(data), later)
		assert.Equal(t, []string{module.CodeTagSignatureInvalid}, codes(res))
	})
}

// TestVerify_SSH_SecurityKeys checks sk keys on objects from testdata/sk: a
// FIDO authenticator was emulated by an ordinary key (OpenSSH PROTOCOL.u2f)
// outside the repository and every object was checked by x/crypto/ssh and
// ssh-keygen -Y verify; the private keys were never written to disk.
func TestVerify_SSH_SecurityKeys(t *testing.T) {
	for _, kind := range []string{"sk-ed25519", "sk-ecdsa"} {
		t.Run(kind, func(t *testing.T) {
			rules := "  - name: all\n    tags: 'v.*'\n    signers: [lead]\n"
			p := mustParse(t, policyYAML(rules, map[string]string{"lead": strings.TrimSpace(readTestdata(t, "sk", kind+".pub"))}))
			name := "v1.0.0-" + kind
			res := push(t, p, "refs/tags/"+name, "tag", []byte(readTestdata(t, "sk", name+".tag")))
			assert.Equal(t, module.Allow, res.Decision, res.Violations)
			require.Len(t, res.Signatures, 1)

			// Without the user presence flag the signature is rejected.
			res = push(t, p, "refs/tags/"+name+"-no-touch", "tag", []byte(readTestdata(t, "sk", name+"-no-touch.tag")))
			require.Equal(t, []string{module.CodeTagSignatureInvalid}, codes(res))
			assert.Contains(t, res.Violations[0].Message, "user presence")
		})
	}
}

func TestEvaluate_OverlapWithoutCommonSigner(t *testing.T) {
	a, b := newSSH(t, "ed25519"), newSSH(t, "ed25519")
	rules := `  - name: one
    tags: 'v.*'
    signers: [a]
  - name: two
    tags: 'v1.*'
    signers: [b]
`
	p := mustParse(t, policyYAML(rules, map[string]string{"a": a.line, "b": b.line}))
	payload := tagPayload("v1", "commit")
	res := push(t, p, "refs/tags/v1", "tag", append(payload, sshSign(t, a, payload, "git", "sha512", "")...))
	require.Equal(t, []string{module.CodeTagSignerNotAllowed}, codes(res))
	assert.Contains(t, res.Violations[0].Message, "allowed:   b")

	res = push(t, p, "refs/tags/v1", "tag", tagPayload("v1", "commit"))
	require.Equal(t, []string{module.CodeTagNotSigned}, codes(res))
	assert.Equal(t, "one, two", res.Violations[0].Rule, "an object problem names every matching rule")
}

func TestModule(t *testing.T) {
	m := New()
	assert.Equal(t, "tagpolicy", m.Name())
	assert.Equal(t, "TAGPOLICY", m.File())
	assert.Equal(t, module.FailClosed, m.OnInvalid())
	p, err := m.Parse(policyYAML(twoRules, map[string]string{"release-lead": newSSH(t, "ed25519").line, "dev-lead": newSSH(t, "ed25519").line}))
	require.NoError(t, err)
	assert.False(t, m.Needs().Identity, "TAGPOLICY does not use the API")
	assert.Empty(t, p.Validate(context.Background(), nil))
	_, err = m.Parse([]byte("version: 1\n"))
	require.Error(t, err)
}
