//go:build !windows

package input

import "fmt"

type Controller struct{}

func New() *Controller                                          { return &Controller{} }
func (c *Controller) Key(uint8) error                           { return fmt.Errorf("windows only") }
func (c *Controller) Combo(uint8, uint8) error                  { return fmt.Errorf("windows only") }
func (c *Controller) Text(string) error                         { return fmt.Errorf("windows only") }
func (c *Controller) MouseMove(int8, int8) error                { return fmt.Errorf("windows only") }
func (c *Controller) MouseClick(uint8) error                    { return fmt.Errorf("windows only") }
func (c *Controller) MouseScroll(int8) error                    { return fmt.Errorf("windows only") }
func (c *Controller) MouseAction(uint8, int8, int8, int8) error { return fmt.Errorf("windows only") }
func (c *Controller) AbsoluteEvent(uint8, uint16, uint16, int8) error {
	return fmt.Errorf("windows only")
}
