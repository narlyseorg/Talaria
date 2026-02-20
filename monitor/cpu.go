package monitor

/*
#include <mach/mach.h>
#include <mach/mach_host.h>
#include <mach/processor_info.h>
#include <mach/mach_error.h>
#include <mach/mach_vm.h>
*/
import "C"
import (
	"runtime"
	"strings"
	"sync"
	"unsafe"
)

type CPUMetrics struct {
	UsagePercent float64   `json:"usage_percent"`
	CoreCount    int       `json:"core_count"`
	PerCore      []float64 `json:"per_core"`
	Model        string    `json:"model"`
}

var (
	prevTicks   []C.processor_cpu_load_info_data_t
	prevPerCore []float64 // Component 8: reusable PerCore buffer
	cpuModel    string
	cpuMutex    sync.Mutex // Guards prevTicks to ensure thread safety
	machHost    C.host_t   // C1 fix: cached mach host port to avoid leak
)

func init() {

	out, err := RunCmdPlain("sysctl", "-n", "machdep.cpu.brand_string")
	if err == nil {
		cpuModel = strings.TrimSpace(string(out))
	}

	machHost = C.mach_host_self()
}

func GetCPU() CPUMetrics {
	m := CPUMetrics{
		CoreCount: runtime.NumCPU(),
		Model:     cpuModel,
	}

	var cpuCount C.natural_t
	var infoArray *C.int
	var infoCount C.mach_msg_type_number_t

	ret := C.host_processor_info(machHost, C.PROCESSOR_CPU_LOAD_INFO, &cpuCount, (*C.processor_info_array_t)(unsafe.Pointer(&infoArray)), &infoCount)
	if ret != C.KERN_SUCCESS {
		return m
	}

	cpuLoad := (*[1 << 30]C.processor_cpu_load_info_data_t)(unsafe.Pointer(infoArray))[:cpuCount:cpuCount]

	cpuMutex.Lock()
	defer cpuMutex.Unlock()

	if prevTicks == nil {
		prevTicks = make([]C.processor_cpu_load_info_data_t, cpuCount)
		copy(prevTicks, cpuLoad)

		C.vm_deallocate(C.mach_task_self_, C.vm_address_t(uintptr(unsafe.Pointer(infoArray))), C.vm_size_t(infoCount*C.sizeof_int))

		return m
	}

	var totalUsage float64

	if prevPerCore == nil || len(prevPerCore) != int(cpuCount) {
		prevPerCore = make([]float64, cpuCount)
	}
	for i := range prevPerCore {
		prevPerCore[i] = 0
	}

	for i := 0; i < int(cpuCount); i++ {
		curr := cpuLoad[i]
		prev := prevTicks[i]

		user := float64(curr.cpu_ticks[C.CPU_STATE_USER] - prev.cpu_ticks[C.CPU_STATE_USER])
		sys := float64(curr.cpu_ticks[C.CPU_STATE_SYSTEM] - prev.cpu_ticks[C.CPU_STATE_SYSTEM])
		idle := float64(curr.cpu_ticks[C.CPU_STATE_IDLE] - prev.cpu_ticks[C.CPU_STATE_IDLE])
		nice := float64(curr.cpu_ticks[C.CPU_STATE_NICE] - prev.cpu_ticks[C.CPU_STATE_NICE])

		total := user + sys + idle + nice
		if total > 0 {
			usage := (user + sys + nice) / total * 100.0
			prevPerCore[i] = usage
			totalUsage += usage
		}
	}

	m.PerCore = make([]float64, cpuCount)
	copy(m.PerCore, prevPerCore)

	if cpuCount > 0 {
		m.UsagePercent = totalUsage / float64(cpuCount)
	}

	copy(prevTicks, cpuLoad)

	C.vm_deallocate(C.mach_task_self_, C.vm_address_t(uintptr(unsafe.Pointer(infoArray))), C.vm_size_t(infoCount*C.sizeof_int))

	return m
}
