package monitor

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type GPUMetrics struct {
	Utilization  int    `json:"utilization"`   // Device Utilization %
	RendererUtil int    `json:"renderer_util"` // Renderer Utilization %
	TilerUtil    int    `json:"tiler_util"`    // Tiler Utilization %
	VRAMUsedMB   uint64 `json:"vram_used_mb"`  // In use system memory
	VRAMAllocMB  uint64 `json:"vram_alloc_mb"` // Alloc system memory
	Model        string `json:"model"`         // e.g. "Apple M1"
	CoreCount    int    `json:"core_count"`    // gpu-core-count
}

var (
	reDeviceUtil   = regexp.MustCompile(`"Device Utilization %"=(\d+)`)
	reRendererUtil = regexp.MustCompile(`"Renderer Utilization %"=(\d+)`)
	reTilerUtil    = regexp.MustCompile(`"Tiler Utilization %"=(\d+)`)
	reInUseMem     = regexp.MustCompile(`"In use system memory"=(\d+)`)
	reAllocMem     = regexp.MustCompile(`"Alloc system memory"=(\d+)`)
	reGPUModel     = regexp.MustCompile(`"model"\s*=\s*"([^"]+)"`)
	reGPUCores     = regexp.MustCompile(`"gpu-core-count"\s*=\s*(\d+)`)

	gpuCache = NewCachedValue[GPUMetrics](2 * time.Second)
)

func GetGPU() GPUMetrics {
	return gpuCache.Get(fetchGPU)
}

func fetchGPU() GPUMetrics {
	m := GPUMetrics{}

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	out, err := RunCmd(ctx, "ioreg", "-r", "-d", "1", "-w", "0", "-c", "IOAccelerator")
	if err != nil {
		return m
	}

	data := string(out)

	if match := reDeviceUtil.FindStringSubmatch(data); len(match) > 1 {
		m.Utilization, _ = strconv.Atoi(match[1])
	}
	if match := reRendererUtil.FindStringSubmatch(data); len(match) > 1 {
		m.RendererUtil, _ = strconv.Atoi(match[1])
	}
	if match := reTilerUtil.FindStringSubmatch(data); len(match) > 1 {
		m.TilerUtil, _ = strconv.Atoi(match[1])
	}
	if match := reInUseMem.FindStringSubmatch(data); len(match) > 1 {
		bytes, _ := strconv.ParseUint(match[1], 10, 64)
		m.VRAMUsedMB = bytes / uint64(MB)
	}
	if match := reAllocMem.FindStringSubmatch(data); len(match) > 1 {
		bytes, _ := strconv.ParseUint(match[1], 10, 64)
		m.VRAMAllocMB = bytes / uint64(MB)
	}

	if match := reGPUModel.FindStringSubmatch(data); len(match) > 1 {
		m.Model = strings.TrimSpace(match[1])
	}
	if match := reGPUCores.FindStringSubmatch(data); len(match) > 1 {
		m.CoreCount, _ = strconv.Atoi(match[1])
	}

	return m
}
