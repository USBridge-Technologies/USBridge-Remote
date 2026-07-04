package mobile

import (
	"log"
	"net"

	"github.com/wlynxg/anet"
)

func NetInterface() error {
	interfaces, err := net.Interfaces()
	if err != nil {
		return err
	}

	for _, i := range interfaces {
		log.Println(i)
	}
	return nil
}

func AnetInterface() error {
	interfaces, err := anet.Interfaces()
	if err != nil {
		return err
	}

	for _, i := range interfaces {
		log.Println(i)
	}
	return nil
}

func NetInterfaceAddrs() error {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return err
	}

	for _, addr := range addrs {
		log.Println(addr)
	}
	return nil
}

func AnetInterfaceAddrs() error {
	addrs, err := anet.InterfaceAddrs()
	if err != nil {
		return err
	}

	for _, addr := range addrs {
		log.Println(addr)
	}
	return nil
}
