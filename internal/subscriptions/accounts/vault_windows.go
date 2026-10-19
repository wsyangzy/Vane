//go:build windows

package accounts

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

// UserVault uses Windows DPAPI for the current Windows user, without UI.
// Moving the database alone cannot expose the subscription credentials.
type UserVault struct{}

func (UserVault) Seal(data []byte) ([]byte, error) { return protect(data, false) }
func (UserVault) Open(data []byte) ([]byte, error) { return protect(data, true) }
func protect(data []byte, decrypt bool) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("empty_secret")
	}
	in := windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
	var out windows.DataBlob
	var err error
	if decrypt {
		err = windows.CryptUnprotectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	} else {
		err = windows.CryptProtectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	}
	if err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, int(out.Size))...), nil
}
