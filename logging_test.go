package main

import (
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveLogDestination(t *testing.T) {
	t.Run("empty resolves to default", func(t *testing.T) {
		dest, err := resolveLogDestination("")
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		if !strings.HasSuffix(dest, filepath.Join("Library", "Logs", "Nebula Tray", "nebula-tray.log")) {
			t.Fatalf("default destination %q does not look like the expected default path", dest)
		}
		if !filepath.IsAbs(dest) {
			t.Fatalf("default destination %q is not absolute", dest)
		}
	})

	t.Run("stdout passes through unchanged", func(t *testing.T) {
		dest, err := resolveLogDestination("stdout")
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		if dest != "stdout" {
			t.Fatalf("expected %q, got %q", "stdout", dest)
		}
	})

	t.Run("relative path is normalized to absolute", func(t *testing.T) {
		dest, err := resolveLogDestination("some/relative/path.log")
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		if !filepath.IsAbs(dest) {
			t.Fatalf("expected absolute path, got %q", dest)
		}
		if !strings.HasSuffix(dest, filepath.Join("some", "relative", "path.log")) {
			t.Fatalf("expected suffix preserved, got %q", dest)
		}
	})

	t.Run("absolute path passes through unchanged", func(t *testing.T) {
		dest, err := resolveLogDestination("/tmp/custom/nebula-tray.log")
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		if dest != "/tmp/custom/nebula-tray.log" {
			t.Fatalf("expected path preserved, got %q", dest)
		}
	})
}

func TestResolveOriginalHomeDir(t *testing.T) {
	t.Run("non-root uses the process's own home dir", func(t *testing.T) {
		want, err := os.UserHomeDir()
		if err != nil {
			t.Skipf("no home dir available in this environment: %s", err)
		}
		got, err := resolveOriginalHomeDir(501, "someone-else")
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		if got != want {
			t.Fatalf("expected %q, got %q", want, got)
		}
	})

	t.Run("root with SUDO_USER resolves that user's home dir", func(t *testing.T) {
		cur, err := user.Current()
		if err != nil {
			t.Skipf("no current user available in this environment: %s", err)
		}
		got, err := resolveOriginalHomeDir(0, cur.Username)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		if got != cur.HomeDir {
			t.Fatalf("expected %q, got %q", cur.HomeDir, got)
		}
	})

	t.Run("root with an unresolvable SUDO_USER falls back to the process home dir", func(t *testing.T) {
		want, err := os.UserHomeDir()
		if err != nil {
			t.Skipf("no home dir available in this environment: %s", err)
		}
		got, err := resolveOriginalHomeDir(0, "definitely-not-a-real-user-xyz")
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		if got != want {
			t.Fatalf("expected fallback %q, got %q", want, got)
		}
	})
}

func TestOpenLogDestinationFile(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "nested", "nebula-tray.log")

	w, err := openLogDestination(dest)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if _, err := w.Write([]byte("first\n")); err != nil {
		t.Fatalf("unexpected write error: %s", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("unexpected close error: %s", err)
	}

	info, err := os.Stat(dest)
	if err != nil {
		t.Fatalf("expected log file to exist: %s", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("expected file mode 0600, got %o", perm)
	}

	// Reopen and write again to confirm append behavior, as required for
	// sequential launcher processes to share one continuous log.
	w2, err := openLogDestination(dest)
	if err != nil {
		t.Fatalf("unexpected error reopening: %s", err)
	}
	if _, err := w2.Write([]byte("second\n")); err != nil {
		t.Fatalf("unexpected write error: %s", err)
	}
	if err := w2.Close(); err != nil {
		t.Fatalf("unexpected close error: %s", err)
	}

	contents, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("unexpected read error: %s", err)
	}
	if string(contents) != "first\nsecond\n" {
		t.Fatalf("expected append behavior, got %q", string(contents))
	}
}

func TestOpenLogDestinationStdout(t *testing.T) {
	w, err := openLogDestination(stdoutDestination)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	// Closing the stdout destination must not close the process's real
	// stdout out from under it.
	if err := w.Close(); err != nil {
		t.Fatalf("unexpected close error: %s", err)
	}
	if _, err := os.Stdout.Stat(); err != nil {
		t.Fatalf("expected real stdout to remain usable: %s", err)
	}
}

func TestOpenLogDestinationErrorNoSilentFallback(t *testing.T) {
	dir := t.TempDir()
	// A file where a directory is expected makes both the directory and
	// the log file impossible to create.
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("unexpected setup error: %s", err)
	}
	dest := filepath.Join(blocker, "nebula-tray.log")

	_, err := openLogDestination(dest)
	if err == nil {
		t.Fatalf("expected an error for an unwritable destination, got nil")
	}
	if !strings.Contains(err.Error(), dest) && !strings.Contains(err.Error(), blocker) {
		t.Fatalf("expected error to reference the failing path, got %q", err)
	}
}

func TestElevatedRelaunchArgs(t *testing.T) {
	t.Run("without config test flag", func(t *testing.T) {
		args := elevatedRelaunchArgs("-elevate-via-osascript", "/etc/nebula/config.yml", false, "/var/log/nebula-tray.log")
		want := []string{"-elevate-via-osascript", "-config", "/etc/nebula/config.yml", "-log-output", "/var/log/nebula-tray.log"}
		if !equalStrings(args, want) {
			t.Fatalf("got %v, want %v", args, want)
		}
	})

	t.Run("with config test flag", func(t *testing.T) {
		args := elevatedRelaunchArgs("-elevate-attempted", "/etc/nebula/config.yml", true, "stdout")
		want := []string{"-elevate-attempted", "-config", "/etc/nebula/config.yml", "-log-output", "stdout", "-test"}
		if !equalStrings(args, want) {
			t.Fatalf("got %v, want %v", args, want)
		}
	})

	t.Run("propagates the resolved destination, not a default marker", func(t *testing.T) {
		args := elevatedRelaunchArgs("-elevate-via-osascript", "/etc/nebula/config.yml", false, "/Users/alice/Library/Logs/Nebula Tray/nebula-tray.log")
		found := false
		for i, a := range args {
			if a == "-log-output" && i+1 < len(args) {
				found = args[i+1] == "/Users/alice/Library/Logs/Nebula Tray/nebula-tray.log"
			}
		}
		if !found {
			t.Fatalf("expected -log-output to carry the resolved destination, got %v", args)
		}
	})
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
