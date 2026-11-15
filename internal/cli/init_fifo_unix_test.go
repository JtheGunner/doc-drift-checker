//go:build unix

package cli_test

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/JtheGunner/doc-drift-checker/internal/testutil"
)

// TestInitAbortsOnFIFODoc covers the FIFO case of the fail-closed unsafe-doc
// behaviour (see TestInitAbortsOnSymlinkOrNonRegularDoc). Git cannot track a
// FIFO, so the tracked doc is replaced on disk. syscall.Mkfifo is unix-only.
func TestInitAbortsOnFIFODoc(t *testing.T) {
	assertInitAbortsOnUnsafeDoc(t, "tracked doc replaced by a FIFO", func(t *testing.T, r *testutil.Repo) {
		path := filepath.Join(r.Dir, "README.md")
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := syscall.Mkfifo(path, 0o644); err != nil {
			t.Skipf("fifo unavailable: %v", err)
		}
	})
}
