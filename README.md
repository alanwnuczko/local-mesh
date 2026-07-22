# local-mesh

local-mesh is a zero-configuration terminal UI for transferring files and folders across a local area network.

It relies on mDNS for automatic peer discovery and transfers data over raw TCP. Data integrity is guaranteed via end-to-end SHA-256 verification before any file is committed to disk. The application operates entirely within your local network boundary-requiring no accounts, cloud services, or external routing.

## Features

- **Automatic Peer Discovery:** Instantly discover other instances on the LAN via mDNS (`_localmesh._tcp`).
- **Terminal User Interface:** Fully interactive TUI built with [Bubbletea](https://github.com/charmbracelet/bubbletea).
- **Folder Streaming:** Directories are streamed as tar archives and extracted on the fly, avoiding intermediate disk writes.
- **Data Integrity:** SHA-256 hashes are computed and verified for all transfers.
- **Collision Protection:** Files are never overwritten; suffixes (e.g., `(1)`) are automatically appended to duplicate names.
- **Cross-Platform:** Native binaries for Windows, macOS, and Linux.

## Installation

Go 1.22 or newer is required to build from source.

### Option 1: Install via Go (Recommended)

This compiles the binary and places it in your Go environment's `bin` directory.

```sh
go install github.com/alanwnuczko/local-mesh/cmd/local-mesh@latest
```

Ensure your Go `bin` directory is in your system's `PATH`:
- **Windows:** `%USERPROFILE%\go\bin`
- **macOS / Linux:** `$HOME/go/bin`

### Option 2: Build from Source

```sh
git clone https://github.com/alanwnuczko/local-mesh.git
cd local-mesh
go build -o local-mesh ./cmd/local-mesh
```

Move the resulting binary to a location in your `PATH` (e.g., `/usr/local/bin` on Unix systems).

## Usage

Start the application by running the binary in your terminal:

```sh
local-mesh
```

### Keybindings

| Key | Action |
|-----|--------|
| `↑` / `↓` (or `j` / `k`) | Navigate lists |
| `Enter` | Select peer / Confirm transfer |
| `r` | Refresh peer list |
| `s` | Select current directory for folder transfer |
| `Esc` | Return to previous screen |
| `y` / `a` | Accept incoming transfer offer |
| `N` / `d` / `Esc` | Reject incoming transfer offer |
| `c` | Cancel active transfer |
| `?` | Toggle help menu |
| `q` / `Ctrl+C` | Quit |

### Default Directories

- **Received Files:** 
  - Windows: `%USERPROFILE%\Downloads\local-mesh`
  - macOS / Linux: `~/Downloads/local-mesh`
- **Application Logs:** Written to `local-mesh.log` in the current working directory.
- **Device ID Configuration:** 
  - Windows: `%APPDATA%\local-mesh`
  - macOS: `~/Library/Application Support/local-mesh`
  - Linux: `~/.config/local-mesh`

## Network Requirements

local-mesh requires a shared Layer 2 network segment (subnet) for mDNS multicast packets to reach peers.

### Windows Environments
- **Firewall:** You may need to run the application as Administrator on the first launch to automatically configure the Windows Firewall to allow inbound UDP traffic on port 5353.
- **Virtual Machines:** If running a VM (VMware, VirtualBox), the network adapter must be set to **Bridged** mode, or a dedicated **Host-Only** adapter must be added. Standard NAT adapters block multicast discovery.

## Protocol Architecture

local-mesh utilizes a custom length-prefixed binary protocol over TCP:

```text
[FrameType: 1 byte] [Length: uint32 BE] [Payload: Length bytes]
```

- **Control Frames:** JSON encoded (`Offer`, `Decision`, `Complete`, `Ack`, `Error`).
- **Data Frames:** Raw file bytes streamed in chunks (default 64 KiB).
- **Folder Transfers:** Pre-processed to calculate the total size and SHA-256 hash before streaming via tar.

See `pkg/protocol/` for implementation details.

## License

This project is licensed under the MIT License.
