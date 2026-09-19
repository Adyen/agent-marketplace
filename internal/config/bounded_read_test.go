package config

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func writeBoundedConfigTestFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestReadConfigFileBoundsAndRegularFiles(t *testing.T) {
	dir := t.TempDir()
	prefix := "registries: []\n#"
	exact := prefix + strings.Repeat("x", int(MaxEffectiveConfigSize)-len(prefix))
	exactPath := filepath.Join(dir, "exact.yaml")
	if err := os.WriteFile(exactPath, []byte(exact), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readConfigFile(exactPath)
	if err != nil || string(got) != exact {
		t.Fatalf("exact read: len=%d err=%v", len(got), err)
	}
	tooLargePath := filepath.Join(dir, "too-large.yaml")
	if err := os.WriteFile(tooLargePath, []byte(exact+" "), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readConfigFile(tooLargePath); !errors.Is(err, ErrConfigTooLarge) {
		t.Fatalf("too-large error = %v, want ErrConfigTooLarge", err)
	}
	emptyPath := filepath.Join(dir, "empty.yaml")
	if err := os.WriteFile(emptyPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := readConfigFile(emptyPath); err != nil || len(got) != 0 {
		t.Fatalf("empty read: len=%d err=%v", len(got), err)
	}
}

func TestConfigLoadersRejectOversizedRootsAndIncludes(t *testing.T) {
	dir := t.TempDir()
	large := "registries: []\n#" + strings.Repeat("x", int(MaxEffectiveConfigSize)) + "\n"
	root := filepath.Join(dir, "config.yaml")
	writeBoundedConfigTestFile(t, root, "apiVersion: agent-marketplace/v2\ninclude: large.yaml\nregistries: []\n")
	writeBoundedConfigTestFile(t, filepath.Join(dir, "large.yaml"), large)
	if _, err := LoadConfigFromPath(root); !errors.Is(err, ErrConfigTooLarge) {
		t.Fatalf("included config error = %v, want ErrConfigTooLarge", err)
	}
	rootOnly := filepath.Join(dir, "root-only.yaml")
	writeBoundedConfigTestFile(t, rootOnly, "apiVersion: agent-marketplace/v2\n"+strings.Repeat("#", int(MaxEffectiveConfigSize))+"x\n")
	if _, err := LoadRootConfigFromPath(rootOnly); !errors.Is(err, ErrConfigTooLarge) {
		t.Fatalf("root config error = %v, want ErrConfigTooLarge", err)
	}
	if _, err := LoadConfigFromPath(rootOnly); !errors.Is(err, ErrConfigTooLarge) {
		t.Fatalf("LoadConfigFromPath root error = %v, want ErrConfigTooLarge", err)
	}
	localRoot := filepath.Join(dir, "config.local.yaml")
	writeBoundedConfigTestFile(t, localRoot, "include: large-local.yaml\n")
	writeBoundedConfigTestFile(t, filepath.Join(dir, "large-local.yaml"), large)
	if _, err := LoadLocalConfigFromPath(localRoot); !errors.Is(err, ErrConfigTooLarge) {
		t.Fatalf("local include error = %v, want ErrConfigTooLarge", err)
	}
}

func TestV4ConfigConsumersEnforceBoundedRegularFiles(t *testing.T) {
	dir := t.TempDir()
	valid := "apiVersion: agent-marketplace/v4\nsources: {}\nresources: {}\n"
	root := filepath.Join(dir, "config.yaml")
	writeBoundedConfigTestFile(t, root, valid)
	rootLink := filepath.Join(dir, "config-link.yaml")
	if err := os.Symlink(root, rootLink); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadV4ConfigFromPath(rootLink); err != nil {
		t.Fatalf("regular root symlink: %v", err)
	}

	oversizedRoot := filepath.Join(dir, "oversized-root.yaml")
	writeBoundedConfigTestFile(t, oversizedRoot, valid+strings.Repeat("#", int(MaxEffectiveConfigSize)))
	for name, load := range map[string]func(string) error{
		"v4 loader":      func(path string) error { _, err := LoadV4ConfigFromPath(path); return err },
		"unified loader": func(path string) error { _, err := LoadConfigFromPath(path); return err },
	} {
		t.Run(name+" oversized root", func(t *testing.T) {
			if err := load(oversizedRoot); !errors.Is(err, ErrConfigTooLarge) {
				t.Fatalf("error = %v, want ErrConfigTooLarge", err)
			}
		})
	}

	include := filepath.Join(dir, "fragment.yaml")
	writeBoundedConfigTestFile(t, root, "apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\n")
	writeBoundedConfigTestFile(t, include, strings.Repeat("#", int(MaxEffectiveConfigSize)+1))
	if _, err := LoadV4ConfigFromPath(root); !errors.Is(err, ErrConfigTooLarge) {
		t.Fatalf("oversized include error = %v, want ErrConfigTooLarge", err)
	}

	directory := filepath.Join(dir, "directory")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{"root": directory, "include": root} {
		t.Run("non-regular "+name, func(t *testing.T) {
			if name == "include" {
				writeBoundedConfigTestFile(t, root, "apiVersion: agent-marketplace/v4\ninclude: directory\n")
			}
			if _, err := LoadV4ConfigFromPath(path); !errors.Is(err, ErrConfigNotRegular) {
				t.Fatalf("error = %v, want ErrConfigNotRegular", err)
			}
		})
	}

	legacy := filepath.Join(dir, "legacy.yaml")
	writeBoundedConfigTestFile(t, legacy, "apiVersion: agent-marketplace/v2\n"+strings.Repeat("#", int(MaxEffectiveConfigSize)))
	if _, err := FindV4MigrationCandidates(MigratePaths{User: legacy}); !errors.Is(err, ErrConfigTooLarge) {
		t.Fatalf("migration candidate error = %v, want ErrConfigTooLarge", err)
	}
}

func TestV4ConfigLoaderFIFOChild(t *testing.T) {
	if path := os.Getenv("V4_CONFIG_FIFO_PATH"); path != "" {
		_, err := LoadV4ConfigFromPath(path)
		if !errors.Is(err, ErrConfigNotRegular) {
			t.Fatalf("FIFO error = %v, want ErrConfigNotRegular", err)
		}
		return
	}
	fifo := filepath.Join(t.TempDir(), "config.fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestV4ConfigLoaderFIFOChild$", "-test.v")
	cmd.Env = append(os.Environ(), "V4_CONFIG_FIFO_PATH="+fifo)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("v4 FIFO child: %v\n%s", err, output)
	}
}

func TestConfigFileOverlaysRejectOversizedInclude(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "config.yaml")
	include := filepath.Join(dir, "fragment.yaml")
	writeBoundedConfigTestFile(t, root, "apiVersion: agent-marketplace/v2\ninclude: fragment.yaml\nregistries: []\n")
	baseline := []byte("registries: []\n")
	writeBoundedConfigTestFile(t, include, string(baseline))
	rootData := []byte("apiVersion: agent-marketplace/v2\ninclude: fragment.yaml\nregistries: []\n")
	overlays := map[string][]byte{include: []byte("registries: []\n#" + strings.Repeat("x", int(MaxEffectiveConfigSize)) + "\n")}
	if _, _, err := loadConfigTreeWithReads(root, root, rootData, repoLayer, overlays); !errors.Is(err, ErrConfigTooLarge) {
		t.Fatalf("overlay error = %v, want ErrConfigTooLarge", err)
	}
	got, err := os.ReadFile(include)
	if err != nil || string(got) != string(baseline) {
		t.Fatalf("baseline include changed: %q err=%v", got, err)
	}
}

func TestReadConfigFileRegularSymlinkAndSpecialFiles(t *testing.T) {
	dir := t.TempDir()
	regular := filepath.Join(dir, "regular.yaml")
	writeBoundedConfigTestFile(t, regular, "registries: []\n")
	link := filepath.Join(dir, "link.yaml")
	if err := os.Symlink(regular, link); err != nil {
		t.Fatal(err)
	}
	if got, err := readConfigFile(link); err != nil || string(got) != "registries: []\n" {
		t.Fatalf("regular symlink read: %q err=%v", got, err)
	}
	directory := filepath.Join(dir, "directory.yaml")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := readConfigFile(directory); !errors.Is(err, ErrConfigNotRegular) {
		t.Fatalf("directory error = %v, want ErrConfigNotRegular", err)
	}
	directoryLink := filepath.Join(dir, "directory-link.yaml")
	if err := os.Symlink(directory, directoryLink); err != nil {
		t.Fatal(err)
	}
	if _, err := readConfigFile(directoryLink); !errors.Is(err, ErrConfigNotRegular) {
		t.Fatalf("directory symlink error = %v, want ErrConfigNotRegular", err)
	}
	zeroLink := filepath.Join(dir, "zero-link.yaml")
	if err := os.Symlink("/dev/zero", zeroLink); err != nil {
		t.Fatal(err)
	}
	if _, err := readConfigFile(zeroLink); !errors.Is(err, ErrConfigNotRegular) {
		t.Fatalf("/dev/zero symlink error = %v, want ErrConfigNotRegular", err)
	}
	if _, err := readConfigFile("/dev/zero"); !errors.Is(err, ErrConfigNotRegular) {
		t.Fatalf("/dev/zero error = %v, want ErrConfigNotRegular", err)
	}
}

func BenchmarkLoadBoundedIncludes(b *testing.B) {
	dir := b.TempDir()
	root := filepath.Join(dir, "config.yaml")
	write := func(path, data string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			b.Fatal(err)
		}
	}
	write(root, "apiVersion: agent-marketplace/v2\ninclude: fragment.yaml\nregistries: []\n")
	write(filepath.Join(dir, "fragment.yaml"), "registries: []\n")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := LoadConfigFromPath(root); err != nil {
			b.Fatal(err)
		}
	}
}

