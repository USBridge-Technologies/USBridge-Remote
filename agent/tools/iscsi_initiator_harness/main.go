// Integration-test harness: exercises the REAL production iscsi.Initiator
// (agent/internal/iscsi) — the exact code App.ReplaceDevices/ClearDevices
// call — against a running client iSCSI target. Run with:
//
//	go run ./tools/iscsi_initiator_harness <portal> <iqn>
//
// Logs in, prints the resulting device path, and waits for SIGINT/SIGTERM
// to log back out (so the caller can read/checksum the device in between).
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"usbridge_agent/internal/iscsi"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Println("usage: iscsi_initiator_harness <portal> <iqn>")
		os.Exit(1)
	}
	portal := os.Args[1]
	targetIQN := os.Args[2]

	initiator := iscsi.New()
	if !initiator.Available() {
		fmt.Println("iSCSI initiator not available on this platform/host")
		os.Exit(1)
	}

	opts := iscsi.LoginOptions{Portal: portal, TargetIQN: targetIQN, LUN: 0}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	result, err := initiator.Login(ctx, opts)
	cancel()
	if err != nil {
		fmt.Println("Login error:", err)
		os.Exit(1)
	}
	fmt.Printf("LOGGED_IN device=%s session=%s\n", result.DevicePath, result.SessionID)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel2()
	if err := initiator.Logout(ctx2, opts); err != nil {
		fmt.Println("Logout error:", err)
		os.Exit(1)
	}
	fmt.Println("LOGGED_OUT")
}
