// Package fsx holds small filesystem helpers shared by the commands.
package fsx

import (
	"fmt"
	"os"
)

// ReadRegularFile reads path only if it is a regular file. Symlinks and other
// non-regular files (devices, FIFOs, directories) are rejected with an error
// naming the path, so untrusted repositories cannot make the tool read files
// outside the repository. A missing file returns an error wrapping
// os.ErrNotExist.
func ReadRegularFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file (symlinks are not followed)", path)
	}
	return os.ReadFile(path)
}
