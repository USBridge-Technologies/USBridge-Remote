//go:build linux || windows || darwin

package usbpass

import (
	"net"
	"testing"
)

func TestAeadStreamRoundTrip(t *testing.T) {
	key := deriveSessionKey([]byte("test-master-key"))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	done := make(chan []byte, 1)
	errCh := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			errCh <- err
			return
		}
		s, err := newAeadStream(conn, key)
		if err != nil {
			errCh <- err
			return
		}
		got, err := s.recvFrame()
		if err != nil {
			errCh <- err
			return
		}
		done <- got
	}()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	s, err := newAeadStream(conn, key)
	if err != nil {
		t.Fatal(err)
	}
	frame := encodeAttachFrame(attachPayload{
		BusID:         "1-92",
		VID:           0x0781,
		PID:           0x55A9,
		Speed:         3,
		DeviceDesc:    syntheticDeviceDesc(0x0781, 0x55A9),
		ConfigDesc:    syntheticMSCConfig(),
		ExportHost:    "127.0.0.1",
		ExportService: "3240",
	})
	if err := s.sendFrame(frame); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-errCh:
		t.Fatal(err)
	case got := <-done:
		msgType, payload, err := decodeUsbpFrame(got)
		if err != nil {
			t.Fatal(err)
		}
		if msgType != msgAttach {
			t.Fatalf("msgType = %d, want %d", msgType, msgAttach)
		}
		if len(payload) == 0 {
			t.Fatal("empty attach payload")
		}
	}
}

func TestFrameCodecRoundTrip(t *testing.T) {
	hello := encodeHelloFrame(usbAesRoleClient, usbAesProtoVersion)
	msgType, payload, err := decodeUsbpFrame(hello)
	if err != nil {
		t.Fatal(err)
	}
	if msgType != msgHello {
		t.Fatalf("msgType = %d, want %d", msgType, msgHello)
	}
	if len(payload) != 2 || payload[0] != usbAesRoleClient || payload[1] != usbAesProtoVersion {
		t.Fatalf("hello payload = %v", payload)
	}

	// HelloAck as the agent would encode it: ok=1, detail="agent".
	ack := encodeUsbpFrame(msgHelloAck, append([]byte{1}, appendUsbpString(nil, "agent")...))
	msgType, payload, err = decodeUsbpFrame(ack)
	if err != nil {
		t.Fatal(err)
	}
	if msgType != msgHelloAck {
		t.Fatalf("msgType = %d, want %d", msgType, msgHelloAck)
	}
	ok, detail, err := decodeHelloAck(payload)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || detail != "agent" {
		t.Fatalf("ok=%v detail=%q", ok, detail)
	}

	detach := encodeDetachFrame(7)
	msgType, payload, err = decodeUsbpFrame(detach)
	if err != nil {
		t.Fatal(err)
	}
	if msgType != msgDetach {
		t.Fatalf("msgType = %d, want %d", msgType, msgDetach)
	}
	if len(payload) != 4 {
		t.Fatalf("detach payload len = %d", len(payload))
	}
}
