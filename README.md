# local-mesh

Zero-config LAN file and folder transfer via a terminal TUI.  
Discover peers automatically with mDNS/DNS-SD. Transfer files with SHA-256 integrity checking.

---

## Features

- **Automatic discovery** - peers appear within seconds via `_localmesh._tcp` mDNS
- **Interactive TUI** - built with Bubbletea (peer list → file picker → confirmation → progress)
- **Incoming-request overlay** - accept or reject offers interactively from any screen
- **Integrity verification** - SHA-256 checked before any file is committed
- **Folder transfers** - streamed as tar (no temp files on either end)
- **Safe naming** - never overwrites; appends `(1)`, `(2)`, … on collision
- **Cross-platform** - Windows, macOS, Linux

---

## Build

Requires Go ≥ 1.22.

```sh
go build -o local-mesh.exe ./cmd/local-mesh   # Windows
go build -o local-mesh     ./cmd/local-mesh   # Linux/macOS
```

Or via Make:

```sh
make build
```

---

## Run

```sh
./local-mesh.exe     # Windows
./local-mesh         # Linux/macOS
```

Log output goes to `local-mesh.log` in the current directory.

---

## Usage

| Key | Action |
|-----|--------|
| `↑`/`↓` or `j`/`k` | Move selection in peer list |
| `enter` | Select peer / confirm file |
| `r` | Refresh peer list |
| `s` | Select current directory as folder transfer |
| `esc` | Go back |
| `y` / `a` | Accept incoming transfer |
| `N` / `d` / `esc` | Reject incoming transfer |
| `c` | Cancel active transfer |
| `?` | Toggle help |
| `q` / `ctrl+c` | Quit |

---

## Manual verification (two terminals on the same machine)

```sh
# Terminal 1
./local-mesh.exe

# Terminal 2
./local-mesh.exe
```

Both instances should discover each other within ~5 seconds.  
Select the peer in one terminal → pick a file → confirm → the other terminal shows an overlay.

> **Note:** mDNS on loopback is restricted on some OSes. For reliable testing use two real machines on the same LAN, or two Docker containers on a user-defined bridge network.

---

## Docker two-container test

```sh
docker network create local-mesh-net

docker run --rm -it --network local-mesh-net \
  -v /path/to/file:/data/file \
  --name lm-a \
  local-mesh

docker run --rm -it --network local-mesh-net \
  --name lm-b \
  local-mesh
```

---

## Wire protocol

Custom length-prefixed binary framing over TCP:

```
[FrameType: 1 byte] [Length: 4 bytes uint32 BE] [Payload: Length bytes]
```

Control frames carry JSON; data frames carry raw file bytes. SHA-256 is computed end-to-end over the entire payload. See `pkg/protocol/` for the full spec.

---

## License

MIT
