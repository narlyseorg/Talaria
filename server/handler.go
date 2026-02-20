package server

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"talaria/monitor"
	"time"
)

//go:embed all:static
var staticFiles embed.FS

type AllMetrics struct {
	CPU          monitor.CPUMetrics          `json:"cpu"`
	Memory       monitor.MemoryMetrics       `json:"memory"`
	Disks        []monitor.DiskInfo          `json:"disks"`
	StorageBreak monitor.StorageBreakdown    `json:"storage_breakdown"`
	DiskIO       monitor.DiskIOMetrics       `json:"disk_io"`
	Network      monitor.NetworkMetrics      `json:"network"`
	Battery      monitor.BatteryMetrics      `json:"battery"`
	Processes    []monitor.ProcessInfo       `json:"processes"`
	System       monitor.SystemMetrics       `json:"system"`
	Thermal      monitor.ThermalMetrics      `json:"thermal"`
	GPU          monitor.GPUMetrics          `json:"gpu"`
	Security     monitor.SecurityMetrics     `json:"security"`
	Connect      monitor.ConnectivityMetrics `json:"connectivity"`
	Health       monitor.HealthMetrics       `json:"health"`
	Timestamp    int64                       `json:"timestamp"`
	ClientCount  int                         `json:"client_count"`
}

var (
	cachedHTTPMetrics     *AllMetrics
	cachedHTTPMetricsJSON []byte
	lastHTTPMetricsTime   time.Time
	httpMetricsMux        sync.Mutex
)

func safeGo(wg *sync.WaitGroup, fn func()) {
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Panic in background task: %v", r)
			}
		}()
		fn()
	}()
}

func CollectAll(clientCount int) *AllMetrics {
	m := &AllMetrics{}
	var wg sync.WaitGroup

	wg.Add(14)

	safeGo(&wg, func() { m.CPU = monitor.GetCPU() })
	safeGo(&wg, func() { m.Memory = monitor.GetMemory() })
	safeGo(&wg, func() { m.Disks = monitor.GetDisks() })
	safeGo(&wg, func() { m.StorageBreak = monitor.GetStorageBreakdown() })
	safeGo(&wg, func() { m.DiskIO = monitor.GetDiskIO() })
	safeGo(&wg, func() { m.Network = monitor.GetNetwork() })
	safeGo(&wg, func() { m.Battery = monitor.GetBattery() })
	safeGo(&wg, func() { m.Processes = monitor.GetProcesses() })
	safeGo(&wg, func() { m.System = monitor.GetSystem() })
	safeGo(&wg, func() { m.Thermal = monitor.GetThermal() })
	safeGo(&wg, func() { m.GPU = monitor.GetGPU() })
	safeGo(&wg, func() { m.Security = monitor.GetSecurity() })
	safeGo(&wg, func() { m.Connect = monitor.GetConnectivity() })
	safeGo(&wg, func() { m.Health = monitor.GetHealth() })

	wg.Wait()

	m.Timestamp = time.Now().UnixMilli()
	m.ClientCount = clientCount

	return m
}

func getCachedHTTPMetrics() []byte {
	httpMetricsMux.Lock()
	if time.Since(lastHTTPMetricsTime) < 500*time.Millisecond && cachedHTTPMetricsJSON != nil {
		data := cachedHTTPMetricsJSON
		httpMetricsMux.Unlock()
		return data
	}
	httpMetricsMux.Unlock()

	metrics := CollectAll(0)
	data, err := json.Marshal(metrics)
	if err != nil {
		log.Printf("Error encoding metrics: %v", err)
		return nil
	}

	httpMetricsMux.Lock()
	cachedHTTPMetrics = metrics
	cachedHTTPMetricsJSON = data
	lastHTTPMetricsTime = time.Now()
	httpMetricsMux.Unlock()

	return data
}

func handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	data := getCachedHTTPMetrics()
	if data == nil {
		http.Error(w, "Failed to collect metrics", http.StatusInternalServerError)
		return
	}
	w.Write(data)
}

