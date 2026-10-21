//go:build !windows

package mailclient

import "errors"

func encryptSecret([]byte) ([]byte, error) {
	return nil, errors.New("credential protection is currently supported on Windows only")
}
func decryptSecret([]byte) ([]byte, error) {
	return nil, errors.New("credential protection is currently supported on Windows only")
}
