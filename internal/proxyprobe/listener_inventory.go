package proxyprobe

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Loopback connection attempts can time out while a listener has a full accept
// queue. Read the listening sockets owned by the isolated process directly.
func missingLinuxListeners(pid int, ports []int) ([]int, error) {
	proc := filepath.Join("/proc", strconv.Itoa(pid))
	fdDir := filepath.Join(proc, "fd")
	fds, err := os.ReadDir(fdDir)
	if err != nil {
		return nil, err
	}
	owned := make(map[string]bool, len(fds))
	for _, fd := range fds {
		target, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
		if errors.Is(err, os.ErrNotExist) {
			continue // A connection may have closed after the directory read.
		}
		if err != nil {
			return nil, err
		}
		if inode, ok := strings.CutPrefix(target, "socket:["); ok {
			owned[strings.TrimSuffix(inode, "]")] = true
		}
	}
	table, err := os.Open(filepath.Join(proc, "net", "tcp"))
	if err != nil {
		return nil, err
	}
	defer table.Close()
	return missingOwnedLoopbackPorts(table, owned, ports)
}

func missingOwnedLoopbackPorts(table io.Reader, owned map[string]bool, ports []int) ([]int, error) {
	loopback := fmt.Sprintf("%08X:", binary.NativeEndian.Uint32([]byte{127, 0, 0, 1}))
	ready := make(map[int]bool, len(ports))
	lines := bufio.NewScanner(table)
	for lines.Scan() {
		fields := strings.Fields(lines.Text())
		if len(fields) < 10 || fields[3] != "0A" || !owned[fields[9]] {
			continue
		}
		// Linux reports IPv4 loopback in host byte order and the port in hex.
		if port, ok := strings.CutPrefix(fields[1], loopback); ok {
			value, err := strconv.ParseUint(port, 16, 16)
			if err == nil {
				ready[int(value)] = true
			}
		}
	}
	if err := lines.Err(); err != nil {
		return nil, err
	}
	var missing []int
	for _, port := range ports {
		if !ready[port] {
			missing = append(missing, port)
		}
	}
	return missing, nil
}
