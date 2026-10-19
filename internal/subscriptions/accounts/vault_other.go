//go:build !windows

package accounts

import "errors"

// Fail closed until the host has a native user-secret store adapter.
type UserVault struct{}

func (UserVault) Seal([]byte) ([]byte, error) {
	return nil, errors.New("native_secret_store_unavailable")
}
func (UserVault) Open([]byte) ([]byte, error) {
	return nil, errors.New("native_secret_store_unavailable")
}
