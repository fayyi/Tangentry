package backup

import (
	"bytes"
	"testing"
)

func TestEncryptDecryptAndTamperDetection(t *testing.T) {
	manifest := Manifest{KDF: KDFParams{Salt: "00112233445566778899aabbccddeeff"}}
	plain := bytes.Repeat([]byte("fixture archive contents"), 90000)
	encoded, err := Encrypt(plain, "fixture-password", &manifest)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decrypt(encoded, "fixture-password")
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("round trip failed: %v", err)
	}
	if _, err := Decrypt(encoded, "wrong-password"); err != ErrBadPassword {
		t.Fatalf("wrong password: %v", err)
	}
	encoded[len(encoded)-1] ^= 1
	if _, err := Decrypt(encoded, "fixture-password"); err != ErrBadPassword {
		t.Fatalf("tamper: %v", err)
	}
	if _, err := Decrypt(encoded[:len(encoded)-3], "fixture-password"); err == nil {
		t.Fatal("truncated package accepted")
	}
}

func TestCleanRelativeRejectsTraversal(t *testing.T) {
	for _, value := range []string{"../secret", "/absolute", `..\secret`} {
		if _, err := cleanRelative(value); err == nil {
			t.Fatalf("accepted unsafe path %q", value)
		}
	}
}
