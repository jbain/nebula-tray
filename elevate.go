package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
)

// nebula needs root to create the TUN device, so this file replaces the old
// requirement of starting the app with `sudo` from a terminal. Getting
// there is a three-process handoff:
//
//  1. The unprivileged process the user actually launches. Not root, shows
//     a confirmation modal, then calls relaunchViaOsascript.
//  2. The process osascript launches directly, as root, inside its
//     "do shell script ... with administrator privileges". Its only job is
//     to call relaunchDirect and exit immediately (see -elevate-via-osascript
//     in main.go) - otherwise osascript would stay resident (burning CPU)
//     for as long as the real app keeps running, since "do shell script"
//     doesn't return until the command it ran exits.
//  3. The final, long-running elevated instance, launched directly by (2)
//     with no relationship to osascript at all.
//
// The -elevate-via-osascript and -elevate-attempted flags carry state
// between these processes. They're passed as explicit command-line flags
// rather than environment variables because "do shell script" does not
// reliably propagate the calling process's environment.

// ensureElevated checks whether the process is running as root. If it
// isn't, it shows a modal explaining that administrator privileges are
// needed, then relaunches itself via macOS's native authentication prompt.
func ensureElevated(a fyne.App) {
	if os.Geteuid() == 0 {
		return
	}

	if *elevationAttempted {
		fmt.Println("already attempted elevation once and still not running as root; giving up")
		os.Exit(1)
	}

	w := a.NewWindow("Nebula Tray")
	w.Resize(fyne.NewSize(380, 240))
	w.SetFixedSize(true)
	w.CenterOnScreen()
	// Closing the window (e.g. the titlebar close box) is treated the same
	// as pressing Quit below.
	w.SetCloseIntercept(func() {
		os.Exit(0)
	})

	confirm := dialog.NewConfirm(
		"Administrator Privileges Required",
		"Nebula Tray needs administrator privileges to create the VPN tunnel.\nYou'll be asked to authenticate.",
		func(ok bool) {
			if !ok {
				os.Exit(0)
			}
			relaunchViaOsascript()
		},
		w,
	)
	confirm.SetConfirmText("OK")
	confirm.SetDismissText("Quit")

	w.Show()
	confirm.Show()
	a.Run()
}

// relaunchViaOsascript starts the current binary as root via osascript's
// native "administrator privileges" authentication prompt, releases it,
// and exits this (unprivileged) process. It never returns.
func relaunchViaOsascript() {
	self, err := os.Executable()
	if err != nil {
		fmt.Printf("failed to determine executable path for elevation: %s\n", err)
		os.Exit(1)
	}

	args := []string{"-elevate-via-osascript", "-config", *configPath}
	if *configTest {
		args = append(args, "-test")
	}

	parts := make([]string, 0, len(args)+1)
	parts = append(parts, quoteForShell(self))
	for _, a := range args {
		parts = append(parts, quoteForShell(a))
	}
	shellCmd := strings.Join(parts, " ")

	script := fmt.Sprintf("do shell script %s with administrator privileges", quoteForAppleScript(shellCmd))

	cmd := exec.Command("osascript", "-e", script)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		fmt.Printf("failed to relaunch with elevated privileges: %s\n", err)
		os.Exit(1)
	}

	// Detach rather than Wait: the auth prompt and everything after it
	// happen independently of this process, which has nothing left to do
	// but exit. Per the os.Process docs, Release is how you hand a started
	// child off without leaking Go-side process state.
	if err := cmd.Process.Release(); err != nil {
		fmt.Printf("failed to release elevated process: %s\n", err)
	}

	os.Exit(0)
}

// relaunchDirect starts a plain, detached copy of the current binary - no
// osascript involved - carrying forward the real flags but dropping
// -elevate-via-osascript so the new process runs normally instead of
// handing off again. It releases the child and exits immediately.
func relaunchDirect() {
	self, err := os.Executable()
	if err != nil {
		fmt.Printf("failed to determine executable path for direct relaunch: %s\n", err)
		os.Exit(1)
	}

	args := []string{"-elevate-attempted", "-config", *configPath}
	if *configTest {
		args = append(args, "-test")
	}

	cmd := exec.Command(self, args...)
	// Do not inherit stdout/stderr from the process launched by
	// "do shell script". osascript captures those descriptors and waits for
	// EOF; if the long-running app inherits them, osascript stays alive even
	// after this short-lived handoff process exits. Leaving them nil makes
	// os/exec connect the child to the null device.

	if err := cmd.Start(); err != nil {
		fmt.Printf("failed to relaunch directly: %s\n", err)
		os.Exit(1)
	}
	if err := cmd.Process.Release(); err != nil {
		fmt.Printf("failed to release relaunched process: %s\n", err)
	}

	os.Exit(0)
}

// quoteForShell wraps s in single quotes for safe use as one argument in a
// POSIX shell command line.
func quoteForShell(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// quoteForAppleScript renders s as a double-quoted AppleScript string
// literal.
func quoteForAppleScript(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}
