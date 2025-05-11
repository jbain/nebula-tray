package main

import (
	"flag"
	"fmt"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"github.com/sirupsen/logrus"
	"github.com/slackhq/nebula"
	"github.com/slackhq/nebula/config"
	"os"
	"sync"
)

type nebulaState string

const (
	StateStarted nebulaState = "STARTED"
	StateStopped nebulaState = "STOPPED"
	StateFailed  nebulaState = "FAILED"
)

// service internals
var (
	nebulaTray  fyne.App
	ctrl        *nebula.Control
	state       = StateStopped
	stateReason = "not started"

	togglemtx = sync.Mutex{}
	l         *logrus.Logger
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

	if *configPath == "" {
		fmt.Println("-config flag must be set")
		flag.Usage()
		os.Exit(1)
	}

	l = logrus.New()
	l.Out = os.Stdout

	nebulaTray = app.New()
	nebulaTray.SetIcon(theme.Icon(theme.IconNameComputer))
	initStatusWindow()

	if desk, ok := nebulaTray.(desktop.App); ok {
		updateSystrayMenu()
		desk.SetSystemTrayMenu(systrayMenu)
	}

	showStatusWindow()
	nebulaTray.Run()
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
		l.Errorf("failed to load config: %s", err)
		setState(StateFailed, fmt.Sprintf("failed to load config: %s", err))
		return
	}

	ctrl, err = nebula.Main(c, *configTest, Build, l, nil)
	if err != nil {
		l.Errorf("Failed to start: %s", err)
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
	quit := fyne.NewMenuItem("Quit", func() {
		stopNebula()
		os.Exit(0)
	})
	quit.IsQuit = true
	systrayMenu.Items = []*fyne.MenuItem{
		fyne.NewMenuItem(menuStartStopStr(), func() {
			toggleNebula()
		}),
		fyne.NewMenuItem("status", func() {
			showStatusWindow()
		}),
		fyne.NewMenuItemSeparator(),
		quit,
	}

	systrayMenu.Refresh()
}

func menuStartStopStr() string {
	if state != StateStarted {
		return "Start Nebula"
	} else {
		return "Stop Nebula"
	}
}
