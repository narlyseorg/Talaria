package monitor

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/net"
)

type ConnectivityMetrics struct {
	ActiveConnections int               `json:"active_connections"` // ESTABLISHED
	ListeningPorts    int               `json:"listening_ports"`    // LISTEN
	VPNActive         bool              `json:"vpn_active"`
	VPNInterface      string            `json:"vpn_interface"`
	BluetoothDevices  []BluetoothDevice `json:"bluetooth_devices"`
}

type BluetoothDevice struct {
	Name      string `json:"name"`
	Battery   string `json:"battery"` // "85%" or ""
	Connected bool   `json:"connected"`
}

var (
	cachedBluetooth   []BluetoothDevice
	lastBluetoothTime time.Time
	connMutex         sync.Mutex

	connectCache = NewCachedValue[ConnectivityMetrics](2 * time.Second)

	connDetailsCache = NewCachedValue[ConnectionDetails](2 * time.Second)
)

func GetConnectivity() ConnectivityMetrics {
	return connectCache.Get(fetchConnectivity)
}

func fetchConnectivity() ConnectivityMetrics {
	m := ConnectivityMetrics{}

	conns, err := net.Connections("tcp")
	if err == nil {
		for _, c := range conns {
			if c.Status == "ESTABLISHED" {
				m.ActiveConnections++
			} else if c.Status == "LISTEN" {
				m.ListeningPorts++
			}
		}
	}

	ifaces, err := net.Interfaces()
	if err == nil {
		for _, iface := range ifaces {

			if strings.HasPrefix(iface.Name, "utun") {

				hasIP := false
				for _, addr := range iface.Addrs {

					if len(addr.Addr) > 0 {
						if strings.Contains(addr.Addr, ":") {

							if !strings.HasPrefix(addr.Addr, "fe80:") {
								hasIP = true
							}
						} else {

							hasIP = true
						}
					}
				}

				if hasIP {
					m.VPNActive = true
					m.VPNInterface = iface.Name
					break // Found one, that's enough
				}
			}
		}
	}

	connMutex.Lock()
	now := time.Now()
	if now.Sub(lastBluetoothTime) > 30*time.Second {
		go updateBluetooth()
		lastBluetoothTime = now
	}
	m.BluetoothDevices = cachedBluetooth
	connMutex.Unlock()

	return m
}

func updateBluetooth() {

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	out, err := RunCmd(ctx, "system_profiler", "SPBluetoothDataType")
	if err != nil {
		return
	}

	var devices []BluetoothDevice
	lines := strings.Split(string(out), "\n")

	var inConnectedSection bool
	var deviceIndent int
	var currentDevice *BluetoothDevice

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}

		indent := 0
		for i := 0; i < len(line); i++ {
			if line[i] == ' ' {
				indent++
			} else {
				break
			}
		}
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "Connected:") {
			inConnectedSection = true
			deviceIndent = 0 // Will be set by first device
			currentDevice = nil
			continue
		} else if strings.HasPrefix(trimmed, "Not Connected:") || strings.HasPrefix(trimmed, "Bluetooth Controller:") {
			inConnectedSection = false

			if currentDevice != nil {
				devices = append(devices, *currentDevice)
				currentDevice = nil
			}
			continue
		}

		if inConnectedSection {
			if strings.HasSuffix(trimmed, ":") {

				if deviceIndent == 0 || indent == deviceIndent {
					if currentDevice != nil {
						devices = append(devices, *currentDevice)
					}
					name := strings.TrimSuffix(trimmed, ":")
					currentDevice = &BluetoothDevice{Name: name, Connected: true}
					deviceIndent = indent
				}
			} else if currentDevice != nil && indent > deviceIndent {

				if strings.Contains(trimmed, "Battery Level:") {
					val := strings.TrimPrefix(trimmed, "Battery Level:")
					currentDevice.Battery = strings.TrimSpace(val)
				}
			}
		}
	}

	if currentDevice != nil {
		devices = append(devices, *currentDevice)
	}

	connMutex.Lock()
	cachedBluetooth = devices
	connMutex.Unlock()
}

type ConnectionDetails struct {
	Active    []ConnectionInfo `json:"active"`
	Listening []ConnectionInfo `json:"listening"`
}

type ConnectionInfo struct {
	Process  string `json:"process"`
	PID      int    `json:"pid"`
	Protocol string `json:"protocol"` // TCP
	Local    string `json:"local"`
	Remote   string `json:"remote"`
	State    string `json:"state"`
}

func GetConnectionDetails() ConnectionDetails {
	return connDetailsCache.Get(fetchConnectionDetails)
}

func fetchConnectionDetails() ConnectionDetails {
	d := ConnectionDetails{
		Active:    []ConnectionInfo{},
		Listening: []ConnectionInfo{},
	}

	conns, err := net.Connections("tcp")
	if err != nil {
		return d
	}

	for _, c := range conns {
		if c.Status != "LISTEN" && c.Status != "ESTABLISHED" {
			continue
		}

		name := ""
		if c.Pid > 0 {
			name = ResolveProcessName(c.Pid)
			if name == "" {
				name = fmt.Sprintf("PID %d", c.Pid)
			}
		} else {
			name = "kernel/unknown"
		}

		info := ConnectionInfo{
			Process:  name,
			PID:      int(c.Pid),
			Protocol: "TCP",
			State:    c.Status,
			Local:    fmt.Sprintf("%s:%d", c.Laddr.IP, c.Laddr.Port),
		}

		if c.Status == "LISTEN" {
			info.Remote = "*"
			d.Listening = append(d.Listening, info)
		} else {
			info.Remote = fmt.Sprintf("%s:%d", c.Raddr.IP, c.Raddr.Port)
			d.Active = append(d.Active, info)
		}
	}

	return d
}
