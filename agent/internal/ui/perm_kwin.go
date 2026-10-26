package ui

// kwinPlanesState is kwinplanes.Status, read off the UI goroutine by
// performRefresh.
type kwinPlanesState struct {
	applicable bool
	done       bool
}

// refreshKWinPlanesUI shows the KWin line only on a machine whose compositor
// is KWin (Wayland), which is the one that splits the screen over planes.
func (w *Window) refreshKWinPlanesUI(st kwinPlanesState) {
	if w.kwinPlanesCheck == nil {
		return
	}
	if !st.applicable {
		w.kwinPlanesCheck.Hide()
		return
	}
	w.kwinPlanesCheck.Show()
	w.kwinPlanesCheck.SetChecked(st.done)
}
