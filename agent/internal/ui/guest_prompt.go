package ui

import (
	"fmt"
	"sync"

	"fyne.io/fyne/v2"

	"usbridge_agent/internal/guestauth"
)

// guestPrompted remembers which guests the person has already been asked
// about, so the 2-second refresh doesn't stack a new toast every tick.
var guestPrompted = struct {
	sync.Mutex
	ids map[string]bool
}{ids: map[string]bool{}}

// promptPendingGuests shows a toast (and a system notification, for a window
// that is behind others) for each guest asking to connect. Allow/Deny goes
// back to the engine; unanswered, the request lapses to a denial there.
func (w *Window) promptPendingGuests(pending []guestauth.Session) {
	guestPrompted.Lock()
	live := map[string]bool{}
	var fresh []guestauth.Session
	for _, s := range pending {
		live[s.ShareID] = true
		if !guestPrompted.ids[s.ShareID] {
			guestPrompted.ids[s.ShareID] = true
			fresh = append(fresh, s)
		}
	}
	for id := range guestPrompted.ids {
		if !live[id] {
			delete(guestPrompted.ids, id)
		}
	}
	guestPrompted.Unlock()

	for _, s := range fresh {
		s := s
		msg := fmt.Sprintf(loc().GuestAskConnect, s.Email, s.Role)
		fyne.Do(func() {
			if app := w.app; app != nil {
				app.SendNotification(fyne.NewNotification("USBridge", msg))
			}
			showConfirmToast(msg, func(allow bool) {
				go func() {
					if err := w.token.GuestDecide(s.ShareID, allow); err != nil {
						fmt.Println("guest decision:", err)
					}
				}()
			}, w.guiWin)
		})
	}
}
