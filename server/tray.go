package main

import (
	"fmt"
	"log"
	"os/exec"
	"runtime"
	"sync/atomic"

	"github.com/getlantern/systray"
)

// dashboardPort is the live web UI port (updated when settings change).
var dashboardPort atomic.Int32

func startSystemTray() {
	systray.Run(func() {
		systray.SetTitle("WT")
		systray.SetTooltip("WalkieTalkie Base Station")
		mOpen := systray.AddMenuItem("Open Dashboard", "Open the Base Station web UI")
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("Quit", "Stop the Base Station")
		go func() {
			for {
				select {
				case <-mOpen.ClickedCh:
					port := int(dashboardPort.Load())
					if port <= 0 {
						port = 9091
					}
					url := fmt.Sprintf("http://127.0.0.1:%d", port)
					if err := openBrowser(url); err != nil {
						log.Printf("tray: open dashboard: %v", err)
					}
				case <-mQuit.ClickedCh:
					systray.Quit()
				}
			}
		}()
	}, func() {
		log.Printf("tray: quitting")
		// Fatal-exit so HTTP serve and goroutines stop; no graceful HTTP
		// shutdown from tray path (acceptable for desktop Quit).
		log.Fatal("quit from system tray")
	})
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
