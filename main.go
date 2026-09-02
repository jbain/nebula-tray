package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"

	"github.com/slackhq/nebula"
	"github.com/slackhq/nebula/config"
)

type nebulaState string

const (
	StateStarted nebulaState = "STARTED"
	StateStopped nebulaState = "STOPPED"
	StateFailed  nebulaState = "FAILED"
)

// ldflags
var (
	NebulaVersion = "unknown"
)

// service internals
var (
	nebulaTray  fyne.App
	ctrl        *nebula.Control
	state       = StateStopped
	stateReason = "not started"

	togglemtx = sync.Mutex{}
	l         *slog.Logger

	// logDestination is the resolved, absolute -log-output value (or
	// "stdout"). It's set once in main and carried through the elevation
	// relaunch hops - see elevatedRelaunchArgs in logging.go.
	logDestination string
)

// interface elements
var (
	systrayMenu = fyne.NewMenu("systray")

	stateColor = binding.NewString()
)

// flags
var (
	Build        = "lol"
	configPath   = flag.String("config", "/Users/jbain/nebula/config.yml", "Path to either a file or directory to load configuration from")
	configTest   = flag.Bool("test", false, "Test the config and print the end result. Non zero exit indicates a faulty config")
	printVersion = flag.Bool("version", false, "Print version")
	printUsage   = flag.Bool("help", false, "Print command line usage")
	logOutput    = flag.String("log-output", "", "Where to write logs: a file path, or the reserved value \"stdout\" for direct/already-elevated runs. Defaults to ~/Library/Logs/Nebula Tray/nebula-tray.log")

	// internal, set by nebula-tray itself when relaunching - not meant to
	// be passed by users. See elevate.go.
	viaOsascript       = flag.Bool("elevate-via-osascript", false, "internal: set on the process osascript launches directly")
	elevationAttempted = flag.Bool("elevate-attempted", false, "internal: set once an elevation relaunch has been attempted, to avoid retry loops")
)

func main() {

	flag.Parse()

	if *printVersion {
		fmt.Printf("Version: %s\n", Build)
		os.Exit(0)
	}

	if *printUsage {
		flag.Usage()
		os.Exit(0)
	}

	// Logging is initialized before config discovery, GUI init, or
	// elevation so startup and handoff failures are captured, not just
	// whatever comes after it succeeds.
	dest, err := resolveLogDestination(*logOutput)
	if err != nil {
		fatalStartup(err)
	}
	if dest == stdoutDestination && os.Geteuid() != 0 {
		// stdout only makes sense for a process that's already privileged;
		// a process about to elevate can't carry a real stdout descriptor
		// across the osascript handoff (see elevate.go), and reattaching
		// osascript's captured pipe there would keep it resident.
		fatalStartup(fmt.Errorf("-log-output stdout requires the process to already be running with administrator privileges; pass a log file path so elevation can hand it off across processes"))
	}
	logDestination = dest

	stage := "initial"
	switch {
	case *viaOsascript:
		stage = "osascript-handoff"
	case *elevationAttempted:
		stage = "elevated-app"
	}

	logger, logCloser, err := setupLogger(dest, stage)
	if err != nil {
		fatalStartup(fmt.Errorf("opening log output %q: %w", dest, err))
	}
	l = logger
	defer logCloser.Close()

	l.Info("nebula-tray starting")

	if *viaOsascript {
		// This process is the one osascript launched directly as root. Hand
		// off to a plain, detached relaunch of itself and exit immediately,
		// so the "do shell script" osascript is running completes right
		// away instead of staying resident for the app's whole lifetime.
		l.Info("received osascript handoff, relaunching detached")
		relaunchDirect()
	}

	if *configPath == "" {
		l.Error("-config flag must be set")
		fmt.Println("-config flag must be set")
		flag.Usage()
		os.Exit(1)
	}

	nebulaTray = app.New()
	nebulaTray.SetIcon(theme.Icon(theme.IconNameComputer))

	ensureElevated(nebulaTray)

	initStatusWindow()

	if desk, ok := nebulaTray.(desktop.App); ok {
		updateSystrayMenu()
		desk.SetSystemTrayMenu(systrayMenu)
	}

	showStatusWindow()
	nebulaTray.Run()
}

// fatalStartup reports a startup failure that happened before (or while)
// initializing logging and exits non-zero. There's no shared logger to
// fall back to here - and no silent fallback allowed either - so the exact
// destination/error always goes to stderr, plus a visible dialog for a GUI
// launch. The osascript-handoff process never has a GUI to show, and
// whatever it writes to stderr is relayed by osascript's captured pipe.
func fatalStartup(err error) {
	fmt.Fprintf(os.Stderr, "nebula-tray: %s\n", err)

	if !*viaOsascript {
		a := app.New()
		w := a.NewWindow("Nebula Tray - Startup Error")
		w.Resize(fyne.NewSize(420, 160))
		w.CenterOnScreen()

		d := dialog.NewError(err, w)
		d.SetOnClosed(func() { a.Quit() })
		w.SetCloseIntercept(func() { a.Quit() })

		w.Show()
		d.Show()
		a.Run()
	}

	os.Exit(1)
}

func toggleNebula() {
	if state != StateStarted {
		startNebula()
	} else {
		stopNebula()
	}
}

func startNebula() {
	togglemtx.Lock()
	defer togglemtx.Unlock()
	if state == StateStarted {
		l.Info("nebula already started")
		return
	}
	l.Info("starting nebula")
	c := config.NewC(l)
	err := c.Load(*configPath)
	if err != nil {

		l.Error("failed to load config", slog.String("error", err.Error()))
		setState(StateFailed, fmt.Sprintf("failed to load config: %s", err))
		return
	}

	ctrl, err = nebula.Main(c, *configTest, Build, l, nil)
	if err != nil {
		l.Error("Failed to start", slog.String("error", err.Error()))
		setState(StateFailed, fmt.Sprintf("failed to start: %s", err))
		return
	}
	ctrl.Start()

	setState(StateStarted, "started successfully")
	l.Info("nebula started")
}

func stopNebula() {
	togglemtx.Lock()
	defer togglemtx.Unlock()
	l.Info("stopping nebula")
	if state == StateStarted {
		ctrl.Stop()
	}
	ctrl = nil
	setState(StateStopped, "stopped")
	l.Info("nebula stopped")
}

func setState(s nebulaState, reason string) {
	state = s
	stateReason = reason
	updateSystrayMenu()
	updateStatusWindow()

}

func updateSystrayMenu() {
	q := fyne.NewMenuItem("Quit", func() {
		quit()
	})
	q.IsQuit = true
	systrayMenu.Items = []*fyne.MenuItem{
		fyne.NewMenuItem(menuStartStopStr(), func() {
			toggleNebula()
		}),
		fyne.NewMenuItem("status", func() {
			showStatusWindow()
		}),
		fyne.NewMenuItemSeparator(),
		q,
	}

	systrayMenu.Refresh()
}

func quit() {
	l.Info("shutting down")
	stopNebula()
	os.Exit(0)
}

func menuStartStopStr() string {
	if state != StateStarted {
		return "Start Nebula"
	} else {
		return "Stop Nebula"
	}
}
