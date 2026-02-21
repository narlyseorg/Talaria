<div align="center">

<img src="https://i.ibb.co.com/Y40WmDfy/talaria-removebg-preview.png" alt="Talaria Logo" width="120" />

<h1>Talaria System Monitor</h1>

**An ultra-lightweight system telemetry daemon forged exclusively for macOS.**

<p align="center">
  <img alt="macOS" src="https://img.shields.io/badge/Platform-macOS%20%7C%20Apple%20Silicon%20%26%20Intel-000000?style=for-the-badge&logo=apple&logoColor=white" />
  <img alt="Go Version" src="https://img.shields.io/badge/Go-%3E%3D1.20-00ADD8?style=for-the-badge&logo=go&logoColor=white" />
  <img alt="License" src="https://img.shields.io/badge/License-MIT-428f7e?style=for-the-badge&logo=open-source-initiative&logoColor=white" />
  <img alt="Dependencies" src="https://img.shields.io/badge/Dependencies-Zero-brightgreen?style=for-the-badge&logo=npm&logoColor=white" />
</p>

> *Built out of pure curiosity and a love for data visualization—a personal project to create a beautiful, native macOS system monitor without the heavy footprint of third-party wrappers.*

---

<img src="https://img.lightshot.app/aZiA_Ay-Rx6JbSmefyLLvw.png" alt="Dashboard Overview" width="75%" style="border-radius: 12px; box-shadow: 0 8px 24px rgba(0,0,0,0.15);" />

</div>

<br/>

## Why Talaria?

Talaria is not just another monitoring tool. It is a dependency-free daemon written purely in **Go**. By bypassing bloated wrappers, it hooks directly into macOS subsystems using native `syscall`, `CGO`, `pmset` and `powermetrics` to deliver real-time metrics natively.

All frontend assets (HTML, CSS, JS) are aggressively minified and embedded cryptographically into **a single compiled binary file**.

---

## Features

Since Talaria is built specifically for macOS, it avoids generic wrappers and interacts directly with the operating system's core utilities to extract metrics natively and accurately. Data streams aggressively at `250ms` intervals via websockets without heavily taxing the CPU.

### CPU & Core Diagnostics
Talaria tracks individual core activity bridging both Apple Silicon (P-Cores/E-Cores) and Intel architectures by heavily parsing raw `powermetrics` and `sysctl`. It actively accesses macOS SMC sensors to retrieve junction temperatures, alongside live tracking of running background applications via `ps` detailing PID, RAM%, CPU% and elapsed execution time.

### Memory & Swap Analysis
Instead of showing generic RAM limits, the daemon separates memory usage into Wired, Compressed, App Memory and freely available blocks using internal `vm_stat` outputs. This allows for deep evaluation of page-ins and page-outs to track heavy swap disk I/O, instantly translating macOS memory pressure states to the UI.

### Storage & APFS Telemetry
The daemon dynamically locates and parses all APFS data volumes and external mounts to map out capacity versus free space natively. Granular `KB/s` and `MB/s` read and write speeds are calculated by running diffs from `iostat` on an isolated background thread.

### Networking & Firewall
Active outbound connections and internal listening ports are continuously monitored using native `netstat` traces. Talaria also extracts localized wireless interface attributes such as the BSSID, RSSI/Noise, TxRate and checks the macOS native application firewall (`alf`) status.

### Power & Battery Metrics
By scraping `pmset -g batt` and `system_profiler SPPowerDataType`, the dashboard accurately computes precise charge capacity, cycle density, battery health condition and alternating current (AC) adapter states in real time.

### Remote Architecture
Beyond just reading data, Talaria ships with a fully featured pseudo-terminal (`xterm.js`) connected via authenticated WebSockets, enabling execution of raw `bash` or `zsh` commands natively on a headless rig. The entire UI—including its auto-syncing light/dark HTML and CSS theme engine—is embedded directly into the Go executable via `//go:embed`. There are zero Python servers or NPM dependencies required to run the final binary.

---

## Quick Start

> [!IMPORTANT]
> Because Talaria interfaces directly with root-level hardware sensors on macOS, you must have Go v1.20+ installed to compile it locally.

### 1. Clone & Build
Grab the source code and compile it instantly. The web assets will be bundled into the binary.

```bash
git clone https://github.com/narlyseorg/Talaria.git
cd Talaria
go mod tidy
go build -o talaria .
```

### 2. Interactive First Boot
Talaria securely stores your credentials (hashed using `bcrypt`) and Telegram integration tokens in a localized `config.yml`. It provides a gorgeous **interactive CLI wizard** to set this up automatically!

```bash
./talaria
```

---

## Advanced Usage

### Running in Daemon Mode (Background)

> [!TIP]
> If you are deploying Talaria on an always-on Mac Mini server, you don't need `screen` or `tmux`. Talaria natively forks itself into the background!

```bash
./talaria -silent

# Terminal Output:
# [SUCCESS] Talaria is now running in the background!
#           PID: 79622
```

To gracefully kill a detached background instance, simply run:
```bash
pkill talaria
```

### Command-Line Flags Reference

Run `./talaria --help` to explore all robust terminal flags available:

| Flag / Option | Description |
| :--- | :--- |
| <kbd>-config &lt;path&gt;</kbd> | Absolute or relative path to the YAML state file (default: `"config.yml"`). |
| <kbd>-hash-password &lt;pwd&gt;</kbd> | Standalone utility to securely generate and output a `bcrypt` hash string. |
| <kbd>-no-browser</kbd> | Prevents the application from launching your default OS browser hook. |
| <kbd>-s</kbd>, <kbd>-silent</kbd> | Detach from the TTY and run Talaria reliably in the OS background. |
| <kbd>-v</kbd>, <kbd>-version</kbd> | Print the localized version and compiler architecture (`darwin/arm64`). |
| <kbd>-h</kbd>, <kbd>-help</kbd> | Output the beautifully formatted documentation for syntax flags. |

---

## Security Architecture

> [!WARNING]
> Talaria is engineered to be exposed publicly (e.g., via Cloudflare Tunnels). Therefore, security is treated as a first-class citizen.

1. **Authentication:** All routes (Web and API) are secured behind a cryptographically signed cookie session. Passwords are never stored in plain text—they are serialized as $2a$12$ `bcrypt` hashes inside your `config.yml`.
2. **WebSocket Validation:** The internal `xterm.js` terminal connection strictly parses Origin headers and validates active session cookies before establishing a dual-directional bash pipe.
3. **No Database:** No databases, no external dependencies, no telemetry home-calling. Your data stays natively on your machine 100% of the time.

---

<div align="center">
  <p>Built for fun. Released under the <a href="LICENSE">MIT License</a>. Enjoy monitoring!</p>
</div>
