package ui

import (
	"fmt"
	"log"
	"path/filepath"
	"strconv"
	"strings"

	"usbridge_agent/internal/streamhost"
	"usbridge_agent/internal/ui/design"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// hostAppsDialogWidth fits a command line in the editor.
const hostAppsDialogWidth float32 = 460

// showHostAppsDialog lists the host's apps (streamhost/apps.go) -- what a
// Moonlight client can start after connecting -- with add, edit and remove.
// Every change saves the whole list.
func (w *Window) showHostAppsDialog(parent fyne.Window) {
	if w.token == nil || parent == nil {
		return
	}
	listBox := container.New(&tightVBoxLayout{gap: 4})
	status := dialogNoteText("")
	status.Hide()
	errLabel := dialogErrorText()
	var popup *widget.PopUp
	closeDialog := func() {
		if popup != nil {
			popup.Hide()
		}
	}

	var apps []streamhost.HostApp
	var render func()
	save := func(next []streamhost.HostApp) {
		go func() {
			pending, err := w.token.SetHostApps(next)
			fyne.Do(func() {
				if err != nil {
					setDialogError(errLabel, err.Error())
					return
				}
				setDialogError(errLabel, "")
				apps = next
				if pending {
					status.Text = loc().HostAppsPending
					status.Show()
				} else {
					status.Hide()
				}
				status.Refresh()
				render()
			})
		}()
	}
	render = func() {
		listBox.RemoveAll()
		for i := range apps {
			i := i
			name := widget.NewLabel(apps[i].Name)
			name.Truncation = fyne.TextTruncateEllipsis
			detail := strings.TrimSpace(apps[i].Cmd)
			if detail == "" && len(apps[i].Detached) > 0 {
				detail = apps[i].Detached[0]
			}
			editBtn := newIconActionButton("", theme.NewColoredResource(theme.DocumentCreateIcon(), design.ColorNameMutedOlive), func() {
				w.showHostAppEditor(parent, apps[i], func(edited streamhost.HostApp) {
					next := append([]streamhost.HostApp(nil), apps...)
					next[i] = edited
					save(next)
				})
			})
			editBtn.Quiet = true
			upBtn := newIconActionButton("", theme.NewColoredResource(theme.MoveUpIcon(), design.ColorNameMutedOlive), func() {
				if i == 0 {
					return
				}
				next := append([]streamhost.HostApp(nil), apps...)
				next[i-1], next[i] = next[i], next[i-1]
				save(next)
			})
			upBtn.Quiet = true
			if i == 0 {
				upBtn.Disable()
			}
			removeBtn := newDangerGlyphButton(func() {
				showConfirmToast(fmt.Sprintf(loc().HostAppDelete, apps[i].Name), func(yes bool) {
					if !yes {
						return
					}
					next := append(append([]streamhost.HostApp(nil), apps[:i]...), apps[i+1:]...)
					save(next)
				}, parent)
			})
			label := container.New(&tightVBoxLayout{gap: 0}, wrapDialogLabel(name, 12, design.ColorTextLight))
			if detail != "" {
				d := dialogNoteText(detail)
				label.Add(d)
			}
			actions := container.New(&tightHBoxLayout{gap: 2}, upBtn, editBtn, removeBtn)
			listBox.Add(container.NewBorder(nil, nil, nil, actions, label))
		}
		listBox.Refresh()
	}

	addBtn := newDialogCTA(loc().HostAppAdd, func() {
		w.showHostAppEditor(parent, streamhost.NewHostApp(""), func(added streamhost.HostApp) {
			save(append(append([]streamhost.HostApp(nil), apps...), added))
		})
	})
	hint := widget.NewLabel(loc().HostAppsHint)
	hint.Wrapping = fyne.TextWrapWord
	scroll := container.NewVScroll(listBox)
	scroll.SetMinSize(fyne.NewSize(0, 220))
	body := container.New(&tightVBoxLayout{gap: 8}, wrapPINHint(hint), scroll, status, errLabel)
	panel := newBrandedDialogPanelInsets(loc().HostApps, hostAppsDialogWidth, 20, 10, body, container.NewCenter(addBtn), closeDialog)
	popup = showOverlayPopup(parent, overlayPopupSpec{Panel: panel})

	go func() {
		loaded, err := w.token.HostApps()
		fyne.Do(func() {
			if err != nil {
				log.Printf("[ui] host apps: %v", err)
				setDialogError(errLabel, err.Error())
				return
			}
			apps = loaded
			render()
		})
	}()
}

// showHostAppEditor edits one app with Sunshine's fields; onSave gets the
// result.
func (w *Window) showHostAppEditor(parent fyne.Window, app streamhost.HostApp, onSave func(streamhost.HostApp)) {
	title := loc().HostAppEdit
	if app.Name == "" {
		title = loc().HostAppAdd
	}
	entry := func(text, placeholder string) *widget.Entry {
		e := widget.NewEntry()
		e.SetText(text)
		e.SetPlaceHolder(placeholder)
		return e
	}
	name := entry(app.Name, "Steam Big Picture")
	cmd := entry(app.Cmd, `C:\Games\game.exe`)
	workDir := entry(app.WorkingDir, "")
	image := entry(app.ImagePath, "steam.png")
	timeout := entry(strconv.Itoa(app.ExitTimeout), "5")
	detached := widget.NewMultiLineEntry()
	detached.SetText(strings.Join(app.Detached, "\n"))
	detached.SetPlaceHolder(loc().HostAppDetachedHint)
	detached.Wrapping = fyne.TextWrapOff
	detached.SetMinRowsVisible(2)

	elevated := newStyledCheck(loc().HostAppElevated, app.Elevated, nil)
	autoDetach := newStyledCheck(loc().HostAppAutoDetach, app.AutoDetach, nil)
	waitAll := newStyledCheck(loc().HostAppWaitAll, app.WaitAll, nil)
	noGlobalPrep := newStyledCheck(loc().HostAppNoGlobalPrep, app.ExcludeGlobalPrepCmd, nil)

	// Prep steps: a Do/Undo pair per row.
	type prepRow struct {
		do, undo *widget.Entry
		elevated *styledCheck
	}
	var rows []*prepRow
	prepBox := container.New(&tightVBoxLayout{gap: 4})
	var renderPrep func()
	renderPrep = func() {
		prepBox.RemoveAll()
		for i, r := range rows {
			i := i
			remove := newDangerGlyphButton(func() {
				rows = append(rows[:i], rows[i+1:]...)
				renderPrep()
			})
			fields := container.NewGridWithColumns(2, wrapDialogFieldCompact(r.do), wrapDialogFieldCompact(r.undo))
			prepBox.Add(container.NewBorder(nil, nil, nil, container.New(&tightHBoxLayout{gap: 4}, r.elevated, remove), fields))
		}
		prepBox.Refresh()
	}
	addPrepRow := func(p streamhost.HostAppPrepCmd) {
		rows = append(rows, &prepRow{
			do:       entry(p.Do, loc().HostAppPrepDo),
			undo:     entry(p.Undo, loc().HostAppPrepUndo),
			elevated: newStyledCheck(loc().HostAppPrepAdmin, p.Elevated, nil),
		})
	}
	for _, p := range app.PrepCmd {
		addPrepRow(p)
	}
	renderPrep()
	addPrep := newIconActionButton(loc().HostAppPrepAdd, theme.NewColoredResource(theme.ContentAddIcon(), design.ColorNameMutedOlive), func() {
		addPrepRow(streamhost.HostAppPrepCmd{})
		renderPrep()
	})
	addPrep.Quiet = true

	var popup *widget.PopUp
	closeDialog := func() {
		if popup != nil {
			popup.Hide()
		}
	}
	browse := func(target *widget.Entry, folder bool) fyne.CanvasObject {
		// quoted is a file path as the command field wants it.
		quoted := func(path string) string {
			if strings.ContainsAny(path, " 	") {
				return `"` + path + `"`
			}
			return path
		}
		// fyneBrowse is the fallback for a system without a native dialog.
		fyneBrowse := func() {
			if folder {
				dialog.NewFolderOpen(func(u fyne.ListableURI, err error) {
					if err == nil && u != nil {
						target.SetText(u.Path())
					}
				}, parent).Show()
				return
			}
			dialog.NewFileOpen(func(r fyne.URIReadCloser, err error) {
				if err == nil && r != nil {
					path := r.URI().Path()
					_ = r.Close()
					target.SetText(quoted(path))
					if workDir.Text == "" {
						if dir, err := storage.Parent(r.URI()); err == nil {
							workDir.SetText(dir.Path())
						}
					}
				}
			}, parent).Show()
		}
		b := newIconActionButton("", theme.NewColoredResource(theme.FolderOpenIcon(), design.ColorNameMutedOlive), func() {
			// The system's own dialog, like the client's disk picker; Fyne's only when
			// that can't be opened.
			go func() {
				path, shown := pickNativePath(folder, loc().HostAppCmd)
				fyne.Do(func() {
					switch {
					case !shown:
						fyneBrowse()
					case path == "":
					case folder:
						target.SetText(path)
					default:
						target.SetText(quoted(path))
						if workDir.Text == "" {
							workDir.SetText(filepath.Dir(path))
						}
					}
				})
			}()
		})
		b.Quiet = true
		return b
	}

	errLabel := dialogErrorText()
	saveBtn := newDialogCTA(loc().Save, func() {
		next := app
		next.Name = strings.TrimSpace(name.Text)
		if next.Name == "" {
			setDialogError(errLabel, loc().HostAppNeedsName)
			return
		}
		next.Cmd = strings.TrimSpace(cmd.Text)
		next.WorkingDir = strings.TrimSpace(workDir.Text)
		next.ImagePath = strings.TrimSpace(image.Text)
		next.Detached = nil
		for _, line := range strings.Split(detached.Text, "\n") {
			if line = strings.TrimSpace(line); line != "" {
				next.Detached = append(next.Detached, line)
			}
		}
		next.PrepCmd = nil
		for _, r := range rows {
			p := streamhost.HostAppPrepCmd{Do: strings.TrimSpace(r.do.Text), Undo: strings.TrimSpace(r.undo.Text), Elevated: r.elevated.Checked}
			if p.Do != "" || p.Undo != "" {
				next.PrepCmd = append(next.PrepCmd, p)
			}
		}
		next.Elevated = elevated.Checked
		next.AutoDetach = autoDetach.Checked
		next.WaitAll = waitAll.Checked
		next.ExcludeGlobalPrepCmd = noGlobalPrep.Checked
		if n, err := strconv.Atoi(strings.TrimSpace(timeout.Text)); err == nil && n >= 0 {
			next.ExitTimeout = n
		}
		closeDialog()
		onSave(next)
	})

	labels := []string{loc().HostAppName, loc().HostAppCmd, loc().HostAppWorkDir, loc().HostAppDetached, loc().HostAppImage, loc().HostAppExitTimeout}
	labelW := dialogFormLabelWidth(labels...)
	withBrowse := func(e *widget.Entry, folder bool) fyne.CanvasObject {
		return container.NewBorder(nil, nil, nil, browse(e, folder), wrapDialogFieldCompact(e))
	}
	prepCaption := dialogFieldCaption(loc().HostAppPrep)
	form := container.New(&tightVBoxLayout{gap: 8},
		newDialogFormRow(loc().HostAppName, labelW, wrapDialogFieldCompact(name)),
		newDialogFormRow(loc().HostAppCmd, labelW, withBrowse(cmd, false)),
		newDialogFormRow(loc().HostAppWorkDir, labelW, withBrowse(workDir, true)),
		newDialogFormRow(loc().HostAppDetached, labelW, wrapDialogFieldCompact(detached)),
		newDialogFormRow(loc().HostAppImage, labelW, wrapDialogFieldCompact(image)),
		container.NewBorder(nil, nil, prepCaption, addPrep),
		prepBox,
		elevated,
		autoDetach,
		waitAll,
		noGlobalPrep,
		newDialogFormRow(loc().HostAppExitTimeout, labelW, wrapDialogFieldCompact(timeout)),
		errLabel,
	)
	scroll := container.NewVScroll(form)
	scroll.SetMinSize(fyne.NewSize(0, 380))
	panel := newBrandedDialogPanelInsets(title, hostAppsDialogWidth, 20, 10, scroll, container.NewCenter(saveBtn), closeDialog)
	popup = showOverlayPopup(parent, overlayPopupSpec{Panel: panel})
}
