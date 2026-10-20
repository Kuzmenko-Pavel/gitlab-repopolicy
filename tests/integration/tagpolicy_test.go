//go:build integration

package integration

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

// tagSigner signs the payload of a tag object and returns the signature block.
type tagSigner struct {
	key  string
	fpr  string
	sign func(t *testing.T, payload []byte) []byte
}

func pgpSigner(t *testing.T) tagSigner {
	t.Helper()
	created := time.Now().Add(-time.Hour)
	cfg := &packet.Config{Algorithm: packet.PubKeyAlgoEdDSA, Time: func() time.Time { return created }}
	e, err := openpgp.NewEntity("Release Lead", "", "lead@example.com", cfg)
	require.NoError(t, err)
	var pub bytes.Buffer
	w, err := armor.Encode(&pub, openpgp.PublicKeyType, nil)
	require.NoError(t, err)
	require.NoError(t, e.Serialize(w))
	require.NoError(t, w.Close())
	return tagSigner{key: pub.String(), fpr: fmt.Sprintf("%X", e.PrimaryKey.Fingerprint), sign: func(t *testing.T, payload []byte) []byte {
		var sig bytes.Buffer
		require.NoError(t, openpgp.ArmoredDetachSign(&sig, e, bytes.NewReader(payload), nil))
		return append(sig.Bytes(), '\n')
	}}
}

func sshSigner(t *testing.T) tagSigner {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	signer, err := ssh.NewSignerFromSigner(priv)
	require.NoError(t, err)
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
	return tagSigner{key: line, fpr: ssh.FingerprintSHA256(signer.PublicKey()), sign: func(t *testing.T, payload []byte) []byte {
		h := sha512.Sum512(payload)
		data := append([]byte("SSHSIG"), ssh.Marshal(struct{ N, R, H, S string }{"git", "", "sha512", string(h[:])})...)
		sig, err := signer.Sign(rand.Reader, data)
		require.NoError(t, err)
		blob := ssh.Marshal(struct {
			Magic   [6]byte
			Version uint32
			Pub     string
			N, R, H string
			Sig     string
		}{[6]byte{'S', 'S', 'H', 'S', 'I', 'G'}, 1, string(signer.PublicKey().Marshal()), "git", "", "sha512", string(ssh.Marshal(sig))})
		return []byte("-----BEGIN SSH SIGNATURE-----\n" + base64.StdEncoding.EncodeToString(blob) + "\n-----END SSH SIGNATURE-----\n")
	}}
}

// tagPolicyFor is a TAGPOLICY with a prod rule for lead and a staging rule for lead and dev.
func tagPolicyFor(lead, dev tagSigner) string {
	key := func(k string) string {
		if strings.Contains(k, "\n") {
			return "|\n    " + strings.ReplaceAll(strings.TrimSpace(k), "\n", "\n    ")
		}
		return k
	}
	return fmt.Sprintf(`version: 1
rules:
  staging:
    match: ['/^v\d+\.\d+\.\d+-staging$/']
    signers: [lead, dev]
    message: Staging releases are signed by lead or dev.
  prod:
    match: ['/^v\d+\.\d+\.\d+-prod$/']
    signers: [lead]
    message: Prod releases are signed by lead.
keys:
  lead: %s
  dev: %s
`, key(lead.key), key(dev.key))
}

// makeTag writes a tag object with git mktag and points refs/tags/<ref> at it
// in the work clone. The object is named name and points to target of type typ;
// signer nil leaves it unsigned.
func (s *Stand) makeTag(ref, name, target, typ string, signer *tagSigner) string {
	s.t.Helper()
	payload := fmt.Appendf(nil, "object %s\ntype %s\ntag %s\ntagger dev <dev@example.com> %d +0000\n\nRelease %s\n",
		target, typ, name, time.Now().Add(-time.Minute).Unix(), name)
	if signer != nil {
		payload = append(payload, signer.sign(s.t, payload)...)
	}
	oid := s.gitStdin(string(payload), s.Work, "mktag")
	s.git(s.Work, "update-ref", "refs/tags/"+ref, oid)
	return oid
}

