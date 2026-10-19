//go:build windows

package accounts

import (
	"bytes"
	"testing"
)

func TestWindowsUserVaultRoundTripAndTamperRejection(t *testing.T) {
	v := UserVault{}
	secret := []byte("official-oauth-sentinel")
	a, e := v.Seal(secret)
	if e != nil {
		t.Fatal(e)
	}
	b, e := v.Seal(secret)
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Equal(a, b) || bytes.Contains(a, secret) {
		t.Fatal("DPAPI did not protect credential")
	}
	decoded, e := v.Open(a)
	if e != nil || !bytes.Equal(decoded, secret) {
		t.Fatal("DPAPI round trip failed")
	}
	a[len(a)/2] ^= 1
	if _, e = v.Open(a); e == nil {
		t.Fatal("tampered credential decrypted")
	}
}
