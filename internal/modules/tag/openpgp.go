package tag

import (
	"bytes"
	"crypto"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	pgperrors "github.com/ProtonMail/go-crypto/openpgp/errors"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
)

// clockSkew is how far a signature may be ahead of the push.
const clockSkew = 5 * time.Minute

// weakHashes are the hashes of OpenPGP signatures that are not accepted.
var weakHashes = map[crypto.Hash]string{
	crypto.MD5:       "MD5",
	crypto.SHA1:      "SHA-1",
	crypto.RIPEMD160: "RIPEMD-160",
}

// errUnknownSigner is a signature by a key that is not in the file.
type errUnknownSigner struct{ keyID string }

func (e errUnknownSigner) Error() string { return "the key " + e.keyID + " is not in TAGPOLICY" }

// verifyOpenPGP checks a detached OpenPGP signature of payload by the keys of
// the file: the signature is not from more than five minutes after the push,
// the key and the signing subkey are not expired or revoked at the push (or at
// the signature, when it is made after the push on a fast clock) and the hash
// is not weak. It returns the entity of the primary key.
func verifyOpenPGP(ring openpgp.EntityList, payload, block []byte, push time.Time) (*openpgp.Entity, error) {
	if !endsWithBlock(block, "-----END PGP ") {
		return nil, badSignature("the OpenPGP signature block is malformed")
	}
	armored, err := armor.Decode(bytes.NewReader(block))
	if err != nil {
		return nil, badSignature("the OpenPGP signature block cannot be decoded")
	}
	if armored.Type != "PGP SIGNATURE" && armored.Type != "PGP MESSAGE" {
		return nil, badSignature("the OpenPGP block is %q, not a signature", armored.Type)
	}
	body, err := io.ReadAll(armored.Body)
	if err != nil {
		return nil, badSignature("the OpenPGP signature block cannot be decoded")
	}
	// A detached signature is exactly one signature packet; go-crypto skips
	// packets it does not support, so the packet is parsed here first.
	r := bytes.NewReader(body)
	p, err := packet.Read(r)
	if err != nil {
		return nil, badSignature("the OpenPGP signature cannot be decoded: %v", err)
	}
	sig, ok := p.(*packet.Signature)
	if !ok || r.Len() > 0 {
		return nil, badSignature("the OpenPGP block must hold exactly one signature")
	}
	if name, weak := weakHashes[sig.Hash]; weak {
		return nil, badSignature("the signature uses %s", name)
	}
	if len(ring) == 0 {
		return nil, errUnknownSigner{keyID: issuer(sig)}
	}
	if sig.CreationTime.After(push.Add(clockSkew)) {
		return nil, badSignature("the signature is made after the push")
	}
	// go-crypto checks the key and the signature at one moment: the push, or
	// the signature when it is a little later, so that it is not "in the future".
	at := push
	if sig.CreationTime.After(push) {
		at = sig.CreationTime
	}
	config := &packet.Config{Time: func() time.Time { return at }}
	_, signer, err := openpgp.VerifyDetachedSignature(ring, bytes.NewReader(payload), bytes.NewReader(body), config)
	switch {
	case errors.Is(err, pgperrors.ErrUnknownIssuer):
		return nil, errUnknownSigner{keyID: issuer(sig)}
	case errors.Is(err, pgperrors.ErrKeyExpired):
		return nil, badSignature("the signing key is expired")
	case errors.Is(err, pgperrors.ErrKeyRevoked):
		return nil, badSignature("the signing key is revoked")
	case errors.Is(err, pgperrors.ErrSignatureExpired):
		return nil, badSignature("the signature is expired")
	case err != nil:
		return nil, badSignature("the OpenPGP signature does not match the tag: %v", err)
	}
	return signer, nil
}

// issuer is the fingerprint or key ID of the key that made a signature.
func issuer(sig *packet.Signature) string {
	switch {
	case sig.IssuerFingerprint != nil:
		return fmt.Sprintf("%X", sig.IssuerFingerprint)
	case sig.IssuerKeyId != nil:
		return fmt.Sprintf("%016X", *sig.IssuerKeyId)
	}
	return "unknown"
}

// endsWithBlock reports whether the last line that is not space starts with end:
// nothing unsigned may follow the signature block.
func endsWithBlock(block []byte, end string) bool {
	trimmed := bytes.TrimRight(block, " \t\r\n")
	last := trimmed[bytes.LastIndexByte(trimmed, '\n')+1:]
	return bytes.HasPrefix(last, []byte(end))
}
