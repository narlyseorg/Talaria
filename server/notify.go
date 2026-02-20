package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/fatih/color"
)

func telegramGetChatID(token string) (int64, error) {
	resp, err := http.Get(fmt.Sprintf("https://api.telegram.org/bot%s/getUpdates?limit=1&offset=-1", token))
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var result struct {
		OK     bool `json:"ok"`
		Result []struct {
			Message struct {
				Chat struct {
					ID int64 `json:"id"`
				} `json:"chat"`
			} `json:"message"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}
	if !result.OK || len(result.Result) == 0 {
		return 0, fmt.Errorf("no messages found — send /start to @TalariaMonitorBot first")
	}
	return result.Result[0].Message.Chat.ID, nil
}

func telegramSend(token string, chatID int64, text string, localURL string, publicURL string) error {
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)

	form := url.Values{
		"chat_id":    {fmt.Sprintf("%d", chatID)},
		"text":       {text},
		"parse_mode": {"HTML"},
	}

	buttons := []map[string]string{}
	if publicURL != "" {
		buttons = append(buttons, map[string]string{"text": "PUBLIC", "url": publicURL})
	}
	if localURL != "" {
		buttons = append(buttons, map[string]string{"text": "LOCAL", "url": localURL})
	}

	if len(buttons) > 0 {
		replyMarkup := map[string]interface{}{
			"inline_keyboard": [][]map[string]string{buttons},
		}
		replyMarkupBytes, _ := json.Marshal(replyMarkup)
		form.Set("reply_markup", string(replyMarkupBytes))
	}

	resp, err := http.PostForm(apiURL, form)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram API error: %s", resp.Status)
	}

	return nil
}

func getLocalIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "127.0.0.1"
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String()
}

func NotifyTelegramStart() {
	if !GlobalConfig.Telegram.Enabled {
		return
	}

	go func() {
		chatID := GlobalConfig.Telegram.ChatID
		// Automatically fetch Chat ID if enabled but not configured
		if chatID == 0 {
			fetchedID, err := telegramGetChatID(GlobalConfig.Telegram.BotToken)
			if err != nil {
				color.New(color.FgYellow).Printf("  [TELEGRAM] System notify skipped: %v\n", err)
				return
			}
			chatID = fetchedID
			fmt.Print("  ")
			color.New(color.FgHiCyan, color.Bold).Print("[TELEGRAM]")
			color.New(color.FgHiBlack).Printf(" Chat ID automatically resolved to: ")
			color.New(color.FgGreen).Printf("%d\n", chatID)
			color.New(color.FgHiBlack).Printf("             Please save this in config.yml for next time.\n")
		}

		port := GlobalConfig.Server.Port
		ip := getLocalIP()
		localURL := fmt.Sprintf("http://%s:%d", ip, port)
		now := time.Now().Format("02/01/2006 15:04")

		exec.Command("pkill", "-f", fmt.Sprintf("cloudflared tunnel --url http://localhost:%d", port)).Run()

		cmd := exec.Command("cloudflared", "tunnel", "--url", fmt.Sprintf("http://localhost:%d", port))
		stderr, err := cmd.StderrPipe()

		publicURL := ""
		if err == nil {
			if err := cmd.Start(); err == nil {

				urlChan := make(chan string, 1)
				go func() {
					scanner := bufio.NewScanner(stderr)
					re := regexp.MustCompile(`https://[a-zA-Z0-9-]+\.trycloudflare\.com`)
					for scanner.Scan() {
						line := scanner.Text()
						if match := re.FindString(line); match != "" {
							urlChan <- match
							break
						}
					}
				}()

				select {
				case publicURL = <-urlChan:

				case <-time.After(15 * time.Second):

				}
			}
		}

		msgTemplate := GlobalConfig.Telegram.StartupMessage
		if msgTemplate == "" {
			msgTemplate = "[%s] Talaria is on Steroids 🔥"
		}

		verbCount := strings.Count(msgTemplate, "%s")
		var msg string
		if verbCount >= 3 {
			msg = fmt.Sprintf(msgTemplate, now, publicURL, localURL)
		} else if verbCount == 1 {
			msg = fmt.Sprintf(msgTemplate, now)
		} else {
			msg = msgTemplate
		}

		if err := telegramSend(GlobalConfig.Telegram.BotToken, chatID, msg, localURL, publicURL); err != nil {
			log.Printf("Telegram notify failed: %v", err)
		}
	}()
}
