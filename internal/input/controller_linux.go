//go:build linux

package input

func New() *Controller {
	return &Controller{}
}

func (c *Controller) Key(keycode uint8) error {
	return nil
}

func (c *Controller) Combo(mod, key uint8) error {
	return nil
}

func (c *Controller) Text(text string) error {
	return nil
}

func (c *Controller) MouseMove(dx, dy int8) error {
	return nil
}

func (c *Controller) MouseClick(button uint8) error {
	return nil
}

func (c *Controller) MouseScroll(dy int8) error {
	return nil
}

func (c *Controller) MouseAction(button uint8, x, y, wheel int8) error {
	return nil
}

func (c *Controller) AbsoluteEvent(button uint8, x, y uint16, wheel int8) error {
	return nil
}
