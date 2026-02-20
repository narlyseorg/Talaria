package monitor

import (
	"fmt"
	"time"

	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
)

type SystemMetrics struct {
	Hostname    string `json:"hostname"`
	OSVersion   string `json:"os_version"`
	KernelVer   string `json:"kernel_version"`
	Uptime      string `json:"uptime"`
	LoadAvg     string `json:"load_avg"`
	CurrentTime string `json:"current_time"`
	CurrentDate string `json:"current_date"`
	Arch        string `json:"arch"`
}

var (
	cachedOSVersion string // "macOS 15.x (24Axx)"
	cachedKernelVer string // "24.1.0"
	cachedArch      string // "arm64"
	cachedHostname  string // "MacBook-Air.local"
)

func init() {

	info, err := host.Info()
	if err == nil {
		cachedOSVersion = fmt.Sprintf("%s %s (%s)", info.Platform, info.PlatformVersion, info.KernelVersion)
		cachedHostname = info.Hostname
		cachedKernelVer = info.KernelVersion
		cachedArch = info.KernelArch
	}
}

func GetSystem() SystemMetrics {
	now := time.Now()
	m := SystemMetrics{
		CurrentTime: now.Format("15:04:05"),
		CurrentDate: now.Format("Monday, 02 Jan 2006"),
		OSVersion:   cachedOSVersion,
		KernelVer:   cachedKernelVer,
		Arch:        cachedArch,
		Hostname:    cachedHostname,
	}

	uptimeSeconds, err := host.Uptime()
	if err == nil {
		d := time.Duration(uptimeSeconds) * time.Second
		days := int(d.Hours()) / 24
		hours := int(d.Hours()) % 24
		mins := int(d.Minutes()) % 60

		if days > 0 {
			m.Uptime = fmt.Sprintf("%d days, %d:%02d", days, hours, mins)
		} else {
			m.Uptime = fmt.Sprintf("%d:%02d", hours, mins)
		}
	}

	loadAvg, err := load.Avg()
	if err == nil {
		m.LoadAvg = fmt.Sprintf("%.2f %.2f %.2f", loadAvg.Load1, loadAvg.Load5, loadAvg.Load15)
	}

	return m
}
