package monitor

import (
	"context"
	"fmt"
	"log"
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type HealthMetrics struct {
	SIPEnabled       bool `json:"sip_enabled"`
	FileVaultEnabled bool `json:"filevault_enabled"`
	FirewallEnabled  bool `json:"firewall_enabled"`

	TimeMachineLastBackup string  `json:"tm_last_backup"`
	TimeMachineStatus     string  `json:"tm_status"`    // "Running", "Idle", "Error", "Unknown"
	TimeMachinePercent    float64 `json:"tm_percent"`   // backup progress 0-100 if running, -1 if not
	TimeMachineAgeMins    int     `json:"tm_age_mins"`  // minutes since last backup, -1 if never
	TimeMachineAgeLabel   string  `json:"tm_age_label"` // human-readable age: "2h 15m", "3d", etc.

	KernelErrorsLast5m int      `json:"kernel_errors_last_5m"`
	KernelLogs         []string `json:"kernel_logs"` // The actual log lines for transparency

	ErrorHistory []int `json:"error_history"` // Now tracks Kernel Errors only

	HealthScore int    `json:"health_score"` // 0-100 overall health
	ErrorTrend  string `json:"error_trend"`  // "rising", "stable", "falling"
}

const errorHistorySize = 30

var (
	cachedKernelErrors int
	cachedKernelLogs   []string // Store actual log lines
	errorHistory       []int
	lastErrorCheck     time.Time
	healthMutex        sync.Mutex

	cachedTMLastBackup string
	cachedTMStatus     string
	cachedTMPercent    float64
	cachedTMAgeMins    int
	cachedTMAgeLabel   string
	lastTMCheckTime    time.Time

	tmPercentRegex = regexp.MustCompile(`Percent\s*=\s*"?([0-9.]+)"?`)

	logLineRegex = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2}`)

	cachedSIPEnabled       bool
	cachedFileVaultEnabled bool

	cachedFirewallEnabled bool
	lastFirewallCheck     time.Time

	kernelPredicate string

	kernelErrorsPending bool
)

var kernelSignificantPatterns = []string{

	"kernel panic",                                 // The big one — always actionable
	"panic(",                                       // C-style panic() call in kexts
	"panic at ",                                    // Variant format
	"assertion failed", "Assert failed", "ASSERT(", // Kernel-level assertion
	"fatal error", // Generic fatal
	"FATAL:",
	"double fault", // CPU exception — always critical
	"triple fault", // CPU exception — always critical
	"NMI: ",        // Non-maskable interrupt

	"ECC error",     // RAM error correction
	"Machine Check", // MCA — hardware detected error
	"corrected hardware error",
	"memory corruption",
	"heap corruption",
	"stack overflow",
	"use after free",
	"out of bounds",

	"APFS: error", // APFS filesystem error
	"apfs_error",
	"I/O error on device",
	"disk I/O error",
	"media error",
	"filesystem error",
	"volume corruption",
	"journal error",

	"watchdog timeout",
	"watchdog detected",
	"unexpected reset",
	"system reset",
}

func init() {

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	out, err := RunCmd(ctx, "csrutil", "status")
	if err == nil && strings.Contains(strings.ToLower(string(out)), "enabled") {
		cachedSIPEnabled = true
	}

	ctx2, cancel2 := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel2()
	out2, err2 := RunCmd(ctx2, "fdesetup", "status")
	if err2 == nil && strings.Contains(strings.ToLower(string(out2)), "on") {
		cachedFileVaultEnabled = true
	}

	kernelPredicate = `process == "kernel" AND messageType == error`

	cachedTMPercent = -1
	cachedTMAgeMins = -1
}

func GetHealth() HealthMetrics {
	m := HealthMetrics{
		TimeMachinePercent: -1,
		TimeMachineAgeMins: -1,
	}

	checkSecurity(&m)

	healthMutex.Lock()
	now := time.Now()
	tmCacheValid := now.Sub(lastTMCheckTime) < 15*time.Second && lastTMCheckTime != (time.Time{})
	healthMutex.Unlock()

	if tmCacheValid {
		healthMutex.Lock()
		m.TimeMachineLastBackup = cachedTMLastBackup
		m.TimeMachineStatus = cachedTMStatus
		m.TimeMachinePercent = cachedTMPercent
		m.TimeMachineAgeMins = cachedTMAgeMins
		m.TimeMachineAgeLabel = cachedTMAgeLabel
		healthMutex.Unlock()
	} else {

		backupTime, parsed := checkTimeMachine(&m)

		healthMutex.Lock()
		cachedTMLastBackup = m.TimeMachineLastBackup
		cachedTMStatus = m.TimeMachineStatus
		cachedTMPercent = m.TimeMachinePercent
		cachedTMAgeMins = m.TimeMachineAgeMins
		cachedTMAgeLabel = m.TimeMachineAgeLabel
		lastTMCheckTime = now
		if parsed {
			_ = backupTime // stored for potential future use
		}
		healthMutex.Unlock()
	}

	healthMutex.Lock()
	if now.Sub(lastErrorCheck) > 60*time.Second && !kernelErrorsPending {

		kernelErrorsPending = true
		go updateKernelErrors()
	}
	m.KernelErrorsLast5m = cachedKernelErrors

	m.KernelLogs = make([]string, len(cachedKernelLogs))
	copy(m.KernelLogs, cachedKernelLogs)

	if len(errorHistory) == 0 {
		errorHistory = append(errorHistory, cachedKernelErrors)
	}
	m.ErrorHistory = make([]int, len(errorHistory))
	copy(m.ErrorHistory, errorHistory)
	healthMutex.Unlock()

	m.HealthScore = computeHealthScore(m)

	m.ErrorTrend = computeErrorTrend(m.ErrorHistory)

	return m
}

func checkSecurity(m *HealthMetrics) {
	m.SIPEnabled = cachedSIPEnabled
	m.FileVaultEnabled = cachedFileVaultEnabled

	healthMutex.Lock()
	now := time.Now()
	needRefresh := now.Sub(lastFirewallCheck) > 5*time.Second
	if !needRefresh {
		m.FirewallEnabled = cachedFirewallEnabled
	}
	healthMutex.Unlock()

	if needRefresh {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		out, err := RunCmd(ctx, "/usr/libexec/ApplicationFirewall/socketfilterfw", "--getglobalstate")
		enabled := false
		if err == nil {
			s := string(out)
			if strings.Contains(s, "State = 1") || strings.Contains(strings.ToLower(s), "enabled") {
				enabled = true
			}
		}
		healthMutex.Lock()
		cachedFirewallEnabled = enabled
		lastFirewallCheck = now
		healthMutex.Unlock()
		m.FirewallEnabled = enabled
	}
}

func checkTimeMachine(m *HealthMetrics) (backupTime time.Time, parsed bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	outStatus, err := RunCmd(ctx, "tmutil", "status")
	if err == nil {
		s := string(outStatus)
		if strings.Contains(s, "Running = 1") {
			m.TimeMachineStatus = "Running"
			if matches := tmPercentRegex.FindStringSubmatch(s); len(matches) > 1 {
				if pct, err := strconv.ParseFloat(matches[1], 64); err == nil {
					m.TimeMachinePercent = math.Round(pct*1000) / 10
				}
			}
		} else {
			m.TimeMachineStatus = "Idle"
		}
	} else {
		m.TimeMachineStatus = "Unknown"
	}

	ctx2, cancel2 := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel2()
	outLast, err2 := RunCmd(ctx2, "tmutil", "latestbackup")
	if err2 == nil {
		path := strings.TrimSpace(string(outLast))
		if path != "" {
			parts := strings.Split(path, "/")
			if len(parts) > 0 {
				last := parts[len(parts)-1]
				last = strings.Split(last, ".")[0]
				layout := "2006-01-02-150405"

				t, err := time.ParseInLocation(layout, last, time.Local)
				if err == nil {
					now := time.Now()
					if t.Year() == now.Year() && t.YearDay() == now.YearDay() {
						m.TimeMachineLastBackup = "Today " + t.Format("15:04")
					} else if t.Year() == now.Year() && t.YearDay() == now.YearDay()-1 {
						m.TimeMachineLastBackup = "Yesterday " + t.Format("15:04")
					} else {
						m.TimeMachineLastBackup = t.Format("2006-01-02 15:04")
					}
					age := now.Sub(t)
					m.TimeMachineAgeMins = int(age.Minutes())
					m.TimeMachineAgeLabel = formatDuration(age)

					return t, true
				} else {
					m.TimeMachineLastBackup = last
				}
			}
		} else {
			m.TimeMachineLastBackup = "Never"
		}
	} else {
		m.TimeMachineLastBackup = "Never"
	}

	return time.Time{}, false
}

func updateKernelErrors() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Panic in updateKernelErrors: %v", r)
			healthMutex.Lock()
			kernelErrorsPending = false
			healthMutex.Unlock()
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := fmt.Sprintf("log show --predicate '%s' --style compact --last 5m 2>/dev/null", kernelPredicate)
	out, err := RunCmd(ctx, "sh", "-c", cmd)

	var logs []string

	if err == nil {
		lines := strings.Split(string(out), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			if !logLineRegex.MatchString(line) {
				continue
			}

			if !isSignificantKernelError(line) {
				continue
			}

			logs = append(logs, line)
		}
	}

	count := len(logs)
	if count > 20 {
		logs = logs[count-20:]
	}

	healthMutex.Lock()
	cachedKernelErrors = count
	cachedKernelLogs = logs
	errorHistory = append(errorHistory, count)
	if len(errorHistory) > errorHistorySize {
		errorHistory = errorHistory[len(errorHistory)-errorHistorySize:]
	}
	lastErrorCheck = time.Now()
	kernelErrorsPending = false
	healthMutex.Unlock()
}

func isSignificantKernelError(line string) bool {
	lower := strings.ToLower(line)
	for _, pattern := range kernelSignificantPatterns {
		if strings.Contains(lower, strings.ToLower(pattern)) {
			return true
		}
	}
	return false
}

func computeHealthScore(m HealthMetrics) int {
	score := 100.0

	if !m.SIPEnabled {
		score -= 20 // Critical Security Fail
	}
	if !m.FileVaultEnabled {
		score -= 15 // Data at Rest Risk
	}
	if !m.FirewallEnabled {
		score -= 10 // Network Surface Risk
	}

	if m.TimeMachineLastBackup != "Never" {
		if m.TimeMachineAgeMins > 0 {
			switch {
			case m.TimeMachineAgeMins > 43200: // > 30 days (Neglected)
				score -= 30
			case m.TimeMachineAgeMins > 10080: // > 7 days (At Risk)
				score -= 15
			case m.TimeMachineAgeMins > 4320: // > 3 days (Warning)
				score -= 5
			}
		}
		if m.TimeMachineStatus == "Error" {
			score -= 15
		}
	}

	if m.KernelErrorsLast5m > 0 {
		penalty := float64(m.KernelErrorsLast5m) * 2.0
		if penalty > 40 {
			penalty = 40
		}
		score -= penalty
	}

	if score < 0 {
		score = 0
	}
	return int(math.Round(score))
}

func computeErrorTrend(history []int) string {
	if len(history) < 6 {
		return "stable"
	}

	n := len(history)
	recentSum := 0.0
	prevSum := 0.0
	for i := n - 3; i < n; i++ {
		recentSum += float64(history[i])
	}
	for i := n - 6; i < n-3; i++ {
		prevSum += float64(history[i])
	}

	recentAvg := recentSum / 3.0
	prevAvg := prevSum / 3.0

	if prevAvg == 0 && recentAvg == 0 {
		return "stable"
	}
	if prevAvg == 0 && recentAvg > 0 {
		return "rising"
	}

	change := (recentAvg - prevAvg) / math.Max(prevAvg, 1)
	if change > 0.25 {
		return "rising"
	}
	if change < -0.25 {
		return "falling"
	}
	return "stable"
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return "just now"
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60

	if days > 0 {
		if hours > 0 {
			return fmt.Sprintf("%dd %dh ago", days, hours)
		}
		return fmt.Sprintf("%dd ago", days)
	}
	if hours > 0 {
		if mins > 0 {
			return fmt.Sprintf("%dh %dm ago", hours, mins)
		}
		return fmt.Sprintf("%dh ago", hours)
	}
	return fmt.Sprintf("%dm ago", mins)
}
