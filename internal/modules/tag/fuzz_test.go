package tag

import (
	"bytes"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
)

func FuzzParse(f *testing.F) {
	f.Add([]byte("version: 1\nrules:\n  prod:\n    match: ['/^(?:v.*)$/']\n    message: m\n    signers: [lead]\nkeys:\n  lead: ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl lead\n"))
	f.Add([]byte("version: 1\nkeys:\n  lead: |\n    -----BEGIN PGP PUBLIC KEY BLOCK-----\n    -----END PGP PUBLIC KEY BLOCK-----\n"))
	f.Add([]byte("version: 1\nkeys:\n  a: -----BEGIN OPENSSH PRIVATE KEY-----\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		p, err := parsePolicy(data, nil)
		if err != nil {
			return
		}
		// A parsed policy has a key for every signer and checks its keys without failing.
		for _, r := range p.rules {
			for _, s := range r.signers {
				if p.byLabel[s] == nil {
					t.Fatalf("signer %q without a key", s)
				}
			}
		}
		p.ValidateVersion(p, time.Unix(0, 0))
	})
}

func FuzzParseTagObject(f *testing.F) {
	f.Add([]byte("object 1111111111111111111111111111111111111111\ntype commit\ntag v1\ntagger a <a> 1 +0000\n\nmsg\n-----BEGIN PGP SIGNATURE-----\nabc\n-----END PGP SIGNATURE-----\n"))
	f.Add([]byte("object x\ntype tag\ntag v1\n\n-----BEGIN SSH SIGNATURE-----\nU1NIU0lH\n-----END SSH SIGNATURE-----\n"))
	f.Add([]byte("-----BEGIN SIGNED MESSAGE-----\n"))
	ring := openpgp.EntityList{}
	f.Fuzz(func(t *testing.T, data []byte) {
		obj, err := parseTag(data)
		if err != nil {
			return
		}
		// The payload and the signature split the object without a gap.
		if !bytes.Equal(append(append([]byte{}, obj.payload...), obj.sig...), data) {
			t.Fatal("payload and signature do not make up the object")
		}
		switch obj.kind {
		case sigSSH:
			_, _ = parseSSHSig(obj.sig)
		case sigOpenPGP:
			_, _ = verifyOpenPGP(ring, obj.payload, obj.sig, time.Unix(0, 0))
		}
	})
}

func FuzzSSHSig(f *testing.F) {
	f.Add([]byte("-----BEGIN SSH SIGNATURE-----\nU1NIU0lHAAAAAQ==\n-----END SSH SIGNATURE-----\n"))
	f.Add([]byte("-----BEGIN SSH SIGNATURE-----\n\n-----END SSH SIGNATURE-----"))
	f.Fuzz(func(t *testing.T, block []byte) {
		s, err := parseSSHSig(block)
		if err != nil {
			return
		}
		if s.hash != "sha256" && s.hash != "sha512" {
			t.Fatalf("hash %q accepted", s.hash)
		}
		_ = s.verify(s.pub, []byte("payload"))
	})
}
