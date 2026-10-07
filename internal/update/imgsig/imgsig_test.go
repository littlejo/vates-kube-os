package imgsig

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
)

// keypair mints an Ed25519 pair. A test that needs "another key" calls it again:
// the point of several tests is that a signature from one key does not satisfy
// another.
func keypair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return pub, priv
}

// artifact returns a body a little larger than one SHA-256 block, so the
// streaming path is exercised rather than a constant-size buffer.
func artifact(t *testing.T) []byte {
	t.Helper()
	b := make([]byte, 1<<12+7)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	return b
}

func TestSignVerifyRoundTrip(t *testing.T) {
	pub, priv := keypair(t)
	body := artifact(t)

	sig, err := Sign(priv, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if err := Verify(bytes.NewReader(body), sig, []PublicKey{NewPublicKey(pub)}); err != nil {
		t.Fatalf("Verify: %v", err)
	}
}

func TestSignSetsKeyIDAndDigest(t *testing.T) {
	pub, priv := keypair(t)
	body := artifact(t)

	sig, err := Sign(priv, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	want, err := Digest(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	if sig.SHA256 != want {
		t.Errorf("SHA256 = %s, want %s", sig.SHA256, want)
	}
	if sig.KeyID != NewPublicKey(pub).KeyID {
		t.Errorf("KeyID = %s, want %s", sig.KeyID, NewPublicKey(pub).KeyID)
	}
}

func TestVerifyDetectsTamperedImage(t *testing.T) {
	pub, priv := keypair(t)
	body := artifact(t)

	sig, err := Sign(priv, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	body[len(body)/2] ^= 0xff

	err = Verify(bytes.NewReader(body), sig, []PublicKey{NewPublicKey(pub)})
	if err == nil {
		t.Fatal("Verify accepted a tampered image")
	}
	if !strings.Contains(err.Error(), "does not match its published digest") {
		t.Errorf("error = %q, want it to name the digest mismatch", err)
	}
}

func TestVerifyDetectsTamperedSignature(t *testing.T) {
	pub, priv := keypair(t)
	body := artifact(t)

	sig, err := Sign(priv, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	sig.Raw[0] ^= 0xff

	err = Verify(bytes.NewReader(body), sig, []PublicKey{NewPublicKey(pub)})
	if err == nil {
		t.Fatal("Verify accepted a tampered signature")
	}
	if !strings.Contains(err.Error(), "signature is invalid") {
		t.Errorf("error = %q, want it to name the signature", err)
	}
}

func TestVerifyRejectsUntrustedKey(t *testing.T) {
	_, priv := keypair(t)
	otherPub, _ := keypair(t)
	body := artifact(t)

	sig, err := Sign(priv, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	// The signature's keyid belongs to the signer, so the trusted set is empty
	// of a match: this is the "no key of ours" path, not the "bad signature" one.
	err = Verify(bytes.NewReader(body), sig, []PublicKey{NewPublicKey(otherPub)})
	if err == nil {
		t.Fatal("Verify accepted a signature from an untrusted key")
	}
	if !strings.Contains(err.Error(), "no trusted key has id") {
		t.Errorf("error = %q, want it to name the missing key id", err)
	}
}

func TestVerifyWithoutKeyIDTriesEveryKey(t *testing.T) {
	pub, priv := keypair(t)
	otherPub, _ := keypair(t)
	body := artifact(t)

	sig, err := Sign(priv, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	sig.KeyID = "" // a signature that names no key
	keys := []PublicKey{NewPublicKey(otherPub), NewPublicKey(pub)}

	if err := Verify(bytes.NewReader(body), sig, keys); err != nil {
		t.Fatalf("Verify with several keys and no keyid: %v", err)
	}
}

func TestVerifyUnknownKeyID(t *testing.T) {
	pub, priv := keypair(t)
	body := artifact(t)

	sig, err := Sign(priv, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	sig.KeyID = "0011223344556677"

	err = Verify(bytes.NewReader(body), sig, []PublicKey{NewPublicKey(pub)})
	if err == nil || !strings.Contains(err.Error(), "no trusted key has id") {
		t.Fatalf("Verify = %v, want a missing-key-id error", err)
	}
}

func TestVerifyRejectsMissingInputs(t *testing.T) {
	pub, _ := keypair(t)
	body := artifact(t)
	key := []PublicKey{NewPublicKey(pub)}

	if err := Verify(bytes.NewReader(body), nil, key); err == nil {
		t.Error("Verify accepted a nil signature")
	}
	sig := &Signature{SHA256: strings.Repeat("0", 64), Raw: make([]byte, ed25519.SignatureSize)}
	if err := Verify(bytes.NewReader(body), sig, nil); err == nil {
		t.Error("Verify accepted an empty key set")
	}
}

func TestSignatureTextRoundTrip(t *testing.T) {
	_, priv := keypair(t)
	body := artifact(t)

	sig, err := Sign(priv, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	parsed, err := ParseSignature([]byte(sig.String()))
	if err != nil {
		t.Fatalf("ParseSignature: %v", err)
	}
	if parsed.KeyID != sig.KeyID || parsed.SHA256 != sig.SHA256 || !bytes.Equal(parsed.Raw, sig.Raw) {
		t.Errorf("parse round trip changed the signature: got %+v, want %+v", parsed, sig)
	}
}

func TestParseSignatureIgnoresAnnotations(t *testing.T) {
	_, priv := keypair(t)
	body := artifact(t)
	sig, err := Sign(priv, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	text := "# a comment\n" + sig.String() + "\npublished 2026-10-02\n"
	parsed, err := ParseSignature([]byte(text))
	if err != nil {
		t.Fatalf("ParseSignature with annotations: %v", err)
	}
	if parsed.SHA256 != sig.SHA256 {
		t.Errorf("SHA256 = %s, want %s", parsed.SHA256, sig.SHA256)
	}
}

func TestParseSignatureErrors(t *testing.T) {
	valid := strings.Repeat("ab", 32)
	sig64 := base64.StdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))

	cases := []struct {
		name string
		text string
		want string
	}{
		{"empty", "", "empty signature"},
		{"wrong header", "some-other-format\nsha256 " + valid + "\nsig " + sig64 + "\n", "not a vates image signature"},
		{"no sha256", magic + "\nsig " + sig64 + "\n", "no sha256 line"},
		{"short sha256", magic + "\nsha256 abc\nsig " + sig64 + "\n", "characters, not 64"},
		{"bad sha256", magic + "\nsha256 " + strings.Repeat("zz", 32) + "\nsig " + sig64 + "\n", "not hexadecimal"},
		{"no sig", magic + "\nsha256 " + valid + "\n", "no sig line"},
		{"bad base64 sig", magic + "\nsha256 " + valid + "\nsig not-base64!!\n", "not base64"},
		{"short sig", magic + "\nsha256 " + valid + "\nsig " + base64.StdEncoding.EncodeToString([]byte("short")) + "\n", "bytes, not 64"},
		{"bad keyid", magic + "\nkeyid abc\nsha256 " + valid + "\nsig " + sig64 + "\n", "characters, not 16"},
		{"non-hex keyid", magic + "\nkeyid " + strings.Repeat("zz", 8) + "\nsha256 " + valid + "\nsig " + sig64 + "\n", "not hexadecimal"},
		{"not key value", magic + "\nsha256\n", "is not \"key value\""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseSignature([]byte(tc.text))
			if err == nil {
				t.Fatal("ParseSignature accepted invalid input")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestParsePublicKeyForms(t *testing.T) {
	pub, _ := keypair(t)
	want := NewPublicKey(pub)

	pemBytes, err := want.PEM()
	if err != nil {
		t.Fatalf("PEM: %v", err)
	}
	for name, data := range map[string][]byte{
		"pem":    pemBytes,
		"base64": []byte(base64.StdEncoding.EncodeToString(pub)),
		"hex":    []byte(hex.EncodeToString(pub)),
	} {
		t.Run(name, func(t *testing.T) {
			got, err := ParsePublicKey(data)
			if err != nil {
				t.Fatalf("ParsePublicKey: %v", err)
			}
			if got.KeyID != want.KeyID || !bytes.Equal(got.Key, want.Key) {
				t.Errorf("key = %+v, want %+v", got, want)
			}
		})
	}
}

func TestParsePublicKeyErrors(t *testing.T) {
	cases := []struct {
		name string
		data string
		want string
	}{
		{"garbage", "not a key", "neither PEM, base64 nor hex"},
		{"short base64", base64.StdEncoding.EncodeToString([]byte("short")), "bytes, not 32"},
		{"wrong pem type", "-----BEGIN PRIVATE KEY-----\nAAAA\n-----END PRIVATE KEY-----\n", "not a public key"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParsePublicKey([]byte(tc.data))
			if err == nil {
				t.Fatal("ParsePublicKey accepted invalid input")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestDigestKnownVector(t *testing.T) {
	// SHA-256 of the empty input, the one value worth pinning: it catches a hasher
	// wired to the wrong algorithm without needing a fixture.
	got, err := Digest(bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	const want = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if got != want {
		t.Errorf("Digest(empty) = %s, want %s", got, want)
	}
}

func TestSignRejectsBadKeyLength(t *testing.T) {
	if _, err := Sign(ed25519.PrivateKey([]byte("too short")), bytes.NewReader(nil)); err == nil {
		t.Fatal("Sign accepted a malformed private key")
	}
}
