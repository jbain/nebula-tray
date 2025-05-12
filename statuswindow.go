package main

import (
	"fmt"
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

	nebIp  *canvas.Text
	nebDev *canvas.Text
}

func initStatusWindow() {
	statusW = statusWindow{
		w:            nebulaTray.NewWindow("Nebula-Tray"),
		status:       canvas.NewText("", color.Black),
		statusReason: canvas.NewText("", color.Black),
		connectButton: widget.NewButton("", func() {
			toggleNebula()
		}),
		nebIp:  canvas.NewText("-", color.Black),
		nebDev: canvas.NewText("-", color.Black),
	}

	statusW.topBox = container.New(layout.NewCenterLayout(),
		container.New(layout.NewHBoxLayout(),
			widgetBoldText("Nebula Status:"),
			statusW.status,
			statusW.connectButton,
		),
	)
	reasonBox := container.New(layout.NewCenterLayout(),
		statusW.statusReason,
	)

	mainLayout := container.New(
		layout.NewVBoxLayout(),
		statusW.topBox,
		container.New(layout.NewCenterLayout(),
			container.New(layout.NewHBoxLayout(),
				widgetBoldText("Nebula Version:"),
				widgetBoldText(NebulaVersion),
			),
		),
		reasonBox,
		//main box
		widget.NewSeparator(),
		container.New(layout.NewHBoxLayout(),
			//left box
			container.New(layout.NewVBoxLayout(),
				container.NewHBox(widgetBoldText("NebulaIp"), statusW.nebIp),
				container.NewHBox(widgetBoldText("NebulaDev"), statusW.nebDev),
			),
			//no right box yet
		),
		layout.NewSpacer(),
		container.New(layout.NewCenterLayout(),
			container.New(layout.NewHBoxLayout(),
				widget.NewButton("close", func() { statusW.w.Hide() }),
				widget.NewButton("quit", func() {
					statusW.w.Hide()
					quit()
				}),
			),
		),
	)

	statusW.w.SetContent(mainLayout)
	statusW.w.Resize(fyne.NewSize(300, 200))

	statusW.w.SetCloseIntercept(func() {
		statusW.w.Hide()
	})

	updateStatusWindow()

}

func widgetBoldText(s string) *canvas.Text {
	text := canvas.NewText(s, color.Black)
	text.TextStyle = fyne.TextStyle{
		Bold: true,
	}
	return text
}

func updateStatusWindow() {
	statusW.statusReason.Text = stateReason
	statusW.status.Text = getStateColor()
	switch state {
	case StateStarted:
		statusW.connectButton.SetText("stop")
		statusW.nebIp.Text = ctrl.Device().Cidr().String()
		statusW.nebDev.Text = ctrl.Device().Name()

		tun := ctrl.PrintTunnel(ctrl.Device().Cidr().Addr())
		fmt.Printf("tunneL: %+v\n", tun)
	case StateFailed:
		statusW.connectButton.SetText("retry")
		statusW.nebIp.Text = "-"
		statusW.nebDev.Text = "-"
	case StateStopped:
		statusW.connectButton.SetText("start")
		statusW.nebIp.Text = "-"
		statusW.nebDev.Text = "-"
	}
}

func showStatusWindow() {
	statusW.w.Show()
}

func getStateColor() string {
	c := string(state)
	switch state {
	case StateStopped:
		c = string(state)
	case StateFailed:
		c = string(state)
	}
	return c
}