func TestReadOpenedConfigFileRestoresBlockingRegularDescriptor(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	data := []byte("registries: []\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	readFlags := func() int {
		var flags int
		var controlErr error
		conn, err := file.SyscallConn()
		if err != nil {
			t.Fatal(err)
		}
		if err := conn.Control(func(fd uintptr) { flags, controlErr = unix.FcntlInt(fd, unix.F_GETFL, 0) }); err != nil {
			t.Fatal(err)
		}
		if controlErr != nil {
			t.Fatal(controlErr)
		}
		return flags
	}
	if readFlags()&unix.O_NONBLOCK == 0 {
		t.Fatal("fixture descriptor did not start nonblocking")
	}
	got, err := readOpenedConfigFile(file)
	if err != nil || string(got) != string(data) {
		t.Fatalf("read = %q err=%v", got, err)
	}
	if readFlags()&unix.O_NONBLOCK != 0 {
		t.Fatal("descriptor remained nonblocking")
	}
}

func TestReadOpenedConfigFileRejectsFIFOBeforeClearingNonblock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.fifo")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	if _, err := readOpenedConfigFile(file); !errors.Is(err, ErrConfigNotRegular) {
		t.Fatalf("error = %v, want ErrConfigNotRegular", err)
	}
	var flags int
	conn, err := file.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Control(func(fd uintptr) { flags, err = unix.FcntlInt(fd, unix.F_GETFL, 0) }); err != nil {
		t.Fatal(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	if flags&unix.O_NONBLOCK == 0 {
		t.Fatalf("FIFO descriptor became blocking: %#x", flags)
	}
}

func TestReadConfigFileFIFOChild(t *testing.T) {
	if path := os.Getenv("CONFIG_FIFO_PATH"); path != "" {
		_, err := readConfigFile(path)
		if !errors.Is(err, ErrConfigNotRegular) {
			t.Fatalf("FIFO error = %v, want ErrConfigNotRegular", err)
		}
		return
	}
	dir := t.TempDir()
	fifo := filepath.Join(dir, "fifo.yaml")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		path string
	}{
		{name: "direct", path: fifo},
		{name: "symlink", path: filepath.Join(dir, "fifo-link.yaml")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "symlink" {
				if err := os.Symlink(fifo, tc.path); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestReadConfigFileFIFOChild$", "-test.v")
			cmd.Env = append(os.Environ(), "CONFIG_FIFO_PATH="+tc.path)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("FIFO child: %v\n%s", err, output)
			}
		})
	}
}
