package global

import (
	"fmt"
	"os"
)

const restrictedKeyFileMode os.FileMode = 0o600

// WriteRestrictedKeyFile writes secret material to path with 0600, then chmod
// again so umask cannot leave group/other bits set.
func WriteRestrictedKeyFile(path string, data []byte) error {
	if err := os.WriteFile(path, data, restrictedKeyFileMode); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	if err := os.Chmod(path, restrictedKeyFileMode); err != nil {
		return fmt.Errorf("failed to chmod %s: %w", path, err)
	}
	return nil
}

// ReadRestrictedKeyFile refuses a non-regular file or one whose mode grants
// group/other access. A one-shot chmod to 0600 is attempted; if the bits
// remain, the read fails closed.
func ReadRestrictedKeyFile(path string) ([]byte, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	if fi.Mode().Perm()&0o177 != 0 {
		if err := os.Chmod(path, restrictedKeyFileMode); err != nil {
			return nil, fmt.Errorf("insecure permissions on %s, expected 0600: %w", path, err)
		}
		fi, err = os.Stat(path)
		if err != nil {
			return nil, err
		}
		if fi.Mode().Perm()&0o177 != 0 {
			return nil, fmt.Errorf("insecure permissions on %s, expected 0600", path)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", path, err)
	}
	return data, nil
}

func zeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
