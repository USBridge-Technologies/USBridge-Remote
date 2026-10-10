package gui

import (
	"fmt"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"usbridge-client/internal/api"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/models"
)

// KVM settings pages: Power & performance, Updates, SD card (the KVM's
// /api/settings/*). Each returns its page and a load func, called when the
// page is shown and, for Updates and SD card, on every refresh tick while
// it's shown.

func newWrapLabel() *widget.Label {
	l := widget.NewLabel("")
	l.Wrapping = fyne.TextWrapWord
	return l
}

func kvmPowerPage(client *api.USBClient) (fyne.CanvasObject, func()) {
	rows := container.NewVBox()
	msg := newWrapLabel()
	var render func(p *models.PowerSettings)
	render = func(p *models.PowerSettings) {
		rows.RemoveAll()
		if !p.Available || len(p.Rows) == 0 {
			rows.Add(view.NewDialogHint(i18n.Current.KVMPowerNone, 0))
			return
		}
		for _, r := range p.Rows {
			r := r
			var labels []string
			sel := ""
			for _, s := range r.Steps {
				l := s.Label
				if s.Value == r.Default {
					l += " (" + i18n.Current.KVMPowerStock + ")"
				}
				labels = append(labels, l)
				if s.Value == r.Value {
					sel = l
				}
			}
			var pick *view.HeaderDropdown
			pick = view.NewDialogPicker(labels, sel, func(v string) {
				for i, l := range labels {
					if l != v || r.Steps[i].Value == r.Value {
						continue
					}
					value := r.Steps[i].Value
					msg.SetText("…")
					go func() {
						p, err := client.SetPowerSetting(r.Key, value)
						fyne.Do(func() {
							if err != nil {
								msg.SetText(err.Error())
								return
							}
							msg.SetText(i18n.Current.KVMNetApplied)
							render(p)
						})
					}()
				}
			})
			rows.Add(view.NewDialogField(r.Label, pick))
			rows.Add(view.NewDialogVSpace(4))
		}
	}
	load := func() {
		go func() {
			p, err := client.GetPowerSettings()
			fyne.Do(func() {
				if err != nil {
					msg.SetText(err.Error())
					return
				}
				render(p)
			})
		}()
	}
	return container.NewVBox(rows, msg), load
}

func kvmUpdatesPage(client *api.USBClient) (fyne.CanvasObject, func()) {
	status := newWrapLabel()
	msg := newWrapLabel()
	check := view.NewDialogApplyButton(i18n.Current.KVMUpdateCheck, func() {
		msg.SetText("…")
		go func() {
			err := client.CheckForUpdate()
			fyne.Do(func() {
				if err != nil {
					msg.SetText(err.Error())
				} else {
					msg.SetText("")
				}
			})
		}()
	})
	commit := view.NewDialogCancelButton(i18n.Current.KVMUpdateCommit, func() {
		go func() {
			err := client.CommitUpdate()
			fyne.Do(func() {
				if err != nil {
					msg.SetText(err.Error())
				} else {
					msg.SetText(i18n.Current.KVMNetApplied)
				}
			})
		}()
	})
	load := func() {
		go func() {
			u, err := client.GetUpdateStatus()
			fyne.Do(func() {
				if err != nil {
					status.SetText(err.Error())
					return
				}
				text := i18n.Current.KVMUpdateVersion + ": " + u.Version
				if u.Status != "" {
					text += "\n" + u.Status
					if u.Message != "" {
						text += ": " + u.Message
					}
				}
				status.SetText(text)
			})
		}()
	}
	return container.NewVBox(
		status,
		view.NewDialogVSpace(6),
		container.NewHBox(check, commit),
		msg,
		view.NewDialogVSpace(8),
		view.NewDialogHint(i18n.Current.KVMUpdateHint, 0),
	), load
}

func humanBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

func kvmSDPage(client *api.USBClient, parent fyne.Window) (fyne.CanvasObject, func()) {
	status := newWrapLabel()
	msg := newWrapLabel()
	quiet := view.NewStyledEntry()
	maxInt := view.NewStyledEntry()
	var current models.SDCardStatus
	filled := false
	format := view.NewDialogCancelButton(i18n.Current.KVMSDFormat, func() {
		if !current.FormatAllowed {
			msg.SetText(fmt.Sprintf(i18n.Current.KVMSDFormatBlocked, current.FormatBlockedBy))
			return
		}
		view.ShowConfirmYesLeftDanger(i18n.Current.KVMSDFormat, i18n.Current.KVMSDFormatConfirm, func(ok bool) {
			if !ok {
				return
			}
			go func() {
				err := client.FormatSDCard() // the KVM checks again: never a card with btrfs
				fyne.Do(func() {
					if err != nil {
						msg.SetText(err.Error())
					}
				})
			}()
		}, parent)
	})
	save := view.NewDialogApplyButton(i18n.Current.KVMSave, func() {
		q, err1 := strconv.Atoi(strings.TrimSpace(quiet.Text))
		m, err2 := strconv.Atoi(strings.TrimSpace(maxInt.Text))
		if err1 != nil || err2 != nil {
			msg.SetText("?")
			return
		}
		t := current.Snapshots
		t.QuietPeriodSec, t.MaxSnapshotIntervalSec = q, m*60
		go func() {
			err := client.SetSnapshotTiming(t)
			fyne.Do(func() { msg.SetText(kvmResultText(err)) })
		}()
	})
	formatRow := container.NewHBox(format)
	load := func() {
		go func() {
			s, err := client.GetSDCardStatus()
			fyne.Do(func() {
				if err != nil {
					status.SetText(err.Error())
					return
				}
				current = *s
				var lines []string
				switch {
				case !s.Present:
					lines = append(lines, i18n.Current.KVMSDNone)
				case s.Mounted:
					lines = append(lines, fmt.Sprintf(i18n.Current.KVMSDUsage, humanBytes(s.UsedBytes), humanBytes(s.TotalBytes), s.FileSystem))
				}
				if s.Formatting {
					lines = append(lines, fmt.Sprintf(i18n.Current.KVMSDFormatting, s.FormatStep))
				} else if s.FormatError != "" {
					lines = append(lines, s.FormatError)
				}
				status.SetText(strings.Join(lines, "\n"))
				// Format only offered for a card the KVM would format.
				if s.FormatAllowed && !s.Formatting {
					formatRow.Show()
				} else {
					formatRow.Hide()
				}
				if !filled {
					filled = true
					quiet.SetText(strconv.Itoa(s.Snapshots.QuietPeriodSec))
					maxInt.SetText(strconv.Itoa(s.Snapshots.MaxSnapshotIntervalSec / 60))
				}
			})
		}()
	}
	return container.NewVBox(
		status,
		formatRow,
		view.NewDialogVSpace(8),
		view.NewDialogField(i18n.Current.KVMSnapQuiet, quiet),
		view.NewDialogField(i18n.Current.KVMSnapMaxInterval, maxInt),
		view.NewDialogVSpace(6),
		container.NewHBox(save),
		msg,
	), load
}
