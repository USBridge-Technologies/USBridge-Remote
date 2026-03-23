package view

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

type FullscreenUI struct {
	VideoImage        *canvas.Image
	KeyboardButton    *widget.Button
	KeyboardLayout    *fyne.Container
	VideoWithKeyboard *fyne.Container
	MainContainer     *fyne.Container
}

func NewFullscreenUI(videoImage *canvas.Image, touchpad fyne.CanvasObject, keyboardLayout *fyne.Container, onToggleKeyboard func()) *FullscreenUI {
	videoContainer := container.NewMax(touchpad)
	videoWithKeyboard := container.NewBorder(nil, keyboardLayout, nil, nil, videoContainer)
	keyboardButton := widget.NewButton("⌨️", onToggleKeyboard)
	keyboardButton.Importance = widget.HighImportance
	mainContainer := container.NewStack(videoWithKeyboard, container.NewWithoutLayout(keyboardButton))

	return &FullscreenUI{
		VideoImage:        videoImage,
		KeyboardButton:    keyboardButton,
		KeyboardLayout:    keyboardLayout,
		VideoWithKeyboard: videoWithKeyboard,
		MainContainer:     mainContainer,
	}
}

type DiskRowWidgets struct {
	Checkbox       *widget.Check
	NameLabel      *widget.Label
	StatusLabel    *widget.Label
	RORWButton     *widget.Button
	ModeSelect     *widget.Select
	UploadButton   *widget.Button
	DeleteButton   *widget.Button
	SettingsButton *widget.Button
	ModeIcon       *canvas.Text
	ModeTitleLabel *widget.Label
}

func ResolveDiskRowWidgets(obj fyne.CanvasObject) *DiskRowWidgets {
	borderContainer, ok := obj.(*fyne.Container)
	if !ok {
		return nil
	}

	var row DiskRowWidgets
	var rightContainer *fyne.Container
	var checkboxContainer *fyne.Container
	var centerContainer *fyne.Container

	for _, child := range borderContainer.Objects {
		if v, ok := child.(*fyne.Container); ok {
			if len(v.Objects) == 2 {
				var hasLabel bool
				var hasModeWrap bool
				for _, o := range v.Objects {
					if _, ok := o.(*widget.Label); ok {
						hasLabel = true
					}
					if c, ok := o.(*fyne.Container); ok {
						for _, sub := range c.Objects {
							if _, ok := sub.(*canvas.Text); ok {
								hasModeWrap = true
								break
							}
						}
					}
				}
				if hasLabel && hasModeWrap {
					centerContainer = v
					continue
				}
			}
			if len(v.Objects) > 0 {
				if _, ok := v.Objects[0].(*widget.Check); ok {
					checkboxContainer = v
				} else {
					rightContainer = v
				}
			}
		}
	}

	if centerContainer != nil {
		for _, o := range centerContainer.Objects {
			if l, ok := o.(*widget.Label); ok {
				row.NameLabel = l
			}
			if c, ok := o.(*fyne.Container); ok {
				for _, sub := range c.Objects {
					if txt, ok := sub.(*canvas.Text); ok && row.ModeIcon == nil {
						row.ModeIcon = txt
					}
					if l, ok := sub.(*widget.Label); ok && row.ModeTitleLabel == nil {
						row.ModeTitleLabel = l
					}
				}
			}
		}
	}

	if checkboxContainer != nil {
		for _, child := range checkboxContainer.Objects {
			if c, ok := child.(*widget.Check); ok {
				row.Checkbox = c
				break
			}
		}
	}

	if rightContainer != nil {
		buttonIndex := 0
		for _, child := range rightContainer.Objects {
			switch v := child.(type) {
			case *widget.Button:
				if buttonIndex == 0 {
					row.RORWButton = v
				} else if buttonIndex == 1 {
					row.UploadButton = v
				} else if buttonIndex == 2 {
					row.DeleteButton = v
				} else if buttonIndex == 3 {
					row.SettingsButton = v
				}
				buttonIndex++
			case *widget.Select:
				row.ModeSelect = v
			case *widget.Label:
				row.StatusLabel = v
			}
		}
	}

	return &row
}
