package mobile

import (
	"log"
	"net"

	"github.com/wlynxg/anet"
)

func NetInterface() {
	interfaces, err := net.Interfaces()
	if err != nil {
		panic(err)
	}

	for _, i := range interfaces {
		log.Println(i)
	}
}

func AnetInterface() {
	interfaces, err := anet.Interfaces()
	if err != nil {
		panic(err)
	}

	for _, i := range interfaces {
		log.Println(i)
	}
}

func NetInterfaceAddrs() {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		panic(err)
	}

	for _, addr := range addrs {
		log.Println(addr)
	}
}

func AnetInterfaceAddrs() {
	addrs, err := anet.InterfaceAddrs()
	if err != nil {
		panic(err)
	}

	for _, addr := range addrs {
		log.Println(addr)
	}
}
