package tag

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
)

// now is the moment of the push in every test.
var now = time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

const commitOID = "1111111111111111111111111111111111111111"

func at(t time.Time) func() time.Time { return func() time.Time { return t } }

// pgpOptions shape a generated OpenPGP key.
type pgpOptions struct {
	created  time.Time
	lifetime time.Duration
	// subkey adds a signing subkey created at created with subLifetime.
	subkey      bool
	subLifetime time.Duration
	revoked     bool
}

func pgpConfig(created time.Time, lifetime time.Duration) *packet.Config {
	return &packet.Config{Algorithm: packet.PubKeyAlgoEdDSA, Time: at(created), KeyLifetimeSecs: uint32(lifetime / time.Second)}
}

func newPGP(t *testing.T, o pgpOptions) *openpgp.Entity {
	t.Helper()
	if o.created.IsZero() {
		o.created = now.Add(-24 * time.Hour)
	}
	e, err := openpgp.NewEntity("Release Lead", "", "lead@example.com", pgpConfig(o.created, o.lifetime))
	require.NoError(t, err)
	if o.subkey {
		require.NoError(t, e.AddSigningSubkey(pgpConfig(o.created, o.subLifetime)))
	}
	if o.revoked {
		require.NoError(t, e.RevokeKey(packet.KeyRetired, "retired", pgpConfig(o.created.Add(time.Hour), 0)))
	}
	return e
}

func armorPublic(t *testing.T, e *openpgp.Entity) string {
	t.Helper()
	var b bytes.Buffer
	w, err := armor.Encode(&b, openpgp.PublicKeyType, nil)
	require.NoError(t, err)
	require.NoError(t, e.Serialize(w))
	require.NoError(t, w.Close())
	return b.String()
}

func armorPrivate(t *testing.T, e *openpgp.Entity) string {
	t.Helper()
	var b bytes.Buffer
	w, err := armor.Encode(&b, openpgp.PrivateKeyType, nil)
	require.NoError(t, err)
	require.NoError(t, e.SerializePrivate(w, nil))
	require.NoError(t, w.Close())
	return b.String()
}

// tagPayload is the unsigned part of a tag object as git writes it.
func tagPayload(name, typ string) []byte {
	return fmt.Appendf(nil, "object %s\ntype %s\ntag %s\ntagger Release Lead <lead@example.com> %d +0000\n\nRelease %s\n",
		commitOID, typ, name, now.Add(-time.Hour).Unix(), name)
}

func signPGP(t *testing.T, e *openpgp.Entity, payload []byte, signed time.Time, hash crypto.Hash) []byte {
	t.Helper()
	var b bytes.Buffer
	cfg := &packet.Config{Time: at(signed), DefaultHash: hash}
	require.NoError(t, openpgp.ArmoredDetachSign(&b, e, bytes.NewReader(payload), cfg))
	return append(append([]byte{}, payload...), append(b.Bytes(), '\n')...)
}

// sshKey is a generated SSH key with its signer.
type sshKey struct {
	signer ssh.Signer
	line   string
}

func newSSH(t *testing.T, kind string) sshKey {
	t.Helper()
	var priv crypto.Signer
	var err error
	switch kind {
	case "ed25519":
		_, priv, err = ed25519.GenerateKey(rand.Reader)
	case "p256":
		priv, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	case "p384":
		priv, err = ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	case "p521":
		priv, err = ecdsa.GenerateKey(elliptic.P521(), rand.Reader)
	case "rsa":
		priv, err = rsa.GenerateKey(rand.Reader, 2048)
	default:
		t.Fatalf("unknown key kind %s", kind)
	}
	require.NoError(t, err)
	s, err := ssh.NewSignerFromSigner(priv)
	require.NoError(t, err)
	return sshKey{signer: s, line: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(s.PublicKey()))) + " " + kind}
}

