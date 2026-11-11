package proxyprobe

import (
	"errors"
	"net"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestSessionErrRecognizesListeningSocketWithFullAcceptQueue(t *testing.T) {
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	raw, err := listener.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var listenErr error
	if err := raw.Control(func(fd uintptr) { listenErr = syscall.Listen(int(fd), 1) }); err != nil || listenErr != nil {
		t.Fatalf("set test listener backlog: %v %v", err, listenErr)
	}
	var connections []net.Conn
	defer func() {
		for _, connection := range connections {
			_ = connection.Close()
		}
	}()
	var timedOut bool
	for range 8 {
		connection, err := net.DialTimeout("tcp4", listener.Addr().String(), 200*time.Millisecond)
		if err != nil {
			var netErr net.Error
			timedOut = errors.As(err, &netErr) && netErr.Timeout()
			break
		}
		connections = append(connections, connection)
	}
	if !timedOut {
		t.Fatal("test listener did not produce a loopback dial timeout")
	}
	process := &probeProcess{
		cmd:    &exec.Cmd{Process: &os.Process{Pid: os.Getpid()}},
		done:   make(chan struct{}),
		output: &limitedBuffer{limit: 128},
	}
	session := &Session{ports: []int{listener.Addr().(*net.TCPAddr).Port}, process: process}
	if err := session.Err(); err != nil {
		t.Fatalf("busy listening socket was reported as lost: %v", err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if err := session.Err(); err == nil || !strings.Contains(err.Error(), "lost 1 of 1 proxy probe listeners") {
		t.Fatalf("closed listening socket was not reported as lost: %v", err)
	}
}