func tagStand(t *testing.T, opts ...Option) (*Stand, string, tagSigner, tagSigner) {
	t.Helper()
	lead, dev := pgpSigner(t), sshSigner(t)
	s, base := mainWith(t, map[string]string{"TAGPOLICY": tagPolicyFor(lead, dev)}, opts...)
	return s, base, lead, dev
}

func TestTagPolicy_SignedAllowed(t *testing.T) {
	s, base, lead, dev := tagStand(t)
	s.makeTag("v1.0.0-prod", "v1.0.0-prod", base, "commit", &lead)
	assertAllowed(t, s.Push(alice, "refs/tags/v1.0.0-prod"))
	ev := Events(s.Audit(), "decision")
	require.Len(t, ev, 1)
	assert.Equal(t, "tagpolicy", ev[0]["module"])
	assert.Equal(t, "allow", ev[0]["decision"])
	assert.Equal(t, "refs/tags/v1.0.0-prod", ev[0]["ref"])
	assert.Equal(t, []any{"prod"}, ev[0]["rules"])
	assert.Equal(t, "lead", ev[0]["signer"])
	assert.Equal(t, lead.fpr, ev[0]["fingerprint"])

	s.makeTag("v1.0.0-staging", "v1.0.0-staging", base, "commit", &dev)
	assertAllowed(t, s.Push(alice, "refs/tags/v1.0.0-staging"))
	ev = Events(s.Audit(), "decision")
	require.Len(t, ev, 1)
	assert.Equal(t, "dev", ev[0]["signer"])
	assert.Equal(t, dev.fpr, ev[0]["fingerprint"])
}

func TestTagPolicy_SignerNotAllowed(t *testing.T) {
	s, base, _, dev := tagStand(t)
	s.makeTag("v1.0.0-prod", "v1.0.0-prod", base, "commit", &dev)
	r := s.Push(alice, "refs/tags/v1.0.0-prod")
	assertDenied(t, r, "TAGPOLICY rule \"prod\", tag v1.0.0-prod: this key may not sign these tags", "Prod releases are signed by lead.",
		`signed with: key "dev" from TAGPOLICY (`, dev.fpr, "allowed keys: lead")
	assert.Empty(t, s.RemoteRev("refs/tags/v1.0.0-prod"))
	ev := Events(s.Audit(), "decision")
	require.Len(t, ev, 1)
	assert.Equal(t, "tag_signer_not_allowed", ev[0]["code"])
	assert.Equal(t, "dev", ev[0]["signer"])

	stranger := sshSigner(t)
	s.makeTag("v1.0.1-prod", "v1.0.1-prod", base, "commit", &stranger)
	assertDenied(t, s.Push(alice, "refs/tags/v1.0.1-prod"), "this key may not sign these tags", "a key that is not in TAGPOLICY", stranger.fpr)
}

func TestTagPolicy_Lightweight(t *testing.T) {
	s, base, _, _ := tagStand(t)
	s.git(s.Work, "update-ref", "refs/tags/v1.0.0-prod", base)
	assertDenied(t, s.Push(alice, "refs/tags/v1.0.0-prod"), "TAGPOLICY rule \"prod\", tag v1.0.0-prod: the tag must be signed")
}

func TestTagPolicy_Unsigned(t *testing.T) {
	s, base, _, _ := tagStand(t)
	s.git(s.Work, "tag", "-a", "-m", "release", "v1.0.0-prod", base)
	assertDenied(t, s.Push(alice, "refs/tags/v1.0.0-prod"), "the tag must be signed", "git tag -s v1.0.0-prod")
}

func TestTagPolicy_TagNameMismatch(t *testing.T) {
	s, base, _, dev := tagStand(t)
	// A staging tag object, validly signed by dev, pushed under a prod name.
	s.makeTag("v1.0.0-prod", "v1.0.0-staging", base, "commit", &dev)
	assertDenied(t, s.Push(alice, "refs/tags/v1.0.0-prod"), `the tag was signed as "v1.0.0-staging" but is pushed as "v1.0.0-prod"`)
}

