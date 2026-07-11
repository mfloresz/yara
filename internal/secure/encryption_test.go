package secure

import (
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// key32 returns a deterministic 32-byte key for tests.
func key32() []byte {
	k := make([]byte, 32)
	for i := range k {
		k[i] = byte(i)
	}
	return k
}

func newTestEncryptor(t *testing.T) *Encryptor {
	t.Helper()
	env := base64.StdEncoding.EncodeToString(key32())
	enc, err := NewEncryptorFromConfig(env, "")
	if err != nil {
		t.Fatalf("NewEncryptorFromConfig: %v", err)
	}
	return enc
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	enc := newTestEncryptor(t)

	cases := []string{
		"hello world",
		"sk-1234567890abcdef",
		"unicode: café — 日本語",
		strings.Repeat("x", 4096),
	}
	for _, plain := range cases {
		encoded, err := enc.Encrypt(plain)
		if err != nil {
			t.Fatalf("Encrypt(%q): %v", plain, err)
		}
		if !strings.HasPrefix(encoded, cipherVersion+":") {
			t.Errorf("Encrypt(%q) = %q, want %q prefix", plain, encoded, cipherVersion+":")
		}
		got, err := enc.Decrypt(encoded)
		if err != nil {
			t.Fatalf("Decrypt: %v", err)
		}
		if got != plain {
			t.Errorf("round trip: got %q, want %q", got, plain)
		}
	}
}

func TestEncryptEmptyReturnsEmpty(t *testing.T) {
	enc := newTestEncryptor(t)
	for _, in := range []string{"", "   ", "\t\n"} {
		out, err := enc.Encrypt(in)
		if err != nil {
			t.Fatalf("Encrypt(%q): %v", in, err)
		}
		if out != "" {
			t.Errorf("Encrypt(%q) = %q, want empty", in, out)
		}
	}
}

func TestDecryptEmptyReturnsEmpty(t *testing.T) {
	enc := newTestEncryptor(t)
	for _, in := range []string{"", "   "} {
		out, err := enc.Decrypt(in)
		if err != nil {
			t.Fatalf("Decrypt(%q): %v", in, err)
		}
		if out != "" {
			t.Errorf("Decrypt(%q) = %q, want empty", in, out)
		}
	}
}

func TestEncryptProducesDistinctCiphertexts(t *testing.T) {
	enc := newTestEncryptor(t)
	a, err := enc.Encrypt("same input")
	if err != nil {
		t.Fatal(err)
	}
	b, err := enc.Encrypt("same input")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Error("expected distinct ciphertexts due to random nonce, got identical values")
	}
}

func TestDecryptUnsupportedVersion(t *testing.T) {
	enc := newTestEncryptor(t)
	for _, in := range []string{
		"v2:" + base64.StdEncoding.EncodeToString([]byte("payload")),
		"no-colon-payload",
		"badversion",
	} {
		if _, err := enc.Decrypt(in); err == nil {
			t.Errorf("Decrypt(%q) expected error, got nil", in)
		}
	}
}

func TestDecryptInvalidBase64(t *testing.T) {
	enc := newTestEncryptor(t)
	if _, err := enc.Decrypt(cipherVersion + ":not*valid*base64"); err == nil {
		t.Error("expected error for invalid base64 payload")
	}
}

func TestDecryptPayloadTooShort(t *testing.T) {
	enc := newTestEncryptor(t)
	short := base64.StdEncoding.EncodeToString([]byte{0x01, 0x02})
	if _, err := enc.Decrypt(cipherVersion + ":" + short); err == nil {
		t.Error("expected error for payload shorter than nonce")
	}
}

func TestDecryptTamperedCiphertextFails(t *testing.T) {
	enc := newTestEncryptor(t)
	encoded, err := enc.Encrypt("secret value")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.SplitN(encoded, ":", 2)
	raw, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)-1] ^= 0xFF // flip a bit in the auth tag / ciphertext
	tampered := cipherVersion + ":" + base64.StdEncoding.EncodeToString(raw)
	if _, err := enc.Decrypt(tampered); err == nil {
		t.Error("expected authentication failure for tampered ciphertext")
	}
}

