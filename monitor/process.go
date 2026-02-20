package monitor

import (
	"sort"
	"strings"
	"sync"

	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/process"
)

type ProcessInfo struct {
	PID    int     `json:"pid"`
	Name   string  `json:"name"`
	CPU    float64 `json:"cpu"`
	MemMB  float64 `json:"mem_mb"`
	MemPct float64 `json:"mem_percent"`
	User   string  `json:"user"`
}

type cachedProc struct {
	proc *process.Process
	name string
	user string
}

var (
	procCache   = make(map[int32]*cachedProc)
	procMutex   sync.Mutex    // guards procCache
	procExecMu  sync.Mutex    // serializes GetProcesses calls — prevents race on shared *process.Process objects
	cachedProcs []ProcessInfo // last successful result
)

func GetProcesses() []ProcessInfo {

	if !procExecMu.TryLock() {
		procMutex.Lock()
		result := cachedProcs
		procMutex.Unlock()
		return result
	}
	defer procExecMu.Unlock()

	pids, err := process.Pids()
	if err != nil {
		return nil
	}

	var totalMem uint64
	if v, err := mem.VirtualMemory(); err == nil {
		totalMem = v.Total
	}

	activePids := make(map[int32]bool, len(pids))
	for _, pid := range pids {
		activePids[pid] = true
	}

	procMutex.Lock()
	cacheSnapshot := make(map[int32]*cachedProc, len(procCache))
	for pid, cp := range procCache {
		cacheSnapshot[pid] = cp
	}
	procMutex.Unlock()

	var pInfos []ProcessInfo
	newEntries := make(map[int32]*cachedProc)
	for _, pid := range pids {
		r := processOnePID(pid, cacheSnapshot, totalMem)
		if r.pid != 0 {
			pInfos = append(pInfos, r.info)
			if r.isNew {
				newEntries[r.pid] = r.cp
			}
		}
	}

	procMutex.Lock()
	for pid, cp := range newEntries {
		procCache[pid] = cp
	}
	for pid := range procCache {
		if !activePids[pid] {
			delete(procCache, pid)
		}
	}
	cachedProcs = pInfos // store for concurrent-return path
	procMutex.Unlock()

	sort.Slice(pInfos, func(i, j int) bool {
		return pInfos[i].CPU > pInfos[j].CPU
	})

	if len(pInfos) > 25 {
		return pInfos[:25]
	}
	return pInfos
}

func processOnePID(pid int32, cacheSnapshot map[int32]*cachedProc, totalMem uint64) (ret struct {
	info  ProcessInfo
	pid   int32
	cp    *cachedProc
	isNew bool
}) {
	defer func() {
		if r := recover(); r != nil {

			ret = struct {
				info  ProcessInfo
				pid   int32
				cp    *cachedProc
				isNew bool
			}{}
		}
	}()

	type result = struct {
		info  ProcessInfo
		pid   int32
		cp    *cachedProc
		isNew bool
	}

	cp, exists := cacheSnapshot[pid]
	isNew := false

	if !exists {
		newP, err := process.NewProcess(pid)
		if err != nil {
			return result{}
		}

		name, _ := newP.Name()
		if name == "" {
			if cmd, err := newP.Cmdline(); err == nil && len(cmd) > 0 {
				parts := strings.Fields(cmd)
				if len(parts) > 0 {
					name = parts[0]
					if idx := strings.LastIndex(name, "/"); idx >= 0 {
						name = name[idx+1:]
					}
				}
			}
		}
		if name == "" {
			name = "unknown"
		}

		user, _ := newP.Username()

		if idx := strings.LastIndex(name, "/"); idx >= 0 {
			name = name[idx+1:]
		}

		cp = &cachedProc{
			proc: newP,
			name: name,
			user: user,
		}
		isNew = true
	}

	cpu, _ := cp.proc.Percent(0)

	memInfo, err := cp.proc.MemoryInfo()
	if err != nil {
		return result{}
	}

	var memPct float64
	if totalMem > 0 {
		memPct = float64(memInfo.RSS) / float64(totalMem) * 100.0
	}

	return result{
		info: ProcessInfo{
			PID:    int(pid),
			Name:   cp.name,
			CPU:    sanitizeFloat(cpu),
			MemMB:  sanitizeFloat(float64(memInfo.RSS) / float64(MB)),
			MemPct: sanitizeFloat(memPct),
			User:   cp.user,
		},
		pid:   pid,
		cp:    cp,
		isNew: isNew,
	}
}

func ResolveProcessName(pid int32) string {
	procMutex.Lock()
	cp, exists := procCache[pid]
	procMutex.Unlock()

	if exists {
		return cp.name
	}

	if p, err := process.NewProcess(pid); err == nil {
		name, _ := p.Name()
		if idx := strings.LastIndex(name, "/"); idx >= 0 {
			name = name[idx+1:]
		}
		return name
	}
	return ""
}
