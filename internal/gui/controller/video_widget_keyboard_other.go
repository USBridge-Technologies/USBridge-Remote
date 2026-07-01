//go:build !windows && !android && !ios

package controller

import "usbridge-client/internal/gui/graphics"

func platformSetupKeyboardWindow(vk *graphics.VirtualKeyboard) {}
