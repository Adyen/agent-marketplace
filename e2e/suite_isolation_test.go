package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestE2EBinaryIsProcessLocal(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(filepath.Dir(cwd), binaryPath)
	if err != nil || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		t.Fatalf("suite binary must be outside the checkout: %q (%v)", binaryPath, err)
	}
	info, err := os.Stat(filepath.Dir(binaryPath))
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("suite binary directory must be private: %v (%v)", info, err)
	}
	env := newEnv(t)
	env.MustRunAM("--version")
}

func TestE2EParallelism(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  int
	}{{"", 4}, {"1", 1}, {"8", 8}, {"0", 4}, {"-1", 4}, {"invalid", 4}} {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv("E2E_PARALLEL", tc.value)
			if got := e2eParallelism(); got != tc.want {
				t.Fatalf("parallelism = %d, want %d", got, tc.want)
			}
		})
	}
}