func TestDecryptWithDifferentKeyFails(t *testing.T) {
	enc1 := newTestEncryptor(t)
	encoded, err := enc1.Encrypt("secret")
	if err != nil {
		t.Fatal(err)
	}

	otherKey := make([]byte, 32)
	for i := range otherKey {
		otherKey[i] = byte(255 - i)
	}
	enc2, err := NewEncryptorFromConfig(base64.StdEncoding.EncodeToString(otherKey), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := enc2.Decrypt(encoded); err == nil {
		t.Error("expected decryption with a different key to fail")
	}
}

func TestNewEncryptorRejectsMalformedKey(t *testing.T) {
	// An odd-length hex-like string is invalid as both base64 (bad padding)
	// and hex (odd digit count), exercising both branches of decodeKey.
	env := hex.EncodeToString(key32()) + "0"
	if _, err := NewEncryptorFromConfig(env, ""); err == nil {
		t.Error("expected error for malformed key")
	}
}

func TestNewEncryptorRejectsWrongKeyLength(t *testing.T) {
	short := base64.StdEncoding.EncodeToString([]byte("too-short-key"))
	if _, err := NewEncryptorFromConfig(short, ""); err == nil {
		t.Error("expected error for key that does not decode to 32 bytes")
	}
}

func TestNewEncryptorRejectsUndecodableKey(t *testing.T) {
	// A string that is neither valid base64 nor valid hex.
	if _, err := NewEncryptorFromConfig("!!!not-a-key!!!", ""); err == nil {
		t.Error("expected error for undecodable key")
	}
}

func TestResolveKeyGeneratesAndReusesFile(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "nested", "app.key")

	// First call with no env value should generate a key file.
	enc1, err := NewEncryptorFromConfig("", keyPath)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	if _, err := os.Stat(keyPath); err != nil {
		t.Fatalf("expected key file at %s: %v", keyPath, err)
	}

	encoded, err := enc1.Encrypt("persist me")
	if err != nil {
		t.Fatal(err)
	}

	// Second call should read the same key file and decrypt prior output.
	enc2, err := NewEncryptorFromConfig("", keyPath)
	if err != nil {
		t.Fatalf("reuse key: %v", err)
	}
	got, err := enc2.Decrypt(encoded)
	if err != nil {
		t.Fatalf("decrypt with reused key: %v", err)
	}
	if got != "persist me" {
		t.Errorf("got %q, want %q", got, "persist me")
	}
}

func TestResolveKeyRejectsCorruptKeyFile(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "app.key")
	if err := os.WriteFile(keyPath, []byte("!!!not-valid!!!"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewEncryptorFromConfig("", keyPath); err == nil {
		t.Error("expected error for corrupt key file")
	}
}

func TestResolveKeyRejectsWrongLengthKeyFile(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "app.key")
	content := base64.StdEncoding.EncodeToString([]byte("16-byte-keyxxxxx"))
	if err := os.WriteFile(keyPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewEncryptorFromConfig("", keyPath); err == nil {
		t.Error("expected error for key file that decodes to wrong length")
	}
}

func TestEnvKeyTakesPrecedenceOverFile(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "app.key")
	// Write a valid-but-different key to the file.
	fileKey := make([]byte, 32)
	for i := range fileKey {
		fileKey[i] = 0xAB
	}
	if err := os.WriteFile(keyPath, []byte(base64.StdEncoding.EncodeToString(fileKey)), 0o600); err != nil {
		t.Fatal(err)
	}

	env := base64.StdEncoding.EncodeToString(key32())
	enc, err := NewEncryptorFromConfig(env, keyPath)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := enc.Encrypt("value")
	if err != nil {
		t.Fatal(err)
	}

	// Env-key encryptor output must NOT be decryptable by the file key.
	fileEnc, err := NewEncryptorFromConfig("", keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fileEnc.Decrypt(encoded); err == nil {
		t.Error("expected env key to take precedence over file key")
	}
}