func TestTagPolicy_TagOfTag(t *testing.T) {
	s, base, lead, _ := tagStand(t)
	inner := s.makeTag("inner", "inner", base, "commit", nil)
	s.makeTag("v1.0.0-prod", "v1.0.0-prod", inner, "tag", &lead)
	assertDenied(t, s.Push(alice, "refs/tags/v1.0.0-prod"), "the tag points to a tag, not a commit")
}

func TestTagPolicy_OutsideRules(t *testing.T) {
	s, base, _, _ := tagStand(t)
	s.git(s.Work, "update-ref", "refs/tags/v1.0.0", base)
	s.git(s.Work, "tag", "-a", "-m", "x", "v1.0.0-prod-hotfix", base)
	assertAllowed(t, s.Push(alice, "refs/tags/v1.0.0", "refs/tags/v1.0.0-prod-hotfix"))
	assert.Empty(t, Events(s.Audit(), "decision"), "a push that touches no rule is not audited")
}

func TestTagPolicy_MoveAndDelete(t *testing.T) {
	s, base, lead, _ := tagStand(t)
	s.makeTag("v1.0.0-prod", "v1.0.0-prod", base, "commit", &lead)
	assertAllowed(t, s.Push(alice, "refs/tags/v1.0.0-prod"))

	next := s.Commit("next", map[string]string{"a": "1"})
	s.Seed("HEAD:refs/heads/main")
	s.git(s.Work, "tag", "-f", "-a", "-m", "moved", "v1.0.0-prod", next)
	assertDenied(t, s.Push(alice, "+refs/tags/v1.0.0-prod"), "the tag must be signed")

	s.makeTag("v1.0.0-prod", "v1.0.0-prod", next, "commit", &lead)
	assertAllowed(t, s.Push(alice, "+refs/tags/v1.0.0-prod"))

	assertAllowed(t, s.Push(alice, ":refs/tags/v1.0.0-prod"))
	assert.Empty(t, s.RemoteRev("refs/tags/v1.0.0-prod"), "deleting a tag is not regulated")
}

func TestTagPolicy_SHA256(t *testing.T) {
	s, base, lead, dev := tagStand(t, WithObjectFormat("sha256"))
	require.Len(t, base, 64)
	s.makeTag("v1.0.0-prod", "v1.0.0-prod", base, "commit", &lead)
	assertAllowed(t, s.Push(alice, "refs/tags/v1.0.0-prod"))
	s.makeTag("v1.0.1-prod", "v1.0.1-prod", base, "commit", &dev)
	assertDenied(t, s.Push(alice, "refs/tags/v1.0.1-prod"), "this key may not sign these tags")
}

func TestTagPolicy_InvalidAnchor(t *testing.T) {
	lead := sshSigner(t)
	s, base := mainWith(t, map[string]string{"TAGPOLICY": "version: 1\nkeys:\n  lead: " + lead.key + "\nrules:\n  prod:\n    match: all\n    signers: [nobody]\n    message: m\n"})
	s.makeTag("v1.0.0", "v1.0.0", base, "commit", &lead)
	assertDenied(t, s.Push(alice, "refs/tags/v1.0.0"), "TAGPOLICY, tag v1.0.0: TAGPOLICY in main is broken", `"nobody" is not a label in keys of the file`)
	require.NotEmpty(t, Events(s.Audit(), "invalid_policy"))

	// Branches and tag deletions pass.
	s.Commit("feature", map[string]string{"a": "1"})
	assertAllowed(t, s.Push(alice, "HEAD:refs/heads/main"))
	s.Seed("refs/tags/v1.0.0")
	assertAllowed(t, s.Push(alice, ":refs/tags/v1.0.0"))
}

