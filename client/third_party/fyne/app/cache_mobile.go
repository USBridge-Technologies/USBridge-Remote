//go:build android || ios || mobile

package app

import "fyne.io/fyne/v2"

func rootCacheDir(a fyne.App) string {
	return a.(*fyneApp).storageRoot()
}
