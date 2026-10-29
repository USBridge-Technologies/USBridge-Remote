package gui

import (
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/netutil"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

// refreshUDPBufferUI shows the header grant button while the kernel's UDP receive-buffer cap
// is too small for whole video frames (Linux only; see netutil/udpbuf_linux.go) -- the
// AWDL grant button's counterpart, refreshed from the same status tick.
func (mw *MainWindow) refreshUDPBufferUI() {
	if mw.udpBufGrantBtn == nil {
		return
	}
	if netutil.UDPBufferOK() || !netutil.UDPBufferAdjustable() {
		mw.udpBufGrantBtn.Hide()
	} else {
		mw.udpBufGrantBtn.Show()
	}
}

// handleUDPBufferGrantTapped raises the cap (one pkexec password prompt). The new size applies
// to sockets opened afterwards, so the next connection gets it.
func (mw *MainWindow) handleUDPBufferGrantTapped() {
	if mw.udpBufGrantBtn == nil {
		return
	}
	mw.udpBufGrantBtn.Disable()
	go func() {
		err := netutil.EnsureUDPBuffer()
		if err == nil {
			logrus.Infof("[udpbuf] net.core.rmem_max raised to %d", netutil.UDPBufferMax())
		}
		fyne.Do(func() {
			mw.udpBufGrantBtn.Enable()
			if err != nil {
				view.ShowErrorDialog(err, mw.window)
				return
			}
			mw.refreshUDPBufferUI()
		})
	}()
}
