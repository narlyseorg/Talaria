package monitor

import (
	"context"
	"strings"
	"sync"
	"time"
)

type SecurityMetrics struct {
	ScreenLocked bool          `json:"screen_locked"`
	SSHActive    bool          `json:"ssh_active"`
	UserSessions []SessionInfo `json:"user_sessions"`
	WakeHistory  []string      `json:"wake_history"` // Last 5 wake/sleep events
}

type SessionInfo struct {
	User     string `json:"user"`
	Terminal string `json:"terminal"`
	Host     string `json:"host"`
}

var (
	cachedWakeHistory   []string
	lastWakeHistoryTime time.Time
	secMutex            sync.Mutex

	cachedUserSessions []SessionInfo
	cachedSSHActive    bool
	lastSessionTime    time.Time
)

func GetSecurity() SecurityMetrics {
	m := SecurityMetrics{}

	m.ScreenLocked = IsScreenLocked()

	secMutex.Lock()
	now := time.Now()
	sessionCacheValid := now.Sub(lastSessionTime) < 5*time.Second && lastSessionTime != (time.Time{})
	if sessionCacheValid {
		m.UserSessions = cachedUserSessions
		m.SSHActive = cachedSSHActive
	}
	secMutex.Unlock()

	if !sessionCacheValid {

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		out, err := RunCmd(ctx, "who")
		if err == nil {
			lines := strings.Split(string(out), "\n")
			for _, line := range lines {
				parts := strings.Fields(line)
				if len(parts) >= 2 {
					s := SessionInfo{
						User:     parts[0],
						Terminal: parts[1],
					}

					if len(parts) >= 5 {
						lastField := parts[len(parts)-1]
						if strings.HasPrefix(lastField, "(") && strings.HasSuffix(lastField, ")") {
							s.Host = strings.Trim(lastField, "()")
						}
					}
					m.UserSessions = append(m.UserSessions, s)

					if strings.Contains(s.Terminal, "pts") || s.Host != "" {
						m.SSHActive = true
					}
				}
			}
		}

		secMutex.Lock()
		cachedUserSessions = m.UserSessions
		cachedSSHActive = m.SSHActive
		lastSessionTime = now
		secMutex.Unlock()
	}

	secMutex.Lock()
	if now.Sub(lastWakeHistoryTime) > 60*time.Second {
		go updateWakeHistory()
		lastWakeHistoryTime = now
	}
	m.WakeHistory = cachedWakeHistory
	secMutex.Unlock()

	return m
}

func updateWakeHistory() {

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	out, err := RunCmd(ctx, "sh", "-c",
		`pmset -g log | grep -E '^\d{4}-\d{2}-\d{2} .+\+\d{4} (Wake|Sleep|DarkWake) ' | tail -n 10`)
	if err != nil {
		return
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var events []string

	count := 0
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 4 {
			continue
		}

		timestamp := parts[0] + " " + parts[1]
		eventType := parts[3]

		detail := ""
		if len(parts) > 4 {
			detail = strings.Join(parts[4:], " ")

			if len(detail) > 60 {
				detail = detail[:57] + "..."
			}
		}

		clean := timestamp + " " + eventType
		if detail != "" {
			clean += " — " + detail
		}
		events = append(events, clean)
		count++
		if count >= 5 {
			break
		}
	}

	secMutex.Lock()
	cachedWakeHistory = events
	secMutex.Unlock()
}
