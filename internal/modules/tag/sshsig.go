package tag

import (
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/ssh"
)

// SSHSIG of OpenSSH PROTOCOL.sshsig, as git writes it with gpg.format=ssh.
const (
	sshsigBegin     = "-----BEGIN SSH SIGNATURE-----"
	sshsigEnd       = "-----END SSH SIGNATURE-----"
	sshsigMagic     = "SSHSIG"
	sshsigVersion   = 1
	sshsigNamespace = "git"
)

// sshsigBlob is the wire format of an SSH signature.
type sshsigBlob struct {
	Magic         [6]byte
	Version       uint32
	PublicKey     string
	Namespace     string
	Reserved      string
	HashAlgorithm string
	Signature     string
}

// sshsigSigned is the data the key signs, after the magic preamble.
type sshsigSigned struct {
	Namespace     string
	Reserved      string
	HashAlgorithm string
	Hash          string
}

type sshSignature struct {
	pub ssh.PublicKey
	sig *ssh.Signature
	// hash is sha256 or sha512.
	hash string
}

// errBadSignature wraps every reason why a signature is not acceptable.
var errBadSignature = errors.New("invalid signature")

func badSignature(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errBadSignature, fmt.Sprintf(format, args...))
}

// unarmorSSH decodes the armored block: the lines between the markers are
// base64, nothing but space may follow the end marker.
func unarmorSSH(block []byte) ([]byte, error) {
	lines := strings.Split(strings.TrimRight(string(block), " \t\r\n"), "\n")
	if len(lines) < 3 || strings.TrimRight(lines[0], "\r") != sshsigBegin || strings.TrimRight(lines[len(lines)-1], "\r") != sshsigEnd {
		return nil, badSignature("the SSH signature block is malformed")
	}
	var b strings.Builder
	for _, l := range lines[1 : len(lines)-1] {
		b.WriteString(strings.TrimSpace(l))
	}
	raw, err := base64.StdEncoding.DecodeString(b.String())
	if err != nil {
		return nil, badSignature("the SSH signature block is not base64")
	}
	return raw, nil
}

// parseSSHSig checks everything that does not need the key: format, version,
// namespace git, hash and, for RSA, a SHA-2 signature algorithm.
func parseSSHSig(block []byte) (*sshSignature, error) {
	raw, err := unarmorSSH(block)
	if err != nil {
		return nil, err
	}
	var b sshsigBlob
	if err := ssh.Unmarshal(raw, &b); err != nil {
		return nil, badSignature("the SSH signature cannot be decoded")
	}
	if string(b.Magic[:]) != sshsigMagic {
		return nil, badSignature("not an SSHSIG signature")
	}
	if b.Version != sshsigVersion {
		return nil, badSignature("SSHSIG version %d is not supported", b.Version)
	}
	if b.Namespace != sshsigNamespace {
		return nil, badSignature("the SSH signature namespace is %q, git signs with %q", b.Namespace, sshsigNamespace)
	}
	if b.HashAlgorithm != "sha256" && b.HashAlgorithm != "sha512" {
		return nil, badSignature("the SSH signature hash %q is not sha256 or sha512", b.HashAlgorithm)
	}
	pub, err := ssh.ParsePublicKey([]byte(b.PublicKey))
	if err != nil {
		return nil, badSignature("the public key of the SSH signature cannot be decoded")
	}
	sig := &ssh.Signature{}
	if err := ssh.Unmarshal([]byte(b.Signature), sig); err != nil {
		return nil, badSignature("the SSH signature cannot be decoded")
	}
	if pub.Type() == ssh.KeyAlgoRSA && sig.Format != ssh.KeyAlgoRSASHA256 && sig.Format != ssh.KeyAlgoRSASHA512 {
		return nil, badSignature("the RSA signature uses %s; only rsa-sha2-256 and rsa-sha2-512 are accepted", sig.Format)
	}
	return &sshSignature{pub: pub, sig: sig, hash: b.HashAlgorithm}, nil
}

// verify checks the signature of payload with the key from the file.
func (s *sshSignature) verify(pub ssh.PublicKey, payload []byte) error {
	var sum []byte
	if s.hash == "sha512" {
		h := sha512.Sum512(payload)
		sum = h[:]
	} else {
		h := sha256.Sum256(payload)
		sum = h[:]
	}
	signed := append([]byte(sshsigMagic), ssh.Marshal(sshsigSigned{Namespace: sshsigNamespace, HashAlgorithm: s.hash, Hash: string(sum)})...)
	if err := pub.Verify(signed, s.sig); err != nil {
		return badSignature("the SSH signature does not match the tag: %v", err)
	}
	return nil
}

// ssh256 is the fingerprint of the key that made the signature.
func ssh256(s *sshSignature) string { return ssh.FingerprintSHA256(s.pub) }
