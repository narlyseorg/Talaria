package monitor

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	psnet "github.com/shirou/gopsutil/v4/net"
)

type NetworkMetrics struct {
	BytesIn        uint64             `json:"bytes_in"`
	BytesOut       uint64             `json:"bytes_out"`
	BytesInRate    float64            `json:"bytes_in_rate"`
	BytesOutRate   float64            `json:"bytes_out_rate"`
	Interfaces     []NetworkInterface `json:"interfaces"`
	LocalIP        string             `json:"local_ip"`
	PublicIP       string             `json:"public_ip"`
	WiFiSSID       string             `json:"wifi_ssid"`
	ConnectionType string             `json:"connection_type"` // "Wi-Fi", "Ethernet", "Unknown"
}

type NetworkInterface struct {
	Name     string `json:"name"`
	BytesIn  uint64 `json:"bytes_in"`
	BytesOut uint64 `json:"bytes_out"`
}

var (
	lastNetTime  time.Time
	lastBytesIn  uint64
	lastBytesOut uint64

	cachedPublicIP   string
	lastPublicIPTime time.Time

	cachedSSID   string
	lastSSIDTime time.Time

	cachedLocalIP   string
	cachedVPNActive bool

	cachedSSIDForChange string

	publicIPRefreshPending bool

	cachedPrimaryInterface string

	cachedWiFiIfaceName string
	cachedWiFiIfaceOnce sync.Once

	netMutex sync.Mutex

	httpClient = &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			DisableKeepAlives: true,
		},
	}
)

func GetNetwork() NetworkMetrics {
	m := NetworkMetrics{}

	counters, err := psnet.IOCounters(true)
	if err == nil {
		for _, c := range counters {
			m.BytesIn += c.BytesRecv
			m.BytesOut += c.BytesSent
			m.Interfaces = append(m.Interfaces, NetworkInterface{
				Name:     c.Name,
				BytesIn:  c.BytesRecv,
				BytesOut: c.BytesSent,
			})
		}
	}

	m.LocalIP, m.ConnectionType = getLocalIP()

	now := time.Now()
	netMutex.Lock()
	if !lastNetTime.IsZero() && m.BytesIn > 0 {
		dt := now.Sub(lastNetTime).Seconds()
		if dt > 0 {
			if m.BytesIn >= lastBytesIn {
				m.BytesInRate = sanitizeFloat(float64(m.BytesIn-lastBytesIn) / dt)
			}
			if m.BytesOut >= lastBytesOut {
				m.BytesOutRate = sanitizeFloat(float64(m.BytesOut-lastBytesOut) / dt)
			}
		}
	}
	lastBytesIn = m.BytesIn
	lastBytesOut = m.BytesOut
	lastNetTime = now

	vpnActive := GetConnectivity().VPNActive
	localIPChanged := m.LocalIP != "" && m.LocalIP != cachedLocalIP
	vpnChanged := vpnActive != cachedVPNActive

	ssidForChangeCurrent := cachedSSID // read inside lock, updated each 5s by SSID block below
	ssidChanged := ssidForChangeCurrent != "" && ssidForChangeCurrent != cachedSSIDForChange
	if localIPChanged {
		cachedLocalIP = m.LocalIP
	}
	if vpnChanged {
		cachedVPNActive = vpnActive
	}
	if ssidChanged {
		cachedSSIDForChange = ssidForChangeCurrent
	}
	if localIPChanged || vpnChanged || ssidChanged {
		publicIPRefreshPending = true
		lastPublicIPTime = time.Time{} // force immediate fetch this tick
	}

	retryInterval := 60 * time.Second
	if publicIPRefreshPending {
		retryInterval = 5 * time.Second
	}
	if now.Sub(lastPublicIPTime) > retryInterval {
		lastPublicIPTime = now
		go updatePublicIP()
	}
	m.PublicIP = cachedPublicIP

	ssidExpired := false
	if now.Sub(lastSSIDTime) > 5*time.Second {
		ssidExpired = true
	}
	currentSSID := cachedSSID
	netMutex.Unlock()

	if m.ConnectionType == "Wi-Fi" {
		if ssidExpired {
			newSSID := GetWiFiSSID()
			netMutex.Lock()
			cachedSSID = newSSID
			lastSSIDTime = now
			netMutex.Unlock()
			m.WiFiSSID = newSSID
		} else {
			m.WiFiSSID = currentSSID
		}
	}

	return m
}

func getLocalIP() (string, string) {

	if cachedPrimaryInterface != "" {
		if iface, err := net.InterfaceByName(cachedPrimaryInterface); err == nil {
			if ip, connType := checkInterface(iface); ip != "" {
				return ip, connType
			}
		}

		cachedPrimaryInterface = ""
	}

	ifaces, err := net.Interfaces()
	if err == nil {
		for _, i := range ifaces {

			if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 {
				continue
			}
			if ip, connType := checkInterface(&i); ip != "" {
				cachedPrimaryInterface = i.Name // Cache it!
				return ip, connType
			}
		}
	}
	return "", ""
}

func checkInterface(i *net.Interface) (string, string) {
	addrs, err := i.Addrs()
	if err != nil {
		return "", ""
	}

	for _, addr := range addrs {
		var ip net.IP
		switch v := addr.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}

		if ip != nil && ip.To4() != nil && !ip.IsLoopback() {
			connType := "Ethernet"

			if strings.HasPrefix(i.Name, "en") {

				cachedWiFiIfaceOnce.Do(func() {
					cachedWiFiIfaceName = GetWiFiInterfaceName()
				})
				if cachedWiFiIfaceName != "" && i.Name == cachedWiFiIfaceName {
					connType = "Wi-Fi"
				}
			}
			return ip.String(), connType
		}
	}
	return "", ""
}

func updatePublicIP() {

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", "https://checkip.amazonaws.com", nil)
	resp, err := httpClient.Do(req)
	if err != nil {
		return // network not ready yet; publicIPRefreshPending stays true → retry in 5s
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	ip := strings.TrimSpace(string(body))
	if len(ip) > 0 {
		netMutex.Lock()
		cachedPublicIP = ip
		publicIPRefreshPending = false // success: back to normal 60s cycle
		netMutex.Unlock()
	}
}
