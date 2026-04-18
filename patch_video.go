package main

import (
	"fmt"
	"net"
)

func main() {
	addr, _ := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	l, _ := net.ListenUDP("udp", addr)
	port := l.LocalAddr().(*net.UDPAddr).Port
	l.Close()
	fmt.Println("Free port:", port)
}