func TestTagPolicy_InvalidNewVersionOnFeatureBranch(t *testing.T) {
	s, _, _, _ := tagStand(t)
	s.Commit("break the policy", map[string]string{"TAGPOLICY": "version: 1\nkeys:\n  lead: " + strings.TrimSpace(sshSigner(t).key) + "\nrules:\n  prod:\n    match: ['/v(/']\n    signers: [lead]\n    message: m\n"})
	assertDenied(t, s.Push(alice, "HEAD:refs/heads/feature"), "TAGPOLICY, branch feature: the changed TAGPOLICY is invalid", "not a valid RE2 regex")

	private := "version: 1\nkeys:\n  lead: |\n    -----BEGIN OPENSSH PRIVATE KEY-----\n    b3BlbnNzaC1rZXktdjEAAAAA\n    -----END OPENSSH PRIVATE KEY-----\n"
	s.Commit("leak a key", map[string]string{"TAGPOLICY": private})
	assertDenied(t, s.Push(alice, "HEAD:refs/heads/feature"), "this is a private key", "treat the private key as exposed")
}

func TestTagPolicy_ExpiredKeyInNewVersion(t *testing.T) {
	s, _, _, dev := tagStand(t)
	created := time.Now().Add(-48 * time.Hour)
	cfg := &packet.Config{Algorithm: packet.PubKeyAlgoEdDSA, Time: func() time.Time { return created }, KeyLifetimeSecs: 3600}
	e, err := openpgp.NewEntity("Old", "", "old@example.com", cfg)
	require.NoError(t, err)
	var pub bytes.Buffer
	w, err := armor.Encode(&pub, openpgp.PublicKeyType, nil)
	require.NoError(t, err)
	require.NoError(t, e.Serialize(w))
	require.NoError(t, w.Close())
	expired := tagSigner{key: pub.String()}
	s.Commit("add an expired key", map[string]string{"TAGPOLICY": tagPolicyFor(expired, dev)})
	assertDenied(t, s.Push(alice, "HEAD:refs/heads/feature"), "is invalid", "keys.lead: the key", "expired")
}

func TestTagPolicy_DeletionChecksWithOldPolicy(t *testing.T) {
	s, base, _, dev := tagStand(t)
	s.Commit("remove TAGPOLICY", map[string]string{"TAGPOLICY": ""})
	s.makeTag("v1.0.0-prod", "v1.0.0-prod", base, "commit", &dev)
	r := s.Push(alice, "HEAD:refs/heads/main", "refs/tags/v1.0.0-prod")
	assertDenied(t, r, "this key may not sign these tags")
	assert.NotEqual(t, s.Rev("HEAD"), s.RemoteRev("refs/heads/main"), "the push is atomic for the hook")

	assertAllowed(t, s.Push(alice, "HEAD:refs/heads/main"))
	assertAllowed(t, s.Push(alice, "refs/tags/v1.0.0-prod"))
}

func TestTagPolicy_BranchesNotAffected(t *testing.T) {
	s, _, _, _ := tagStand(t)
	s.Commit("feature", map[string]string{"a": "1"})
	assertAllowed(t, s.Push(alice, "HEAD:refs/heads/v1.0.0-prod"))
}

// TestTagPolicy_Protected: a new tag under a rule with protected must be a
// protected tag of the project; a signed tag is denied only for that.
func TestTagPolicy_Protected(t *testing.T) {
	lead, dev := pgpSigner(t), sshSigner(t)
	policy := strings.Replace(tagPolicyFor(lead, dev), "    signers: [lead]\n", "    signers: [lead]\n    protected: true\n", 1)
	s, base := mainWith(t, map[string]string{"TAGPOLICY": policy})
	s.Protected(nil, nil)
	s.makeTag("v1.0.0-prod", "v1.0.0-prod", base, "commit", &lead)
	assertDenied(t, s.Push(alice, "refs/tags/v1.0.0-prod"), "the tag must be protected, but the project does not protect it",
		"GL-HOOK-ERR: gitlab-repopolicy: tag v1.0.0-prod must be protected (TAGPOLICY rule prod)")

	s.Protected(nil, []string{"v*"})
	assertAllowed(t, s.Push(alice, "refs/tags/v1.0.0-prod"))

	s.makeTag("v1.0.0-staging", "v1.0.0-staging", base, "commit", &dev)
	before := s.ProtectedCalls()
	assertAllowed(t, s.Push(alice, "refs/tags/v1.0.0-staging"))
	assert.Equal(t, before, s.ProtectedCalls(), "a rule without protected does not ask GitLab")
}
