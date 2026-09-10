//go:build windows

package secrets

import (
	"syscall"
	"unsafe"
)

const (
	credTypeGeneric         = 1
	credPersistLocalMachine = 2
)

type nativeCREDENTIAL struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        syscall.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

var (
	modadvapi32     = syscall.NewLazyDLL("advapi32.dll")
	procCredReadW   = modadvapi32.NewProc("CredReadW")
	procCredWriteW  = modadvapi32.NewProc("CredWriteW")
	procCredDeleteW = modadvapi32.NewProc("CredDeleteW")
	procCredFree    = modadvapi32.NewProc("CredFree")
)

type osBackend struct{}

func newOSBackend() backend { return osBackend{} }

func credTarget(id string) string {
	return Service + ":" + id
}

func (osBackend) Get(id string) (string, error) {
	target, err := syscall.UTF16PtrFromString(credTarget(id))
	if err != nil {
		return "", err
	}
	var cred *nativeCREDENTIAL
	r0, _, e1 := procCredReadW.Call(uintptr(unsafe.Pointer(target)), credTypeGeneric, 0, uintptr(unsafe.Pointer(&cred)))
	if r0 == 0 {
		if e1 == syscall.ERROR_NOT_FOUND {
			return "", ErrNotFound
		}
		return "", e1
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(cred)))
	if cred == nil || cred.CredentialBlob == nil || cred.CredentialBlobSize == 0 {
		return "", ErrNotFound
	}
	blob := unsafe.Slice(cred.CredentialBlob, cred.CredentialBlobSize)
	return string(blob), nil
}

func (osBackend) Set(id, secret string) error {
	target, err := syscall.UTF16PtrFromString(credTarget(id))
	if err != nil {
		return err
	}
	blob := []byte(secret)
	var blobPtr *byte
	if len(blob) > 0 {
		blobPtr = &blob[0]
	}
	cred := nativeCREDENTIAL{
		Type:               credTypeGeneric,
		TargetName:         target,
		CredentialBlobSize: uint32(len(blob)),
		CredentialBlob:     blobPtr,
		Persist:            credPersistLocalMachine,
	}
	r0, _, e1 := procCredWriteW.Call(uintptr(unsafe.Pointer(&cred)), 0)
	if r0 == 0 {
		return e1
	}
	return nil
}

func (osBackend) Delete(id string) error {
	target, err := syscall.UTF16PtrFromString(credTarget(id))
	if err != nil {
		return err
	}
	r0, _, e1 := procCredDeleteW.Call(uintptr(unsafe.Pointer(target)), credTypeGeneric, 0)
	if r0 == 0 {
		if e1 == syscall.ERROR_NOT_FOUND {
			return ErrNotFound
		}
		return e1
	}
	return nil
}
