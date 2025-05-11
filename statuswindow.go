package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
	"image/color"
)

var statusW statusWindow

type statusWindow struct {
	w             fyne.Window
	status        *canvas.Text
	statusReason  *canvas.Text
	connectButton *widget.Button
	topBox        *fyne.Container
}

func initStatusWindow() {
	statusW = statusWindow{
		w:            nebulaTray.NewWindow("Nebula-Tray"),
		status:       canvas.NewText("", color.Black),
		statusReason: canvas.NewText("", color.Black),
		connectButton: widget.NewButton("", func() {
			toggleNebula()
		}),
	}
	statusW.topBox = container.New(layout.NewCenterLayout(),
		container.New(layout.NewHBoxLayout(),
			statusW.connectButton,
			widget.NewLabel("Nebula Status:"),
			statusW.status,
		),
	)
	reasonBox := container.New(layout.NewHBoxLayout(),
		statusW.statusReason,
	)

	mainLayout := container.New(layout.NewVBoxLayout(), statusW.topBox, reasonBox)
	statusW.w.SetContent(mainLayout)
	statusW.w.Resize(fyne.NewSize(300, 200))

	statusW.w.SetCloseIntercept(func() {
		statusW.w.Hide()
	})

	updateStatusWindow()

}
func updateStatusWindow() {
	statusW.statusReason.Text = stateReason
	statusW.status.Text = getStateColor()
	switch state {
	case StateStarted:
		statusW.connectButton.SetText("stop")
	case StateFailed:
		statusW.connectButton.SetText("retry")
	case StateStopped:
		statusW.connectButton.SetText("start")
	}

}

func showStatusWindow() {
	statusW.w.Show()
}

func getStateColor() string {
	color := string(state)
	switch state {
	case StateStopped:
		color = string(state)
	case StateFailed:
		color = string(state)
	}
	return color
}
