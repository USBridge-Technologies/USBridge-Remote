//go:build !android

package service

func initAndroidTailscaleUserspace() {}

func refreshAndroidDefaultRouteInterface() {}

func getAndroidNativeLibraryDir() (string, error) {
	return "", nil
}

func getAndroidFilesDir() (string, error) {
	return "", nil
}

func getAndroidCacheDir() (string, error) {
	return "", nil
}

func openAndroidExternalUrl(url string) (bool, error) {
	return false, nil
}

func requestAndroidVpnPermission() error {
	return nil
}
