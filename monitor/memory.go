package monitor

/*
#include <mach/mach.h>
#include <mach/mach_host.h>
#include <mach/vm_statistics.h>
*/
import "C"
import (
	"unsafe"

	"github.com/shirou/gopsutil/v4/mem"
)

type MemoryMetrics struct {
	TotalMB       uint64  `json:"total_mb"`
	UsedMB        uint64  `json:"used_mb"`
	FreeMB        uint64  `json:"free_mb"`
	WiredMB       uint64  `json:"wired_mb"`
	ActiveMB      uint64  `json:"active_mb"`
	InactiveMB    uint64  `json:"inactive_mb"`
	CompressedMB  uint64  `json:"compressed_mb"`
	PurgeableMB   uint64  `json:"purgeable_mb"`
	SwapTotalMB   uint64  `json:"swap_total_mb"`
	SwapUsedMB    uint64  `json:"swap_used_mb"`
	UsedPercent   float64 `json:"used_percent"`
	PressureLevel string  `json:"pressure_level"` // "Normal", "Warn", "Critical"
}

func vmStatsFromMach() (active, inactive, wired, free, compressed, purgeable uint64, ok bool) {
	var vmStat C.vm_statistics64_data_t
	count := C.mach_msg_type_number_t(C.HOST_VM_INFO64_COUNT)

	ret := C.host_statistics64(
		machHost,
		C.HOST_VM_INFO64,
		(*C.integer_t)(unsafe.Pointer(&vmStat)),
		&count,
	)

	if ret != C.KERN_SUCCESS {
		return 0, 0, 0, 0, 0, 0, false
	}

	pageSize := uint64(C.vm_kernel_page_size)
	active = uint64(vmStat.active_count) * pageSize
	inactive = uint64(vmStat.inactive_count) * pageSize
	wired = uint64(vmStat.wire_count) * pageSize
	free = uint64(vmStat.free_count) * pageSize
	compressed = uint64(vmStat.compressor_page_count) * pageSize
	purgeable = uint64(vmStat.purgeable_count) * pageSize
	return active, inactive, wired, free, compressed, purgeable, true
}

func GetMemory() MemoryMetrics {
	m := MemoryMetrics{
		PressureLevel: "Normal", // Default safe value
	}

	v, err := mem.VirtualMemory()
	if err == nil {
		m.TotalMB = v.Total / MB
		m.UsedMB = v.Used / MB
		m.UsedPercent = v.UsedPercent
	}

	active, inactive, wired, free, compressed, _, ok := vmStatsFromMach()
	if ok {
		m.ActiveMB = active / MB
		m.InactiveMB = inactive / MB
		m.WiredMB = wired / MB
		m.FreeMB = free / MB
		m.CompressedMB = compressed / MB
	}

	s, err := mem.SwapMemory()
	if err == nil {
		m.SwapTotalMB = s.Total / MB
		m.SwapUsedMB = s.Used / MB
	}

	return m
}
