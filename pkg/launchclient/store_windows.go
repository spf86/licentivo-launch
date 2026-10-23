//go:build windows

package launchclient

import (
	"golang.org/x/sys/windows"
	"os"
	"unsafe"
)

// NewSystemSessionStore protects remembered sessions using Windows DPAPI bound
// to the current Windows account, with the scope as authenticated entropy.
func NewSystemSessionStore(directory string) (SessionStore, error) {
	if err := ProtectDirectory(directory); err != nil {
		return nil, err
	}
	return &protectedFileStore{directory: directory}, nil
}

// ProtectDirectory restricts SDK cache access to the OS user and SYSTEM. The
// SDK's own device credential file remains plaintext inside this private folder.
func ProtectDirectory(directory string) error {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	token := windows.GetCurrentProcessToken()
	user, err := token.GetTokenUser()
	if err != nil {
		return err
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;" + user.User.Sid.String() + ")")
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(directory, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
}
func blob(value []byte) windows.DataBlob {
	b := windows.DataBlob{Size: uint32(len(value))}
	if len(value) > 0 {
		b.Data = &value[0]
	}
	return b
}
func protect(value, entropy []byte) ([]byte, error) {
	in, salt := blob(value), blob(entropy)
	var out windows.DataBlob
	if err := windows.CryptProtectData(&in, nil, &salt, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, int(out.Size))...), nil
}
func unprotect(value, entropy []byte) ([]byte, error) {
	in, salt := blob(value), blob(entropy)
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(&in, nil, &salt, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	plaintext := unsafe.Slice(out.Data, int(out.Size))
	defer clear(plaintext)
	return append([]byte(nil), plaintext...), nil
}
