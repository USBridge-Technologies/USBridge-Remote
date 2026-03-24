//go:build darwin

package service

type userspaceWireGuardService struct {
	ifaceName    string
	routeTargets []string
}