func handleKill(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	pidStr := r.URL.Query().Get("pid")
	if pidStr == "" {
		http.Error(w, "Missing pid", http.StatusBadRequest)
		return
	}

	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		http.Error(w, "Invalid pid", http.StatusBadRequest)
		return
	}

	currentUID := os.Getuid()

	importPath := "github.com/shirou/gopsutil/v4/process"
	_ = importPath // Just to show we'd need it; actually monitor package already has it. We will use the standard library for basic checks or exec if gopsutil isn't directly imported here.

	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "uid=").Output()
	if err != nil || len(out) == 0 {
		http.Error(w, "Process not found or access denied", http.StatusNotFound)
		return
	}

	targetUIDStr := strings.TrimSpace(string(out))
	targetUID, err := strconv.Atoi(targetUIDStr)
	if err != nil {
		http.Error(w, "Failed to determine process ownership", http.StatusInternalServerError)
		return
	}

	if currentUID != 0 && targetUID != currentUID {
		log.Printf("Security Violation: Attempted to kill process %d owned by UID %d from Talaria running as UID %d", pid, targetUID, currentUID)
		http.Error(w, "Unauthorized: You can only kill your own processes", http.StatusForbidden)
		return
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		http.Error(w, "Process not found", http.StatusNotFound)
		return
	}

	if err := proc.Kill(); err != nil {
		http.Error(w, fmt.Sprintf("Failed to kill process: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Process %d killed", pid)
}

func handleExport(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=talaria-metrics-%d.json", time.Now().Unix()))

	data := getCachedHTTPMetrics()
	if data == nil {
		http.Error(w, "Failed to collect metrics", http.StatusInternalServerError)
		return
	}
	w.Write(data)
}

var (
	flushDNSMu       sync.Mutex
	lastFlushDNSTime time.Time
)

func handleFlushDNS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	flushDNSMu.Lock()
	if time.Since(lastFlushDNSTime) < 30*time.Second {
		flushDNSMu.Unlock()
		http.Error(w, "Rate limit exceeded. Please wait 30 seconds.", http.StatusTooManyRequests)
		return
	}
	lastFlushDNSTime = time.Now()
	flushDNSMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	script := `do shell script "dscacheutil -flushcache; killall -HUP mDNSResponder" with administrator privileges`
	out, err := exec.CommandContext(ctx, "osascript", "-e", script).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if strings.Contains(msg, "User canceled") || strings.Contains(err.Error(), "exit status 1") && msg == "" {
			http.Error(w, "User cancelled authentication", http.StatusUnauthorized)
		} else {
			http.Error(w, fmt.Sprintf("Failed to flush DNS: %s", msg), http.StatusInternalServerError)
		}
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "DNS cache flushed")
	log.Println("DNS cache flushed successfully")
}

func handleConnections(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	data := monitor.GetConnectionDetails()
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("Error encoding connections: %v", err)
	}
}

func RecoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("PANIC in HTTP handler: %v", err)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"error": "Internal Server Error",
				})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"theme": GlobalConfig.Server.Theme,
	})
}

func NewRouter(hub *Hub) http.Handler {

	protected := http.NewServeMux()

	protected.HandleFunc("/api/metrics", handleMetrics)
	protected.HandleFunc("/api/kill", handleKill)
	protected.HandleFunc("/api/export", handleExport)
	protected.HandleFunc("/api/flushdns", handleFlushDNS)
	protected.HandleFunc("/api/connections", handleConnections)
	protected.HandleFunc("/api/config", handleConfig)

	protected.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		ServeWs(hub, w, r)
	})

	protected.HandleFunc("/ws/terminal", ServeTerminal)

	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		log.Fatalf("Failed to create sub filesystem: %v", err)
	}
	protected.Handle("/", http.FileServer(http.FS(staticFS)))

	root := http.NewServeMux()
	root.HandleFunc("/api/login", handleLogin)
	root.HandleFunc("/api/logout", handleLogout)
	root.HandleFunc("/api/auth/check", handleAuthCheck)
	root.Handle("/", AuthMiddleware(protected))

	return RecoveryMiddleware(root)
}
