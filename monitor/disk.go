package monitor

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type DiskInfo struct {
	Filesystem string  `json:"filesystem"`
	MountPoint string  `json:"mount_point"`
	TotalGB    float64 `json:"total_gb"`
	UsedGB     float64 `json:"used_gb"`
	FreeGB     float64 `json:"free_gb"`
	UsedPct    float64 `json:"used_percent"`
}

type StorageCategory struct {
	Name string  `json:"name"`
	Size float64 `json:"size_gb"`
	Icon string  `json:"icon"`
}

type StorageBreakdown struct {
	TotalGB     float64           `json:"total_gb"`
	UsedGB      float64           `json:"used_gb"`
	FreeGB      float64           `json:"free_gb"`
	PurgeableGB float64           `json:"purgeable_gb"` // APFS purgeable (local TM snapshots)
	Categories  []StorageCategory `json:"categories"`
}

var (
	cachedBreakdown     StorageBreakdown
	breakdownMutex      sync.RWMutex
	lastBreakdownUpdate time.Time

	breakdownPending bool

	cachedDisks  []DiskInfo
	lastDiskTime time.Time
	diskMutex    sync.Mutex
)

func init() {

	go func() {
		updateBreakdown()
	}()
}

func GetDisks() []DiskInfo {
	diskMutex.Lock()

	if time.Since(lastDiskTime) < 1*time.Second && cachedDisks != nil {
		result := cachedDisks
		diskMutex.Unlock()
		return result
	}
	diskMutex.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	out, err := RunCmd(ctx, "df", "-k")
	if err != nil {
		return nil
	}

	var disks []DiskInfo

	const gbDivisor = 976562.5

	lines := strings.Split(string(out), "\n")
	for i, line := range lines {
		if i == 0 {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 9 {
			continue
		}

		fs := fields[0]

		mount := strings.Join(fields[8:], " ")

		if !strings.HasPrefix(fs, "/dev/") {
			continue
		}

		if isNoisyMount(mount) {
			continue
		}

		totalKB, _ := strconv.ParseFloat(fields[1], 64)
		usedKB, _ := strconv.ParseFloat(fields[2], 64)
		freeKB, _ := strconv.ParseFloat(fields[3], 64)

		pctStr := strings.TrimSuffix(fields[4], "%")
		pct, _ := strconv.ParseFloat(pctStr, 64)

		disks = append(disks, DiskInfo{
			Filesystem: fs,
			MountPoint: mount,
			TotalGB:    totalKB / gbDivisor,
			UsedGB:     usedKB / gbDivisor,
			FreeGB:     freeKB / gbDivisor,
			UsedPct:    pct,
		})
	}

	diskMutex.Lock()
	cachedDisks = disks
	lastDiskTime = time.Now()
	diskMutex.Unlock()

	return disks
}

type apfsContainerInfo struct {
	TotalBytes     int64 // APFS container ceiling
	UsedBytes      int64 // bytes allocated by volumes (df-style: excludes purgeable of individual vols)
	FreeBytes      int64 // bytes not allocated to any volume
	PurgeableBytes int64 // APFS purgeable (TM snapshots, caches) — counted in UsedBytes by volumes
}

var rApfsBytes = regexp.MustCompile(`(\d+) B \(`)

func getAPFSContainerInfo() (apfsContainerInfo, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := RunCmd(ctx, "diskutil", "apfs", "list")
	if err != nil {
		return apfsContainerInfo{}, false
	}

	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()
	diOut, diErr := RunCmd(ctx2, "diskutil", "info", "/System/Volumes/Data")

	lines := strings.Split(string(out), "\n")

	var info apfsContainerInfo
	inMainContainer := false
	seenRoot := false

	for _, line := range lines {
		l := strings.TrimSpace(line)

		if strings.HasPrefix(l, "+-- Container disk") {

			if seenRoot {
				break
			}
			inMainContainer = true
			info = apfsContainerInfo{} // reset for each container
		}

		if !inMainContainer {
			continue
		}

		m := rApfsBytes.FindStringSubmatch(l)
		if m == nil {

			if strings.Contains(l, "Snapshot Mount Point:") && strings.Contains(l, "/") {
				fields := strings.Fields(l)
				for _, f := range fields {
					if f == "/" {
						seenRoot = true
					}
				}
			}
			continue
		}

		val, _ := strconv.ParseInt(m[1], 10, 64)
		switch {
		case strings.Contains(l, "Size (Capacity Ceiling)"):
			info.TotalBytes = val
		case strings.Contains(l, "Capacity In Use By Volumes"):
			info.UsedBytes = val
		case strings.Contains(l, "Capacity Not Allocated"):
			info.FreeBytes = val
		}
	}

	if info.TotalBytes == 0 {
		return apfsContainerInfo{}, false
	}

	var rPurgeable = regexp.MustCompile(`Volume Purgeable Space:[\s\S]*?(\d+) Bytes`)
	if diErr == nil {
		if pm := rPurgeable.FindSubmatch(diOut); pm != nil {
			info.PurgeableBytes, _ = strconv.ParseInt(string(pm[1]), 10, 64)
		}
	}

	return info, true
}

func isNoisyMount(mount string) bool {
	noisyPrefixes := []string{
		"/Library/Developer/CoreSimulator/",
		"/Library/Developer/XCTestDevices/",
		"/private/var/folders/",
		"/System/Volumes/VM",
		"/System/Volumes/Preboot",
		"/System/Volumes/Recovery",
		"/System/Volumes/Update",
	}
	noisySubstrings := []string{
		"CoreSimulator",
		"Cryptex",
		".timemachine",
		"com.apple.TimeMachine",
		"TimeMachineBackup",
	}
	for _, prefix := range noisyPrefixes {
		if strings.HasPrefix(mount, prefix) {
			return true
		}
	}
	for _, sub := range noisySubstrings {
		if strings.Contains(mount, sub) {
			return true
		}
	}
	return false
}

func updateBreakdown() {
	disks := GetDisks()

	foundTotal, foundBasic, foundOpport := getFoundationStorageBytes()

	var total, used, free, purgeable float64
	var systemUsed, dataUsed float64
	categories := []StorageCategory{}

	if foundTotal > 0 && foundOpport > 0 {

		const toGB = 1e9
		total = float64(foundTotal) / toGB
		purgeable = float64(foundOpport-foundBasic) / toGB
		if purgeable < 0 {
			purgeable = 0
		}

		free = float64(foundOpport) / toGB
		used = total - free

		for _, d := range disks {
			switch d.MountPoint {
			case "/":
				systemUsed = d.UsedGB
			case "/System/Volumes/Data":

				dataUsed = d.UsedGB - purgeable
				if dataUsed < 0 {
					dataUsed = 0
				}
			}
		}

		if systemUsed > 0 {
			categories = append(categories, StorageCategory{Name: "macOS", Size: systemUsed, Icon: "system"})
		}
		if dataUsed > 0 {
			categories = append(categories, StorageCategory{Name: "Data", Size: dataUsed, Icon: "apps"})
		}
		known := systemUsed + dataUsed
		if used > known+1.0 {
			categories = append(categories, StorageCategory{Name: "Other", Size: used - known, Icon: "doc"})
		}

		categories = append(categories, StorageCategory{Name: "Free", Size: free, Icon: "free"})
	} else {

		container, ok := getAPFSContainerInfo()
		if ok && container.TotalBytes > 0 {
			const toGB = 1e9
			total = float64(container.TotalBytes) / toGB
			purgeable = float64(container.PurgeableBytes) / toGB
			basicFreeBytes := float64(container.FreeBytes) / toGB
			free = basicFreeBytes + purgeable // opportunistic
			used = total - free
			for _, d := range disks {
				switch d.MountPoint {
				case "/":
					systemUsed = d.UsedGB
				case "/System/Volumes/Data":
					dataUsed = d.UsedGB - purgeable
					if dataUsed < 0 {
						dataUsed = 0
					}
				}
			}
			if systemUsed > 0 {
				categories = append(categories, StorageCategory{Name: "macOS", Size: systemUsed, Icon: "system"})
			}
			if dataUsed > 0 {
				categories = append(categories, StorageCategory{Name: "Data", Size: dataUsed, Icon: "apps"})
			}
			known := systemUsed + dataUsed
			if used > known+1.0 {
				categories = append(categories, StorageCategory{Name: "Other", Size: used - known, Icon: "doc"})
			}
			if purgeable > 0.5 {
				categories = append(categories, StorageCategory{Name: "Purgeable", Size: purgeable, Icon: "snapshot"})
			}
			categories = append(categories, StorageCategory{Name: "Free", Size: basicFreeBytes, Icon: "free"})
		} else {

			for _, d := range disks {
				if d.MountPoint == "/" {
					total = d.TotalGB
					free = d.FreeGB
					systemUsed = d.UsedGB
				} else if d.MountPoint == "/System/Volumes/Data" {
					dataUsed = d.UsedGB
				}
			}
			used = total - free
			if systemUsed > 0 {
				categories = append(categories, StorageCategory{Name: "macOS", Size: systemUsed, Icon: "system"})
			}
			if dataUsed > 0 {
				categories = append(categories, StorageCategory{Name: "Data", Size: dataUsed, Icon: "apps"})
			}
			known := systemUsed + dataUsed
			if used > known+1.0 {
				categories = append(categories, StorageCategory{Name: "Other", Size: used - known, Icon: "doc"})
			}
			categories = append(categories, StorageCategory{Name: "Free", Size: free, Icon: "free"})
		}
	}

	breakdown := StorageBreakdown{
		TotalGB:     total,
		UsedGB:      used,
		FreeGB:      free,
		PurgeableGB: purgeable,
		Categories:  categories,
	}

	breakdownMutex.Lock()
	cachedBreakdown = breakdown
	lastBreakdownUpdate = time.Now()
	breakdownPending = false
	breakdownMutex.Unlock()
}

func GetStorageBreakdown() StorageBreakdown {
	breakdownMutex.RLock()
	shouldUpdate := time.Since(lastBreakdownUpdate) > 5*time.Second
	breakdownMutex.RUnlock()

	if shouldUpdate {
		breakdownMutex.Lock()

		if time.Since(lastBreakdownUpdate) > 5*time.Second && !breakdownPending {
			breakdownPending = true
			go updateBreakdown()
		}
		breakdownMutex.Unlock()
	}

	breakdownMutex.RLock()
	defer breakdownMutex.RUnlock()
	return cachedBreakdown
}
