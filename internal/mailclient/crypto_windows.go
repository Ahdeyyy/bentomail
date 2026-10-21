//go:build windows

package mailclient

import (
	"errors"
	"syscall"
	"unsafe"
)

var (
	dllCrypt32             = syscall.NewLazyDLL("crypt32.dll")
	procCryptProtectData   = dllCrypt32.NewProc("CryptProtectData")
	procCryptUnprotectData = dllCrypt32.NewProc("CryptUnprotectData")
)

type dataBlob struct {
	cbData uint32
	pbData *byte
}

func newBlob(d []byte) dataBlob {
	if len(d) == 0 {
		return dataBlob{}
	}
	return dataBlob{
		cbData: uint32(len(d)),
		pbData: &d[0],
	}
}

func (b *dataBlob) toByteArray() []byte {
	if b.cbData == 0 || b.pbData == nil {
		return nil
	}
	d := make([]byte, b.cbData)
	copy(d, unsafe.Slice(b.pbData, b.cbData))
	return d
}

// encryptSecret encrypts data using Windows DPAPI (CryptProtectData).
func encryptSecret(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}
	inBlob := newBlob(data)
	var outBlob dataBlob
	// CRYPTPROTECT_UI_FORBIDDEN = 0x01
	r, _, err := procCryptProtectData.Call(
		uintptr(unsafe.Pointer(&inBlob)),
		0,
		0,
		0,
		0,
		0x01,
		uintptr(unsafe.Pointer(&outBlob)),
	)
	if r == 0 {
		if err != nil && !errors.Is(err, syscall.Errno(0)) {
			return nil, err
		}
		return nil, errors.New("CryptProtectData failed")
	}
	defer syscall.LocalFree(syscall.Handle(unsafe.Pointer(outBlob.pbData)))
	return outBlob.toByteArray(), nil
}

// decryptSecret decrypts data using Windows DPAPI (CryptUnprotectData).
func decryptSecret(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}
	inBlob := newBlob(data)
	var outBlob dataBlob
	// CRYPTPROTECT_UI_FORBIDDEN = 0x01
	r, _, err := procCryptUnprotectData.Call(
		uintptr(unsafe.Pointer(&inBlob)),
		0,
		0,
		0,
		0,
		0x01,
		uintptr(unsafe.Pointer(&outBlob)),
	)
	if r == 0 {
		if err != nil && !errors.Is(err, syscall.Errno(0)) {
			return nil, err
		}
		return nil, errors.New("CryptUnprotectData failed")
	}
	defer syscall.LocalFree(syscall.Handle(unsafe.Pointer(outBlob.pbData)))
	return outBlob.toByteArray(), nil
}
