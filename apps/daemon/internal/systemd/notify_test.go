package systemd

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestReadySendsNotifyDatagram(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "notify.sock")
	addr := &net.UnixAddr{Name: socketPath, Net: "unixgram"}
	conn, err := net.ListenUnixgram("unixgram", addr)
	if err != nil {
		t.Fatalf("listen unixgram: %v", err)
	}
	defer conn.Close()

	notifier := NewNotifier()
	notifier.Env = func(key string) string {
		if key == "NOTIFY_SOCKET" {
			return socketPath
		}
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := notifier.Ready(ctx, "hoopoe ready"); err != nil {
		t.Fatalf("Ready: %v", err)
	}

	buf := make([]byte, 256)
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	n, _, err := conn.ReadFromUnix(buf)
	if err != nil {
		t.Fatalf("read notify datagram: %v", err)
	}
	if got, want := string(buf[:n]), "READY=1\nSTATUS=hoopoe ready"; got != want {
		t.Fatalf("datagram = %q, want %q", got, want)
	}
}

func TestNotifyIsNoopWithoutSocket(t *testing.T) {
	notifier := NewNotifier()
	notifier.Env = func(string) string { return "" }
	if err := notifier.Ready(context.Background(), "ignored"); err != nil {
		t.Fatalf("Ready without socket: %v", err)
	}
}

func TestWatchdogIntervalHonorsPID(t *testing.T) {
	notifier := NewNotifier()
	notifier.PID = os.Getpid()
	notifier.Env = func(key string) string {
		switch key {
		case "WATCHDOG_USEC":
			return "30000000"
		case "WATCHDOG_PID":
			return "1"
		default:
			return ""
		}
	}
	if _, ok, err := notifier.WatchdogInterval(); err != nil || ok {
		t.Fatalf("mismatched pid interval = ok:%v err:%v, want inactive nil", ok, err)
	}

	notifier.Env = func(key string) string {
		switch key {
		case "WATCHDOG_USEC":
			return "30000000"
		case "WATCHDOG_PID":
			return strconv.Itoa(os.Getpid())
		default:
			return ""
		}
	}
	interval, ok, err := notifier.WatchdogInterval()
	if err != nil {
		t.Fatalf("WatchdogInterval: %v", err)
	}
	if !ok {
		t.Fatal("WatchdogInterval inactive, want active")
	}
	if interval != 15*time.Second {
		t.Fatalf("interval = %s, want 15s", interval)
	}
}
