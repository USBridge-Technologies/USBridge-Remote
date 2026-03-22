package gui

var appVersion = "1.0.0"

func SetAppVersion(version string) {
	if version == "" {
		return
	}
	appVersion = version
}
