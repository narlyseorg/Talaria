package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"golang.org/x/crypto/bcrypt"
	"github.com/fatih/color"

	"talaria/server"
)

func main() {
	var (
		noBrowser    = flag.Bool("no-browser", false, "Don't auto-open browser")
		configPath   = flag.String("config", "config.yml", "Path to config file")
		hashPassword = flag.String("hash-password", "", "Generate bcrypt hash for a password and exit")
		versionFlag  = flag.Bool("version", false, "Print version information and exit")
		vFlag        = flag.Bool("v", false, "Print version information and exit (shorthand)")
		silentFlag   = flag.Bool("silent", false, "Run Talaria in the background as a daemon")
		sFlag        = flag.Bool("s", false, "Run Talaria in the background as a daemon (shorthand)")
	)

	flag.Usage = func() {
		appleBlue := color.New(color.FgHiCyan, color.Bold)
		appleDim := color.New(color.FgHiBlack)
		appleKey := color.New(color.FgGreen)
		appleCode := color.New(color.FgHiWhite)

		fmt.Println()
		appleBlue.Println("  Talaria System Monitor")
		appleDim.Println("  An ultra-lightweight, cross-platform system monitoring dashboard.")
		fmt.Println()
		
		color.New(color.FgHiWhite, color.Bold).Println("  USAGE")
		fmt.Println("    talaria [flags]")
		fmt.Println()

		color.New(color.FgHiWhite, color.Bold).Println("  FLAGS")
		fmt.Printf("    %s   Path to the YAML configuration file (default: \"config.yml\")\n", appleKey.Sprint("-config <path>          "))
		fmt.Printf("    %s   Generate a secure bcrypt hash for a plaintext password\n", appleKey.Sprint("-hash-password <pwd>    "))
		fmt.Printf("    %s   Do not automatically launch the web dashboard\n", appleKey.Sprint("-no-browser             "))
		fmt.Printf("    %s   Run Talaria in the background as a daemon\n", appleKey.Sprint("-s, -silent             "))
		fmt.Printf("    %s   Print Talaria version and build information\n", appleKey.Sprint("-v, -version            "))
		fmt.Printf("    %s   Show this comprehensive help message\n", appleKey.Sprint("-h, -help               "))
		fmt.Println()

		color.New(color.FgHiWhite, color.Bold).Println("  EXAMPLES")
		appleDim.Println("    Start interactively (auto-generates config.yml on first run):")
		appleCode.Println("    $ ./talaria\n")

		appleDim.Println("    Run headless (for servers) with a custom config file:")
		appleCode.Println("    $ ./talaria -no-browser -config /etc/talaria/config.yml\n")

		appleDim.Println("    Safely generate a bcrypt hash to paste into config.yml:")
		appleCode.Println("    $ ./talaria -hash-password \"my_secret_password\"\n")
	}

	flag.Parse()

	if *silentFlag || *sFlag {
		if os.Getenv("TALARIA_BACKGROUND") != "1" {
			cmd := exec.Command(os.Args[0], os.Args[1:]...)
			cmd.Env = append(os.Environ(), "TALARIA_BACKGROUND=1")
			if err := cmd.Start(); err != nil {
				color.New(color.FgRed, color.Bold).Printf("\n  [FATAL] Failed to start Talaria in background: %v\n", err)
				os.Exit(1)
			}
			fmt.Println()
			color.New(color.FgGreen, color.Bold).Print("  [SUCCESS]")
			color.New(color.FgHiWhite).Print(" Talaria is now running in the background!\n")
			color.New(color.FgHiBlack).Printf("            PID: %d\n\n", cmd.Process.Pid)
			os.Exit(0)
		}
	}

	if *versionFlag || *vFlag {
		color.New(color.FgHiCyan, color.Bold).Println("\n  Talaria System Monitor")
		color.New(color.FgHiWhite).Println("  Version:  1.0.0")
		color.New(color.FgHiBlack).Printf("  OS/Arch:  %s/%s\n", runtime.GOOS, runtime.GOARCH)
		color.New(color.FgHiBlack).Printf("  Compiler: %s\n\n", runtime.Compiler)
		os.Exit(0)
	}

	if *hashPassword != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(*hashPassword), 12)
		if err != nil {
			color.New(color.FgRed, color.Bold).Printf("\n  [ERROR] Failed to hash password: %v\n", err)
			os.Exit(1)
		}
		color.New(color.FgGreen, color.Bold).Println("\n  [SUCCESS] Generated bcrypt hash:")
		color.New(color.FgHiBlack).Println("  Copy the string below and paste it into your config.yml\n")
		color.New(color.FgHiCyan).Println("  " + string(hash) + "\n")
		os.Exit(0)
	}

	if err := server.LoadConfig(*configPath); err != nil {
		color.New(color.FgRed, color.Bold).Printf("\n  [FATAL] Failed to load config from %s: %v\n", *configPath, err)
		os.Exit(1)
	}

	if server.GlobalConfig.Auth.PasswordHash == "" {
		pwd := server.GenerateRandomPassword()
		hash, _ := bcrypt.GenerateFromPassword([]byte(pwd), 12)
		server.GlobalConfig.Auth.PasswordHash = string(hash)
		color.New(color.FgHiYellow).Println("\n  [WARNING] No password_hash set in config!")
		fmt.Printf("  Generated random temporary password: ")
		color.New(color.FgHiCyan, color.Bold).Println(pwd + "\n")
	}

	server.SetPasswordHash(server.GlobalConfig.Auth.PasswordHash)

	addr := fmt.Sprintf("%s:%d", server.GlobalConfig.Server.Host, server.GlobalConfig.Server.Port)
	url := fmt.Sprintf("http://localhost:%d", server.GlobalConfig.Server.Port)

	hub := server.NewHub()
	go hub.Run()

	router := server.NewRouter(hub)

	srv := &http.Server{
		Addr:    addr,
		Handler: router,

		ReadHeaderTimeout: 5 * time.Second,
		ConnState: func(c net.Conn, state http.ConnState) {
			if state == http.StateNew {

				if tc, ok := c.(*net.TCPConn); ok {
					tc.SetLinger(0)
				}
			}
		},
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		fmt.Println()
		color.New(color.FgHiCyan, color.Bold).Println("  Talaria System Monitor")
		fmt.Println()
		
		fmt.Print("  ")
		color.New(color.FgHiBlack).Print("→")
		fmt.Print(" Running at ")
		color.New(color.FgHiBlue, color.Underline).Println(url)
		
		fmt.Print("  ")
		color.New(color.FgHiBlack).Print("→")
		fmt.Print(" Press ")
		color.New(color.FgHiWhite, color.Bold).Print("Ctrl+C")
		fmt.Println(" to stop")
		fmt.Println()

		ln, err := server.NewListener(addr)
		if err != nil {
			color.New(color.FgRed, color.Bold).Printf("  [FATAL] Server error: %v\n", err)
			os.Exit(1)
		}

		server.NotifyTelegramStart()

		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			color.New(color.FgRed, color.Bold).Printf("  [FATAL] Server error: %v\n", err)
			os.Exit(1)
		}
	}()

	if !*noBrowser {
		go func() {
			time.Sleep(300 * time.Millisecond)
			openBrowser(url)
		}()
	}

	<-stop
	fmt.Println()
	fmt.Print("  ")
	color.New(color.FgHiBlack).Print("→")
	color.New(color.FgHiWhite).Println(" Shutting down...")

	hub.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		color.New(color.FgRed, color.Bold).Printf("  [FATAL] Server forced to shutdown: %v\n", err)
		os.Exit(1)
	}

	fmt.Print("  ")
	color.New(color.FgHiBlack).Print("→")
	color.New(color.FgHiCyan, color.Bold).Println(" Bye!")
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		cmd = exec.Command("open", url)
	}
	if err := cmd.Start(); err == nil {

		go cmd.Wait()
	}
}
