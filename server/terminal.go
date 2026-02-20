package server

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

var termUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
}

type termMsg struct {
	Type string `json:"type"`           // "input", "resize", "output", "exit"
	Data string `json:"data,omitempty"` // payload
	Cols int    `json:"cols,omitempty"` // for resize
	Rows int    `json:"rows,omitempty"` // for resize
}

func ServeTerminal(w http.ResponseWriter, r *http.Request) {
	conn, err := termUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Terminal WS upgrade error: %v", err)
		return
	}

	shell := os.Getenv("SHELL")
	if shell != "" {
		if _, err := exec.LookPath(shell); err != nil {
			shell = ""
		}
	}
	if shell == "" {
		if path, err := exec.LookPath("/bin/bash"); err == nil {
			shell = path
		} else if path, err := exec.LookPath("/bin/sh"); err == nil {
			shell = path
		} else {
			shell = "/bin/zsh" // Fallback
		}
	}

	cmd := exec.Command(shell, "-l")
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"LANG=en_US.UTF-8",
	)

	ptmx, err := pty.Start(cmd)
	if err != nil {
		log.Printf("PTY start error: %v", err)
		conn.WriteJSON(termMsg{Type: "exit", Data: "Failed to start shell: " + err.Error()})
		conn.Close()
		return
	}

	var closeOnce sync.Once
	cleanup := func() {
		closeOnce.Do(func() {
			ptmx.Close()
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			conn.Close()
		})
	}
	defer cleanup()

	_ = pty.Setsize(ptmx, &pty.Winsize{Rows: 24, Cols: 80})

	const promptCmd = "export PS1='\\[\\e[32m\\]\\u@\\h:\\[\\e[34m\\]\\w\\[\\e[0m\\]\\$ '; export PROMPT='%F{green}%n@%m:%F{blue}%~%f%(#.#.$) '; clear\n"
	_, _ = ptmx.Write([]byte(promptCmd))

	sendCh := make(chan termMsg, 64)

	go func() {
		defer cleanup()
		ticker := time.NewTicker(pingPeriod)
		defer ticker.Stop()

		for {
			select {
			case msg, ok := <-sendCh:
				if !ok {
					return
				}
				conn.SetWriteDeadline(time.Now().Add(writeWait))
				if err := conn.WriteJSON(msg); err != nil {
					return
				}
			case <-ticker.C:
				conn.SetWriteDeadline(time.Now().Add(writeWait))
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					return
				}
			}
		}
	}()

	go func() {
		defer func() {
			close(sendCh) // Stop the writer
		}()
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if err != nil {

				sendCh <- termMsg{Type: "exit", Data: "Shell exited"}
				return
			}
			if n > 0 {

				sendCh <- termMsg{Type: "output", Data: string(buf[:n])}
			}
		}
	}()

	conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error { conn.SetReadDeadline(time.Now().Add(pongWait)); return nil })

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			break
		}

		var msg termMsg
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}

		switch msg.Type {
		case "input":
			if _, err := ptmx.Write([]byte(msg.Data)); err != nil {
				return
			}
		case "resize":
			if msg.Cols > 0 && msg.Rows > 0 {
				_ = pty.Setsize(ptmx, &pty.Winsize{
					Rows: uint16(msg.Rows),
					Cols: uint16(msg.Cols),
				})
			}
		}
	}
}
