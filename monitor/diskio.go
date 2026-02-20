package monitor

import (
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/disk"
)

type DiskIOMetrics struct {
	ReadMBps  float64 `json:"read_mbps"`  // Read throughput MB/s
	WriteMBps float64 `json:"write_mbps"` // Write throughput MB/s
	TotalMBps float64 `json:"total_mbps"` // Combined throughput
	ReadMB    float64 `json:"read_mb"`    // Cumulative read since boot
	WriteMB   float64 `json:"write_mb"`   // Cumulative write since boot
	TotalMB   float64 `json:"total_mb"`   // Cumulative total since boot
}

var (
	lastDiskIOTime time.Time
	lastReadBytes  uint64
	lastWriteBytes uint64
	diskIOMutex    sync.Mutex
)

func GetDiskIO() DiskIOMetrics {
	m := DiskIOMetrics{}

	counters, err := disk.IOCounters()
	if err != nil {
		return m
	}

	var totalRead, totalWrite uint64

	for _, c := range counters {
		totalRead += c.ReadBytes
		totalWrite += c.WriteBytes
	}

	m.ReadMB = float64(totalRead) / float64(MB)
	m.WriteMB = float64(totalWrite) / float64(MB)
	m.TotalMB = m.ReadMB + m.WriteMB

	now := time.Now()
	diskIOMutex.Lock()
	if !lastDiskIOTime.IsZero() {
		dt := now.Sub(lastDiskIOTime).Seconds()
		if dt > 0 {
			if totalRead >= lastReadBytes {
				m.ReadMBps = sanitizeFloat(float64(totalRead-lastReadBytes) / float64(MB) / dt)
			}
			if totalWrite >= lastWriteBytes {
				m.WriteMBps = sanitizeFloat(float64(totalWrite-lastWriteBytes) / float64(MB) / dt)
			}
			m.TotalMBps = m.ReadMBps + m.WriteMBps
		}
	}

	lastReadBytes = totalRead
	lastWriteBytes = totalWrite
	lastDiskIOTime = now
	diskIOMutex.Unlock()

	return m
}
