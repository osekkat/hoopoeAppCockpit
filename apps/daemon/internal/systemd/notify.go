// Package systemd provides the tiny sd_notify surface the daemon needs when
// running under a Type=notify unit.
package systemd

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

type datagramSender func(ctx context.Context, socket string, payload string) error

// Notifier sends sd_notify datagrams to NOTIFY_SOCKET. The zero value is
// usable and becomes a no-op outside systemd.
type Notifier struct {
	Env    func(string) string
	PID    int
	sendFn datagramSender
}

func NewNotifier() Notifier {
	return Notifier{
		Env:    os.Getenv,
		PID:    os.Getpid(),
		sendFn: sendDatagram,
	}
}

// Ready tells systemd that the HTTP listener is bound and ready to accept
// requests.
func (n Notifier) Ready(ctx context.Context, status string) error {
	if strings.TrimSpace(status) == "" {
		return n.Notify(ctx, "READY=1")
	}
	return n.Notify(ctx, "READY=1\nSTATUS="+status)
}

// Watchdog sends the periodic keepalive datagram expected when WatchdogSec is
// set on the unit.
func (n Notifier) Watchdog(ctx context.Context) error {
	return n.Notify(ctx, "WATCHDOG=1")
}

// Notify sends payload to NOTIFY_SOCKET. It returns nil when systemd did not
// provide a socket for this process.
func (n Notifier) Notify(ctx context.Context, payload string) error {
	env := n.Env
	if env == nil {
		env = os.Getenv
	}
	socket := env("NOTIFY_SOCKET")
	if socket == "" {
		return nil
	}
	send := n.sendFn
	if send == nil {
		send = sendDatagram
	}
	if err := send(ctx, socket, payload); err != nil {
		return fmt.Errorf("systemd notify %q: %w", socket, err)
	}
	return nil
}

// WatchdogInterval returns half the systemd-provided WATCHDOG_USEC, which is
// the recommended cadence for daemon keepalives.
func (n Notifier) WatchdogInterval() (time.Duration, bool, error) {
	env := n.Env
	if env == nil {
		env = os.Getenv
	}
	usecValue := env("WATCHDOG_USEC")
	if usecValue == "" {
		return 0, false, nil
	}
	pidValue := env("WATCHDOG_PID")
	if pidValue != "" {
		pid, err := strconv.Atoi(pidValue)
		if err != nil {
			return 0, false, fmt.Errorf("parse WATCHDOG_PID: %w", err)
		}
		currentPID := n.PID
		if currentPID == 0 {
			currentPID = os.Getpid()
		}
		if pid != currentPID {
			return 0, false, nil
		}
	}
	usec, err := strconv.ParseInt(usecValue, 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("parse WATCHDOG_USEC: %w", err)
	}
	if usec <= 0 {
		return 0, false, fmt.Errorf("parse WATCHDOG_USEC: value must be positive")
	}
	interval := time.Duration(usec) * time.Microsecond / 2
	if interval <= 0 {
		interval = time.Microsecond
	}
	return interval, true, nil
}

func sendDatagram(ctx context.Context, socket string, payload string) error {
	addrName := socket
	if strings.HasPrefix(addrName, "@") {
		addrName = "\x00" + strings.TrimPrefix(addrName, "@")
	}
	addr := &net.UnixAddr{Name: addrName, Net: "unixgram"}
	conn, err := net.DialUnix("unixgram", nil, addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetWriteDeadline(deadline); err != nil {
			return err
		}
	}
	_, err = conn.Write([]byte(payload))
	return err
}
