//go:build ios

package graphics

func (vk *VirtualKeyboard) RegisterAsIMETarget() {}

func GetLastIMEH() float32 { return 0 }