// sshSign writes an SSHSIG block for payload, independently of the product code.
func sshSign(t *testing.T, k sshKey, payload []byte, namespace, hash, algo string) []byte {
	t.Helper()
	var sum []byte
	switch hash {
	case "sha512":
		h := sha512.Sum512(payload)
		sum = h[:]
	default:
		h := sha256.Sum256(payload)
		sum = h[:]
	}
	signed := append([]byte("SSHSIG"), ssh.Marshal(struct{ N, R, H, S string }{namespace, "", hash, string(sum)})...)
	var sig *ssh.Signature
	var err error
	if algo != "" {
		sig, err = k.signer.(ssh.AlgorithmSigner).SignWithAlgorithm(rand.Reader, signed, algo)
	} else {
		sig, err = k.signer.Sign(rand.Reader, signed)
	}
	require.NoError(t, err)
	blob := ssh.Marshal(struct {
		Magic   [6]byte
		Version uint32
		Pub     string
		N, R, H string
		Sig     string
	}{[6]byte{'S', 'S', 'H', 'S', 'I', 'G'}, 1, string(k.signer.PublicKey().Marshal()), namespace, "", hash, string(ssh.Marshal(sig))})
	return armorSSH(blob)
}

func armorSSH(blob []byte) []byte {
	enc := base64.StdEncoding.EncodeToString(blob)
	var b strings.Builder
	b.WriteString("-----BEGIN SSH SIGNATURE-----\n")
	for len(enc) > 70 {
		b.WriteString(enc[:70] + "\n")
		enc = enc[70:]
	}
	b.WriteString(enc + "\n-----END SSH SIGNATURE-----\n")
	return []byte(b.String())
}

// fakeObjects serves tag objects by oid.
type fakeObjects map[string]module.Object

func (f fakeObjects) Read(_ context.Context, oid string, max int) (module.Object, error) {
	o, ok := f[oid]
	if !ok {
		return module.Object{}, module.ErrMissing
	}
	if len(o.Data) > max {
		return module.Object{}, module.ErrTooLarge
	}
	return o, nil
}

// indent turns a key into a YAML block scalar.
func indent(s string) string {
	return "|\n    " + strings.ReplaceAll(strings.TrimSpace(s), "\n", "\n    ")
}

// policyYAML builds a TAGPOLICY from rules in YAML and keys by label.
func policyYAML(rules string, keys map[string]string) []byte {
	var b strings.Builder
	b.WriteString("version: 1\n")
	if rules != "" {
		b.WriteString("rules:\n" + rules)
	}
	b.WriteString("keys:\n")
	for label, k := range keys {
		if strings.Contains(k, "\n") {
			fmt.Fprintf(&b, "  %s: %s\n", label, indent(k))
		} else {
			fmt.Fprintf(&b, "  %s: %s\n", label, k)
		}
	}
	return []byte(b.String())
}

func mustParse(t *testing.T, data []byte) *policy {
	t.Helper()
	p, err := parsePolicy(data, nil)
	require.NoError(t, err)
	return p
}

// push evaluates one tag update whose new object is data.
func push(t *testing.T, p *policy, ref, typ string, data []byte) *module.Result {
	t.Helper()
	return pushObject(t, p, ref, typ, data, now)
}

func pushAt(t *testing.T, p *policy, ref string, data []byte, when time.Time) *module.Result {
	t.Helper()
	return pushObject(t, p, ref, "tag", data, when)
}

func pushObject(t *testing.T, p *policy, ref, typ string, data []byte, when time.Time) *module.Result {
	t.Helper()
	oid := fmt.Sprintf("%040x", sha256.Sum256(data))[:40]
	in := &module.Input{
		Updates: []module.RefUpdate{{Old: strings.Repeat("0", 40), New: oid, Ref: ref}},
		Objects: fakeObjects{oid: {OID: oid, Type: typ, Data: data}},
		Clock:   at(when),
	}
	res, err := p.Evaluate(context.Background(), in)
	require.NoError(t, err)
	return res
}

func codes(res *module.Result) []string {
	out := make([]string, 0, len(res.Violations))
	for _, v := range res.Violations {
		out = append(out, v.Code)
	}
	return out
}

// signPGPPayload signs an arbitrary payload at the push time with SHA-256.
func signPGPPayload(t *testing.T, e *openpgp.Entity, payload []byte) []byte {
	t.Helper()
	return signPGP(t, e, payload, now, crypto.SHA256)
}
