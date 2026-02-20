package server

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"
	"github.com/fatih/color"
)

type Config struct {
	Server struct {
		Host  string `yaml:"host"`
		Port  int    `yaml:"port"`
		Theme string `yaml:"theme"`
	} `yaml:"server"`

	Auth struct {
		PasswordHash string `yaml:"password_hash"`
	} `yaml:"auth"`

	Telegram struct {
		Enabled        bool   `yaml:"enabled"`
		BotToken       string `yaml:"bot_token"`
		ChatID         int64  `yaml:"chat_id"`
		StartupMessage string `yaml:"startup_message"`
	} `yaml:"telegram"`
}

var GlobalConfig *Config

func LoadConfig(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			appleBlue := color.New(color.FgHiCyan, color.Bold)
			appleDim := color.New(color.FgHiBlack)
			applePrompt := color.New(color.FgHiWhite, color.Bold)
			appleKey := color.New(color.FgHiBlue)

			fmt.Println()
			appleDim.Println("=============================================")
			appleBlue.Println("  Welcome to Talaria!")
			appleDim.Println("  It looks like this is your first time.")
			appleDim.Println("  Let's set up your config.yml file.")
			appleDim.Println("=============================================")
			fmt.Println()

			reader := bufio.NewReader(os.Stdin)

			applePrompt.Print("  Enter a new Passphrase: ")
			passBytes, _ := term.ReadPassword(int(syscall.Stdin))
			fmt.Println()
			passStr := strings.TrimSpace(string(passBytes))
			
			hash := ""
			if passStr != "" {
				h, err := bcrypt.GenerateFromPassword([]byte(passStr), 12)
				if err == nil {
					hash = string(h)
				}
			}

			fmt.Println()
			applePrompt.Print("\n  Do you want to enable Telegram Notifications? (y/N): ")
			tgEnableStr, _ := reader.ReadString('\n')
			tgEnableStr = strings.TrimSpace(strings.ToLower(tgEnableStr))
			tgEnabled := tgEnableStr == "y" || tgEnableStr == "yes"

			tgToken := "YOUR_BOT_TOKEN_HERE"
			var tgChatID int64 = 0

			if tgEnabled {
				appleKey.Print("    -> Enter Telegram Bot Token: ")
				token, _ := reader.ReadString('\n')
				tgToken = strings.TrimSpace(token)

				appleKey.Print("    -> Enter Telegram Chat ID ")
				appleDim.Print("(Leave blank to auto-detect later): ")
				chatStr, _ := reader.ReadString('\n')
				chatStr = strings.TrimSpace(chatStr)
				if chatStr != "" {
					parsed, err := strconv.ParseInt(chatStr, 10, 64)
					if err == nil {
						tgChatID = parsed
					}
				}
			}

			applePrompt.Print("\n  Select default theme ")
			appleDim.Print("(dark/light) [dark]: ")
			themeStr, _ := reader.ReadString('\n')
			themeStr = strings.TrimSpace(strings.ToLower(themeStr))
			if themeStr != "light" {
				themeStr = "dark"
			}

			// Generate default config
			defaultCfg := &Config{}
			defaultCfg.Server.Host = "0.0.0.0"
			defaultCfg.Server.Port = 8745
			defaultCfg.Server.Theme = themeStr
			defaultCfg.Auth.PasswordHash = hash
			defaultCfg.Telegram.Enabled = tgEnabled
			defaultCfg.Telegram.BotToken = tgToken
			defaultCfg.Telegram.ChatID = tgChatID
			defaultCfg.Telegram.StartupMessage = "[%s] Talaria is on Steroids 🔥"

			cfgData, _ := yaml.Marshal(defaultCfg)
			os.WriteFile(path, cfgData, 0600)
			
			GlobalConfig = defaultCfg
			fmt.Println()
			color.New(color.FgGreen, color.Bold).Printf("  [SUCCESS]")
			color.New(color.FgHiWhite).Printf(" Configuration saved to ")
			color.New(color.FgHiCyan, color.Bold).Printf("%s!\n\n", path)
			return nil
		}
		return err
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return err
	}

	GlobalConfig = cfg
	return nil
}
