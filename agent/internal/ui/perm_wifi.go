package ui

// wifiTuneState is netutil.WiFiTuneStatus, read off the UI goroutine by
// performRefresh (on Linux it runs iw).
type wifiTuneState struct {
	applicable bool
	done       bool
}

// refreshWiFiTuneUI shows the Wi-Fi permission line only while the host
// has Wi-Fi with something to tune.
func (w *Window) refreshWiFiTuneUI(st wifiTuneState) {
	if w.wifiTuneCheck == nil {
		return
	}
	if !st.applicable {
		w.wifiTuneCheck.Hide()
		return
	}
	w.wifiTuneCheck.Show()
	w.wifiTuneCheck.SetChecked(st.done)
}
