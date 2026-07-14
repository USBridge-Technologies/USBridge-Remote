//go:build !windows && !darwin && !linux

package input

import "fmt"

func New() *Controller                                          { return &Controller{} }
func (c *Controller) Key(uint8) error                           { return fmt.Errorf("unsupported platform") }
func (c *Controller) Combo(uint8, uint8) error                  { return fmt.Errorf("unsupported platform") }
func (c *Controller) Text(string) error                         { return fmt.Errorf("unsupported platform") }
func (c *Controller) MouseMove(int8, int8) error                { return fmt.Errorf("unsupported platform") }
func (c *Controller) MouseClick(uint8) error                    { return fmt.Errorf("unsupported platform") }
func (c *Controller) MouseScroll(int8) error                    { return fmt.Errorf("unsupported platform") }
func (c *Controller) MouseAction(uint8, int8, int8, int8) error { return fmt.Errorf("unsupported platform") }
func (c *Controller) AbsoluteEvent(uint8, uint16, uint16, int8) error {
	return fmt.Errorf("unsupported platform")
}
