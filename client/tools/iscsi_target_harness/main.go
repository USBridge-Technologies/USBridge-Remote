// Integration-test harness: runs the REAL production IscsiTargetRunner
// (client/internal/service/iscsi_target.go) — not a throwaway spike — so
// the agent's real iscsi initiator code can be validated end-to-end
// against it. Run with:
//
//	go run ./tools/iscsi_target_harness <path-to-test-image> <iqn> <bind-host:port>
package main

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"usbridge-client/internal/service"
)

func main() {
	if len(os.Args) < 4 {
		fmt.Println("usage: iscsi_target_harness <path-to-test-image> <iqn> <bind-host:port>")
		os.Exit(1)
	}
	imgPath := os.Args[1]
	iqn := os.Args[2]
	bindAddr := os.Args[3]

	host, port, err := splitHostPort(bindAddr)
	if err != nil {
		fmt.Println("bad bind address:", err)
		os.Exit(1)
	}

	runner := service.NewIscsiTargetRunner(imgPath, false, host, iqn)
	if err := runner.Start(port); err != nil {
		fmt.Println("Start error:", err)
		os.Exit(1)
	}
	fmt.Printf("READY target=%q bind=%s:%d file=%s\n", iqn, host, port, imgPath)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	_ = runner.Stop()
}

func splitHostPort(addr string) (string, int, error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return "", 0, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return "", 0, err
	}
	return host, port, nil
}
