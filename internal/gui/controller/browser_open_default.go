//go:build !linux && !freebsd && !openbsd && !netbsd

package controller

import (
	"net/url"

	"fyne.io/fyne/v2"
)

func openExternalURL(app fyne.App, uri *url.URL) error {
	return app.OpenURL(uri)
}
