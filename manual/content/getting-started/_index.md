---
title: "Getting Started"
weight: 10
---

## Requirements

- **Go 1.26+** (for building from source)
- **MQTT broker** (e.g., Mosquitto)
- **Linux** with SQLite support (CGO enabled for `mattn/go-sqlite3`)
- **Optional:** Home Assistant instance
- **Optional:** ESP or other MQTT-capable sensor/valve hardware

## Building from Source

Clone the repository and build:

```bash
git clone https://github.com/robinelvin/bedwetter.git
cd bedwetter
make build
```

This compiles Tailwind CSS and builds the Go binary. The resulting `bedwetter` binary is ready to run.

### Build Commands

| Command | Description |
|---------|-------------|
| `make build` | Compile Tailwind CSS and build Go binary |
| `make css` | Compile Tailwind CSS only |
| `make dev` | Live-reload development mode (requires [Air](https://github.com/air-verse/air)) |
| `make test` | Run all Go tests with coverage |
| `make clean` | Remove binary, database, and temp files |

## Initial Configuration

1. Copy the example configuration:

```bash
cp example-config.yaml config.yaml
```

2. Edit `config.yaml` with your MQTT broker details, zone definitions, and other settings. At minimum, configure:

   - **MQTT broker** connection details
   - **At least one zone** with sensor and valve topics
   - **Weather coordinates** (latitude/longitude) for forecast integration

See the [Configuration](../configuration/) section for the full reference.

## Running BedWetter

```bash
./bedwetter
```

By default, BedWetter reads `config.yaml` from the current directory and listens on port `8080`. To specify a different config file:

```bash
./bedwetter -config /path/to/config.yaml
```

## First-Time Setup

On first launch, open your browser to:

```
http://<your-host>:8080/setup
```

The setup wizard will prompt you to create an admin account. Until you do, the default credentials `admin` / `bedwetter` work at the login page.

After creating your admin account, you can:

- Add and configure zones via the web UI
- Set up watering schedules
- Configure alerts and notifications
- All settings are persisted in SQLite — the YAML file is only used for initial seeding

## Running as a Service

BedWetter runs as a foreground process. To run it as a background service, use systemd:

```ini
# /etc/systemd/system/bedwetter.service
[Unit]
Description=BedWetter Irrigation Controller
After=network.target mosquitto.service

[Service]
Type=simple
ExecStart=/opt/bedwetter/bedwetter -config /opt/bedwetter/config.yaml
WorkingDirectory=/opt/bedwetter
Restart=on-failure
RestartSec=10

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable bedwetter
sudo systemctl start bedwetter
```

## Cross-Compilation

Pre-built binaries are available for multiple platforms via GitHub releases:

- `linux/amd64`, `linux/arm64`, `linux/arm`
- `darwin/amd64`, `darwin/arm64`
- `windows/amd64`

To build for a specific platform:

```bash
CGO_ENABLED=0 go build -ldflags="-s -w" -o build/bedwetter .
```

{{% note %}}
When building with `CGO_ENABLED=0`, ensure your SQLite driver supports pure-Go mode or use static linking.
{{% /note %}}
