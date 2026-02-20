package monitor

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"
)

type BatteryMetrics struct {
	Percent        int     `json:"percent"`
	Charging       bool    `json:"charging"`
	PowerSource    string  `json:"power_source"`
	TimeLeft       string  `json:"time_left"`
	HasBattery     bool    `json:"has_battery"`
	CycleCount     int     `json:"cycle_count"`
	DesignCapacity int     `json:"design_capacity_mah"` // mAh
	MaxCapacity    int     `json:"max_capacity_mah"`    // mAh (current actual)
	HealthPercent  float64 `json:"health_percent"`      // max/design * 100
	Temperature    float64 `json:"temperature"`         // Celsius
}

var batteryCache = NewCachedValue[BatteryMetrics](3 * time.Second)

func GetBattery() BatteryMetrics {
	return batteryCache.Get(fetchBattery)
}

func fetchBattery() BatteryMetrics {
	m := BatteryMetrics{}

	type pmsetResult struct {
		PowerSource string
		Charging    bool
		HasBattery  bool
		Percent     int
		TimeLeft    string
	}
	type ioregResult struct {
		CycleCount     int
		DesignCapacity int
		MaxCapacity    int
		Temperature    float64
		HealthPercent  float64
	}

	var pmRes pmsetResult
	var ioRes ioregResult

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()

		out, err := RunCmd(ctx, "pmset", "-g", "batt")
		if err != nil {
			return
		}

		output := string(out)
		lines := strings.Split(output, "\n")

		if len(lines) > 0 {
			if strings.Contains(lines[0], "AC Power") {
				pmRes.PowerSource = "AC Power"

			} else if strings.Contains(lines[0], "Battery Power") {
				pmRes.PowerSource = "Battery"
				pmRes.Charging = false
			}
		}

		for _, line := range lines[1:] {
			if !strings.Contains(line, "InternalBattery") {
				continue
			}
			pmRes.HasBattery = true

			parts := strings.Split(line, "\t")
			for _, part := range parts {
				part = strings.TrimSpace(part)

				if idx := strings.Index(part, "%"); idx > 0 {
					start := idx
					for start > 0 && part[start-1] >= '0' && part[start-1] <= '9' {
						start--
					}
					if pct, err := strconv.Atoi(part[start:idx]); err == nil {
						pmRes.Percent = pct
					}
				}

				if strings.Contains(part, "charging") && !strings.Contains(part, "not charging") && !strings.Contains(part, "charged") {
					pmRes.Charging = true
				}
				if strings.Contains(part, "remaining") || strings.Contains(part, "until") {
					pmRes.TimeLeft = strings.TrimSpace(part)
				}
			}
			break
		}
	}()

	go func() {
		defer wg.Done()

		ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
		defer cancel()

		ioOut, ioErr := RunCmd(ctx, "ioreg", "-r", "-n", "AppleSmartBattery", "-d", "1")
		if ioErr != nil {
			return
		}

		ioData := string(ioOut)
		for _, line := range strings.Split(ioData, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "\"CycleCount\"") {
				ioRes.CycleCount = extractInt(line)
			} else if strings.HasPrefix(line, "\"DesignCapacity\"") {
				ioRes.DesignCapacity = extractInt(line)
			} else if strings.HasPrefix(line, "\"NominalChargeCapacity\"") {
				ioRes.MaxCapacity = extractInt(line)
			} else if strings.HasPrefix(line, "\"Temperature\"") {

				ioRes.Temperature = float64(extractInt(line)) / 100.0
			}
		}
		if ioRes.DesignCapacity > 0 && ioRes.MaxCapacity > 0 {
			ioRes.HealthPercent = float64(ioRes.MaxCapacity) / float64(ioRes.DesignCapacity) * 100.0
		}
	}()

	wg.Wait()

	m.PowerSource = pmRes.PowerSource
	m.Charging = pmRes.Charging
	m.HasBattery = pmRes.HasBattery
	m.Percent = pmRes.Percent
	m.TimeLeft = pmRes.TimeLeft
	m.CycleCount = ioRes.CycleCount
	m.DesignCapacity = ioRes.DesignCapacity
	m.MaxCapacity = ioRes.MaxCapacity
	m.Temperature = ioRes.Temperature
	m.HealthPercent = ioRes.HealthPercent

	return m
}

func extractInt(line string) int {
	parts := strings.SplitN(line, "=", 2)
	if len(parts) < 2 {
		return 0
	}
	val := strings.TrimSpace(parts[1])
	n, _ := strconv.Atoi(val)
	return n
}
