package main

import (
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"

	"github.com/sirupsen/logrus"
)

// Logs must survive the three-process elevation handoff described in
// elevate.go. Rather than passing an open file descriptor across that
// boundary, every process resolves the same destination independently and
// opens it itself in append mode - see resolveLogDestination and
// openLogDestination.

// stdoutDestination is the reserved -log-output value that selects standard
// output, for direct/already-elevated CLI runs. It is never valid for a
// process that still needs to elevate: see main.go's guard around
// resolveLogDestination.
const stdoutDestination = "stdout"

// resolveLogDestination turns the -log-output flag value into an absolute
// destination: the reserved value "stdout", or an absolute file path. An
// empty raw value resolves to the default per-user log path. Called once,
// in the initial unprivileged process, before elevation; every relaunch hop
// then carries the already-resolved value forward via -log-output so a
// root child never re-resolves the default (see elevatedRelaunchArgs).
func resolveLogDestination(raw string) (string, error) {
	if raw == "" {
		return defaultLogPath()
	}
	if raw == stdoutDestination {
		return stdoutDestination, nil
	}
	abs, err := filepath.Abs(raw)
	if err != nil {
		return "", fmt.Errorf("resolving log output path %q: %w", raw, err)
	}
	return abs, nil
}

// defaultLogPath returns ~/Library/Logs/Nebula Tray/nebula-tray.log, where
// ~ belongs to the user who launched the app, not /var/root.
func defaultLogPath() (string, error) {
	home, err := originalUserHomeDir()
	if err != nil {
		return "", fmt.Errorf("determining home directory for default log path: %w", err)
	}
	return filepath.Join(home, "Library", "Logs", "Nebula Tray", "nebula-tray.log"), nil
}

// originalUserHomeDir returns the home directory of the user who launched
// the app. Under the normal elevation flow this is called from the
// initial, not-yet-elevated process, so os.UserHomeDir() is already
// correct. It falls back to SUDO_USER to cover a direct `sudo` launch that
// bypasses that flow entirely.
func originalUserHomeDir() (string, error) {
	return resolveOriginalHomeDir(os.Geteuid(), os.Getenv("SUDO_USER"))
}

func resolveOriginalHomeDir(euid int, sudoUser string) (string, error) {
	if euid == 0 && sudoUser != "" {
		if u, err := user.Lookup(sudoUser); err == nil {
			return u.HomeDir, nil
		}
	}
	return os.UserHomeDir()
}

// openLogDestination opens dest for logging. A file destination is opened
// in append mode so sequential launcher processes and the final app
// contribute to one continuous log; missing parent directories are created
// with user-private permissions, and the file itself is created 0600
// (subject to the process umask). There is no silent fallback: a failure
// here is meant to be surfaced to the user and treated as fatal.
func openLogDestination(dest string) (io.WriteCloser, error) {
	if dest == stdoutDestination {
		return nopWriteCloser{os.Stdout}, nil
	}

	dir := filepath.Dir(dest)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("creating log directory %q: %w", dir, err)
	}

	f, err := os.OpenFile(dest, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("opening log file %q: %w", dest, err)
	}
	return f, nil
}

type nopWriteCloser struct {
	io.Writer
}

func (nopWriteCloser) Close() error { return nil }

// stagePIDHook stamps every log entry with the PID and handoff stage of
// the process that wrote it, so a single continuous log file can be
// untangled back into the three processes that contributed to it.
type stagePIDHook struct {
	pid   int
	stage string
}

func (h stagePIDHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

func (h stagePIDHook) Fire(e *logrus.Entry) error {
	e.Data["pid"] = h.pid
	e.Data["stage"] = h.stage
	return nil
}

// setupLogger opens dest and builds the shared Logrus logger, stamped with
// the current PID and stage. The returned io.Closer should be closed on
// normal shutdown; process exit closes it regardless, and no entry is
// buffered beyond its own Write call, so nothing is lost on the os.Exit
// paths elsewhere in the app.
func setupLogger(dest, stage string) (*logrus.Logger, io.Closer, error) {
	w, err := openLogDestination(dest)
	if err != nil {
		return nil, nil, err
	}

	logger := logrus.New()
	logger.Out = w
	logger.SetFormatter(&logrus.TextFormatter{FullTimestamp: true})
	logger.AddHook(stagePIDHook{pid: os.Getpid(), stage: stage})

	return logger, w, nil
}

// elevatedRelaunchArgs builds the flag set passed to a relaunched child in
// the elevation handoff, carrying the already-resolved log destination
// forward so the child never re-resolves the default.
func elevatedRelaunchArgs(stageFlag, configPath string, configTest bool, logDest string) []string {
	args := []string{stageFlag, "-config", configPath, "-log-output", logDest}
	if configTest {
		args = append(args, "-test")
	}
	return args
}
