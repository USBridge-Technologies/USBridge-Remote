//go:build !android
// +build !android

package platform

// GetIntentDataURI получает URI из Intent (только для Android, на desktop возвращает пустую строку)
func GetIntentDataURI() (string, error) {
	return "", nil
}
