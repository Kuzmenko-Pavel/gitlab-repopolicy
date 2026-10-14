package tag

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"golang.org/x/crypto/ssh"
)

// keyFormat is the signature format a key verifies.
type keyFormat int

const (
	formatOpenPGP keyFormat = iota + 1
	formatSSH
)

// key is one entry of keys: exactly one public key (D30).
type key struct {
	label  string
	format keyFormat
	// text is the entry without surrounding space, to tell a changed entry.
	text string
	// fingerprint is the hex fingerprint of the OpenPGP primary key or SHA256:… of the SSH key.
	fingerprint string
	entity      *openpgp.Entity
	ssh         ssh.PublicKey
}

const (
	pgpPublicBegin = "-----BEGIN PGP PUBLIC KEY BLOCK-----"
	pgpPublicEnd   = "-----END PGP PUBLIC KEY BLOCK-----"
)

// sshKeyTypes are the SSH key types that x/crypto/ssh verifies (architecture §11.3).
var sshKeyTypes = map[string]bool{
	ssh.KeyAlgoED25519:    true,
	ssh.KeyAlgoECDSA256:   true,
	ssh.KeyAlgoECDSA384:   true,
	ssh.KeyAlgoECDSA521:   true,
	ssh.KeyAlgoRSA:        true,
	ssh.KeyAlgoSKED25519:  true,
	ssh.KeyAlgoSKECDSA256: true,
}

var errPrivateKey = errors.New("this is a private key: TAGPOLICY holds only public keys; " +
	"remove it and treat the private key as exposed")

// parseKey detects the format by content: an armored OpenPGP public key block
// or an OpenSSH public key on one line.
func parseKey(label, raw string) (*key, error) {
	text := strings.TrimSpace(raw)
	if strings.Contains(text, "PRIVATE KEY") {
		return nil, errPrivateKey
	}
	k := &key{label: label, text: text}
	var err error
	if strings.HasPrefix(text, "-----BEGIN PGP") {
		err = k.parseOpenPGP()
	} else {
		err = k.parseSSH()
	}
	if err != nil {
		return nil, err
	}
	return k, nil
}

func (k *key) parseOpenPGP() error {
	if !strings.HasPrefix(k.text, pgpPublicBegin) {
		return errors.New("an OpenPGP key must be an armored public key block (gpg --armor --export)")
	}
	if strings.Count(k.text, pgpPublicBegin) != 1 || !strings.HasSuffix(k.text, pgpPublicEnd) {
		return errors.New("exactly one armored public key block is allowed per entry")
	}
	ring, err := openpgp.ReadArmoredKeyRing(strings.NewReader(k.text))
	if err != nil {
		return fmt.Errorf("not a valid OpenPGP public key: %w", err)
	}
	if len(ring) != 1 {
		return fmt.Errorf("the block holds %d keys, exactly one is allowed per entry", len(ring))
	}
	e := ring[0]
	if e.PrivateKey != nil {
		return errPrivateKey
	}
	for _, sub := range e.Subkeys {
		if sub.PrivateKey != nil {
			return errPrivateKey
		}
	}
	k.format, k.entity = formatOpenPGP, e
	k.fingerprint = fmt.Sprintf("%X", e.PrimaryKey.Fingerprint)
	if revoked(e) {
		return fmt.Errorf("the key %s is revoked", k.fingerprint)
	}
	if !canSign(e) {
		return fmt.Errorf("the key %s cannot sign: neither the primary key nor a subkey has the signing flag", k.fingerprint)
	}
	return nil
}

// revoked reports a revocation of the primary key at any time.
func revoked(e *openpgp.Entity) bool {
	return len(e.Revocations) > 0
}

// canSign reports whether the primary key or a subkey that is not revoked may
// sign, regardless of expiry: expiry is checked against time separately.
func canSign(e *openpgp.Entity) bool {
	self, _ := e.PrimarySelfSignature()
	if self != nil && self.FlagsValid && self.FlagSign && e.PrimaryKey.PubKeyAlgo.CanSign() {
		return true
	}
	for _, sub := range e.Subkeys {
		if sub.Sig != nil && sub.Sig.FlagsValid && sub.Sig.FlagSign && sub.PublicKey.PubKeyAlgo.CanSign() && len(sub.Revocations) == 0 {
			return true
		}
	}
	return false
}

func (k *key) parseSSH() error {
	if strings.ContainsAny(k.text, "\r\n") {
		return errors.New("not an OpenPGP public key block or an OpenSSH public key on one line")
	}
	pub, _, options, rest, err := ssh.ParseAuthorizedKey([]byte(k.text))
	if err != nil {
		return errors.New("not an OpenPGP public key block or an OpenSSH public key on one line")
	}
	if len(options) > 0 || len(bytes.TrimSpace(rest)) > 0 {
		return errors.New("an SSH key is written as \"<type> <base64> [comment]\" without options")
	}
	if _, ok := pub.(*ssh.Certificate); ok {
		return errors.New("SSH certificates are not supported, use the public key")
	}
	if !sshKeyTypes[pub.Type()] {
		return fmt.Errorf("SSH key type %s is not supported", pub.Type())
	}
	k.format, k.ssh = formatSSH, pub
	k.fingerprint = ssh.FingerprintSHA256(pub)
	return nil
}

// expired reports an OpenPGP key without a signing key valid at now; SSH keys
// have no expiry and stay valid while they are in the file (D30).
func (k *key) expired(now time.Time) bool {
	if k.format != formatOpenPGP {
		return false
	}
	_, ok := k.entity.SigningKey(now)
	return !ok
}

// ring is the OpenPGP keys of the policy.
func (p *policy) ring() openpgp.EntityList {
	var ring openpgp.EntityList
	for _, k := range p.keys {
		if k.format == formatOpenPGP {
			ring = append(ring, k.entity)
		}
	}
	return ring
}

// byOpenPGP finds the entry of a verified OpenPGP entity.
func (p *policy) byOpenPGP(e *openpgp.Entity) *key {
	for _, k := range p.keys {
		if k.format == formatOpenPGP && bytes.Equal(k.entity.PrimaryKey.Fingerprint, e.PrimaryKey.Fingerprint) {
			return k
		}
	}
	return nil
}

// bySSH finds the entry of an SSH public key.
func (p *policy) bySSH(pub ssh.PublicKey) *key {
	blob := pub.Marshal()
	for _, k := range p.keys {
		if k.format == formatSSH && bytes.Equal(k.ssh.Marshal(), blob) {
			return k
		}
	}
	return nil
}
