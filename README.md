# OpenVPN Prometheus Exporter (Go)

A Go-based HTTP API server that exposes OpenVPN metrics in Prometheus format.

## Features

- Parses OpenVPN status files (v2 and v3 formats)
- Exposes metrics via HTTP `/metrics` endpoint
- Per-client byte transfer statistics
- Server queue length monitoring
- Status file age tracking
- Performance metrics
- Health check endpoint

## Building

```bash
cd /path/to/Telegraf
go build -o openvpn-exporter openvpn-exporter.go
```

Or cross-compile for Linux:

```bash
GOOS=linux GOARCH=amd64 go build -o openvpn-exporter openvpn-exporter.go
```

## Usage

### Basic usage (prints to stdout)

```bash
OPENVPN_STATUS_FILE="/var/log/openvpn/openvpn-status.log" ./openvpn-exporter
```

### Custom port and address

```bash
OPENVPN_STATUS_FILE="/var/log/openvpn/openvpn-status.log" \
PORT=9101 \
LISTEN_ADDR=0.0.0.0 \
./openvpn-exporter
```

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `OPENVPN_STATUS_FILE` | `/var/log/openvpn/openvpn-status.log` | Path to OpenVPN status log |
| `PORT` | `8080` | HTTP server port |
| `LISTEN_ADDR` | `0.0.0.0` | Listen address for HTTP server |

## Endpoints

- `GET /metrics` - Prometheus metrics in text format
- `GET /health` - JSON health check
- `GET /` - Endpoint documentation

## Metrics Exported

### Core

- `openvpn_exporter_info{version}` - Exporter version
- `openvpn_up` - Status file availability (1=available, 0=error)

### Clients

- `openvpn_connected_clients` - Number of connected clients
- `openvpn_client_bytes_received{common_name}` - Bytes received per client
- `openvpn_client_bytes_sent{common_name}` - Bytes sent per client
- `openvpn_client_connected_since_timestamp{common_name}` - Connection time per client

### Server

- `openvpn_server_max_bcast_mcast_queue_length` - Max broadcast/multicast queue length

### File

- `openvpn_status_file_age_seconds` - Age of status file in seconds

### Exporter

- `openvpn_exporter_duration_seconds` - Time to collect metrics
- `openvpn_exporter_last_run_timestamp` - Last collection time

## Example Response

```
# HELP openvpn_exporter_info Exporter version information
# TYPE openvpn_exporter_info gauge
openvpn_exporter_info{version="1.0"} 1
# HELP openvpn_up OpenVPN status file reachability (1=readable, 0=not found)
# TYPE openvpn_up gauge
openvpn_up 1
# HELP openvpn_connected_clients Number of currently connected clients
# TYPE openvpn_connected_clients gauge
openvpn_connected_clients 2
# HELP openvpn_client_bytes_received Bytes received from client
# TYPE openvpn_client_bytes_received gauge
openvpn_client_bytes_received{common_name="client1"} 1024000
openvpn_client_bytes_received{common_name="client2"} 2048000
...
```

## Prometheus Configuration

Add to your `prometheus.yml`:

```yaml
scrape_configs:
  - job_name: 'openvpn'
    static_configs:
      - targets: ['localhost:8080']
```

## Docker Usage

Build a Docker image:

```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY openvpn-exporter.go .
COPY go.mod .
RUN go build -o openvpn-exporter openvpn-exporter.go

FROM alpine:latest
RUN apk add --no-cache ca-certificates
COPY --from=builder /app/openvpn-exporter /usr/local/bin/
EXPOSE 8080
ENV OPENVPN_STATUS_FILE=/var/log/openvpn/openvpn-status.log
CMD ["openvpn-exporter"]
```

## License

MIT
