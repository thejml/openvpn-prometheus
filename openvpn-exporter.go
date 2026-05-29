package main

import (
	"bufio"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	version = "1.0"
)

type MetricValue struct {
	Help   string
	Type   string
	Value  string
	Labels string
}

type OpenVPNExporter struct {
	statusFile  string
	metrics     map[string][]MetricValue
	metricOrder []string // Preserve insertion order
	mu          sync.RWMutex
	lastRun     time.Time
	duration    time.Duration
}

func NewOpenVPNExporter(statusFile string) *OpenVPNExporter {
	return &OpenVPNExporter{
		statusFile:  statusFile,
		metrics:     make(map[string][]MetricValue),
		metricOrder: make([]string, 0),
	}
}

// formatMetrics converts the internal metrics map to Prometheus format
func (e *OpenVPNExporter) formatMetrics() string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var output strings.Builder
	seen := make(map[string]bool)

	// Iterate in insertion order
	for _, name := range e.metricOrder {
		values, exists := e.metrics[name]
		if !exists || len(values) == 0 {
			continue
		}

		// Write HELP and TYPE only once per metric
		if !seen[name] {
			first := values[0]
			output.WriteString(fmt.Sprintf("# HELP %s %s\n", name, first.Help))
			output.WriteString(fmt.Sprintf("# TYPE %s %s\n", name, first.Type))
			seen[name] = true
		}

		// Write all metric values
		for _, v := range values {
			if v.Labels != "" {
				output.WriteString(fmt.Sprintf("%s{%s} %s\n", name, v.Labels, v.Value))
			} else {
				output.WriteString(fmt.Sprintf("%s %s\n", name, v.Value))
			}
		}
	}

	return output.String()
}

// addMetric adds a metric to the internal map
func (e *OpenVPNExporter) addMetric(name, metricType, help, value, labels string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if _, exists := e.metrics[name]; !exists {
		e.metrics[name] = []MetricValue{}
		e.metricOrder = append(e.metricOrder, name)
	}

	e.metrics[name] = append(e.metrics[name], MetricValue{
		Help:   help,
		Type:   metricType,
		Value:  value,
		Labels: labels,
	})
}

// clearMetrics clears all collected metrics
func (e *OpenVPNExporter) clearMetrics() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.metrics = make(map[string][]MetricValue)
	e.metricOrder = make([]string, 0)
}

// detectStatusFormat determines if the status file is v2 or v3 format
func (e *OpenVPNExporter) detectStatusFormat() (string, error) {
	file, err := os.Open(e.statusFile)
	if err != nil {
		return "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	if scanner.Scan() {
		firstLine := scanner.Text()
		if strings.Contains(firstLine, "TITLE") {
			return "v3", nil
		}
	}
	return "v2", nil
}

// parseDateTime converts OpenVPN date format to Unix timestamp
func parseDateTime(dateStr string) string {
	// Try parsing common formats
	formats := []string{
		"Mon Jan 02 15:04:05 2006",
		"2006-01-02 15:04:05",
		"Mon Jan 2 15:04:05 2006",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, dateStr); err == nil {
			return strconv.FormatInt(t.Unix(), 10)
		}
	}

	return "0"
}

// collectStatusV2 parses OpenVPN v2 format status file
func (e *OpenVPNExporter) collectStatusV2() error {
	file, err := os.Open(e.statusFile)
	if err != nil {
		return err
	}
	defer file.Close()

	var section string
	var clientCount int
	var maxQueue string

	scanner := bufio.NewScanner(file)
	isFirstClient := true

	for scanner.Scan() {
		line := scanner.Text()
		line = strings.TrimSpace(line)

		if line == "" {
			continue
		}

		// Detect section headers
		if line == "OpenVPN CLIENT LIST" {
			section = "clients"
			continue
		} else if line == "ROUTING TABLE" {
			section = "routing"
			continue
		} else if line == "GLOBAL STATS" {
			section = "global"
			continue
		} else if line == "END" {
			section = ""
			continue
		}

		// Skip header lines
		if strings.HasPrefix(line, "Common Name,") || strings.HasPrefix(line, "Virtual Address,") || strings.HasPrefix(line, "Updated,") {
			continue
		}

		switch section {
		case "clients":
			parts := strings.Split(line, ",")
			if len(parts) < 5 {
				continue
			}

			cn := strings.TrimSpace(parts[0])
			bytesRecv := strings.TrimSpace(parts[2])
			bytesSent := strings.TrimSpace(parts[3])
			connectedSince := strings.TrimSpace(parts[4])

			if cn == "" || cn == "Common Name" {
				continue
			}

			clientCount++

			// Convert bytes to int to validate
			if _, err := strconv.ParseInt(bytesRecv, 10, 64); err != nil {
				continue
			}
			if _, err := strconv.ParseInt(bytesSent, 10, 64); err != nil {
				continue
			}

			labels := fmt.Sprintf(`common_name="%s"`, cn)

			if isFirstClient {
				e.addMetric("openvpn_client_bytes_received", "gauge", "Bytes received from client", bytesRecv, labels)
				e.addMetric("openvpn_client_bytes_sent", "gauge", "Bytes sent to client", bytesSent, labels)

				sinceTs := parseDateTime(connectedSince)
				e.addMetric("openvpn_client_connected_since_timestamp", "gauge", "Unix timestamp when client connected", sinceTs, labels)

				isFirstClient = false
			} else {
				e.mu.Lock()
				e.metrics["openvpn_client_bytes_received"] = append(e.metrics["openvpn_client_bytes_received"], MetricValue{
					Help:   "Bytes received from client",
					Type:   "gauge",
					Value:  bytesRecv,
					Labels: labels,
				})
				e.metrics["openvpn_client_bytes_sent"] = append(e.metrics["openvpn_client_bytes_sent"], MetricValue{
					Help:   "Bytes sent to client",
					Type:   "gauge",
					Value:  bytesSent,
					Labels: labels,
				})
				sinceTs := parseDateTime(connectedSince)
				e.metrics["openvpn_client_connected_since_timestamp"] = append(e.metrics["openvpn_client_connected_since_timestamp"], MetricValue{
					Help:   "Unix timestamp when client connected",
					Type:   "gauge",
					Value:  sinceTs,
					Labels: labels,
				})
				e.mu.Unlock()
			}

		case "global":
			if strings.Contains(line, "Max bcast/mcast queue length") {
				parts := strings.Split(line, ",")
				if len(parts) == 2 {
					maxQueue = strings.TrimSpace(parts[1])
				}
			}
		}
	}

	e.addMetric("openvpn_connected_clients", "gauge", "Number of currently connected clients", strconv.Itoa(clientCount), "")

	if maxQueue == "" {
		maxQueue = "0"
	}
	e.addMetric("openvpn_server_max_bcast_mcast_queue_length", "gauge", "Maximum broadcast/multicast queue length", maxQueue, "")

	return scanner.Err()
}

// collectStatusV3 parses OpenVPN v3 format status file
func (e *OpenVPNExporter) collectStatusV3() error {
	file, err := os.Open(e.statusFile)
	if err != nil {
		return err
	}
	defer file.Close()

	var clientCount int
	var maxQueue string
	isFirstClient := true

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		line = strings.TrimSpace(line)

		if line == "" {
			continue
		}

		fields := strings.Split(line, "\t")
		if len(fields) == 0 {
			continue
		}

		lineType := fields[0]

		switch lineType {
		case "TITLE", "TIME", "HEADER":
			continue

		case "CLIENT_LIST":
			if len(fields) < 9 {
				continue
			}

			cn := fields[1]
			bytesRecv := fields[5]
			bytesSent := fields[6]
			connectedSince := fields[7]
			connectedSinceT := fields[8]

			if cn == "" || cn == "UNDEF" {
				continue
			}

			// Validate bytes are numeric
			if _, err := strconv.ParseInt(bytesRecv, 10, 64); err != nil {
				continue
			}
			if _, err := strconv.ParseInt(bytesSent, 10, 64); err != nil {
				continue
			}

			clientCount++

			labels := fmt.Sprintf(`common_name="%s"`, cn)
			sinceTs := connectedSinceT
			if sinceTs == "" || sinceTs == "0" {
				sinceTs = parseDateTime(connectedSince)
			}

			if isFirstClient {
				e.addMetric("openvpn_client_bytes_received", "gauge", "Bytes received from client", bytesRecv, labels)
				e.addMetric("openvpn_client_bytes_sent", "gauge", "Bytes sent to client", bytesSent, labels)
				e.addMetric("openvpn_client_connected_since_timestamp", "gauge", "Unix timestamp when client connected", sinceTs, labels)
				isFirstClient = false
			} else {
				e.mu.Lock()
				e.metrics["openvpn_client_bytes_received"] = append(e.metrics["openvpn_client_bytes_received"], MetricValue{
					Help:   "Bytes received from client",
					Type:   "gauge",
					Value:  bytesRecv,
					Labels: labels,
				})
				e.metrics["openvpn_client_bytes_sent"] = append(e.metrics["openvpn_client_bytes_sent"], MetricValue{
					Help:   "Bytes sent to client",
					Type:   "gauge",
					Value:  bytesSent,
					Labels: labels,
				})
				e.metrics["openvpn_client_connected_since_timestamp"] = append(e.metrics["openvpn_client_connected_since_timestamp"], MetricValue{
					Help:   "Unix timestamp when client connected",
					Type:   "gauge",
					Value:  sinceTs,
					Labels: labels,
				})
				e.mu.Unlock()
			}

		case "GLOBAL_STATS":
			if len(fields) >= 3 {
				statName := fields[1]
				statValue := fields[2]
				if statName == "Max bcast/mcast queue length" {
					maxQueue = statValue
				}
			}

		case "END":
			goto done
		}
	}

done:
	e.addMetric("openvpn_connected_clients", "gauge", "Number of currently connected clients", strconv.Itoa(clientCount), "")

	if maxQueue == "" {
		maxQueue = "0"
	}
	e.addMetric("openvpn_server_max_bcast_mcast_queue_length", "gauge", "Maximum broadcast/multicast queue length", maxQueue, "")

	return scanner.Err()
}

// collectStatusFileAge calculates the age of the status file
func (e *OpenVPNExporter) collectStatusFileAge() {
	info, err := os.Stat(e.statusFile)
	if err != nil {
		e.addMetric("openvpn_status_file_age_seconds", "gauge", "Age of the status file in seconds", "-1", "")
		return
	}

	age := time.Since(info.ModTime()).Seconds()
	e.addMetric("openvpn_status_file_age_seconds", "gauge", "Age of the status file in seconds", strconv.FormatFloat(age, 'f', 0, 64), "")
}

// Collect gathers metrics from the OpenVPN status file
func (e *OpenVPNExporter) Collect() error {
	startTime := time.Now()
	e.clearMetrics()

	// Exporter info
	e.addMetric("openvpn_exporter_info", "gauge", "Exporter version information", "1", fmt.Sprintf(`version="%s"`, version))

	// Try to read and parse status file
	if _, err := os.Stat(e.statusFile); err != nil {
		e.addMetric("openvpn_up", "gauge", "OpenVPN status file reachability (1=readable, 0=not found)", "0", "")
		e.addMetric("openvpn_connected_clients", "gauge", "Number of currently connected clients", "0", "")
		e.addMetric("openvpn_server_max_bcast_mcast_queue_length", "gauge", "Maximum broadcast/multicast queue length", "0", "")
		e.addMetric("openvpn_status_file_age_seconds", "gauge", "Age of the status file in seconds", "-1", "")
	} else {
		e.addMetric("openvpn_up", "gauge", "OpenVPN status file reachability (1=readable, 0=not found)", "1", "")

		// Detect format and collect
		format, err := e.detectStatusFormat()
		if err != nil {
			log.Printf("Error detecting format: %v", err)
		} else {
			if format == "v3" {
				if err := e.collectStatusV3(); err != nil {
					log.Printf("Error collecting v3 metrics: %v", err)
				}
			} else {
				if err := e.collectStatusV2(); err != nil {
					log.Printf("Error collecting v2 metrics: %v", err)
				}
			}
		}

		e.collectStatusFileAge()
	}

	// Exporter performance metrics
	e.duration = time.Since(startTime)
	e.lastRun = startTime

	e.addMetric("openvpn_exporter_duration_seconds", "gauge", "Time to generate all metrics", fmt.Sprintf("%.6f", e.duration.Seconds()), "")
	e.addMetric("openvpn_exporter_last_run_timestamp", "gauge", "Unix timestamp of last successful run", strconv.FormatInt(time.Now().Unix(), 10), "")

	return nil
}

// MetricsHandler handles the /metrics endpoint
func (e *OpenVPNExporter) MetricsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Collect metrics on each request
	if err := e.Collect(); err != nil {
		http.Error(w, fmt.Sprintf("Error collecting metrics: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, e.formatMetrics())
}

// HealthHandler handles the /health endpoint
func (e *OpenVPNExporter) HealthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"status":"healthy","version":"%s"}`, version)
}

func main() {
	// Get configuration from environment variables
	statusFile := os.Getenv("OPENVPN_STATUS_FILE")
	if statusFile == "" {
		statusFile = "/var/log/openvpn/openvpn-status.log"
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = "0.0.0.0"
	}

	fullAddr := fmt.Sprintf("%s:%s", addr, port)

	// Create exporter instance
	exporter := NewOpenVPNExporter(statusFile)

	log.Printf("Starting OpenVPN Prometheus Exporter v%s", version)
	log.Printf("Status file: %s", statusFile)
	log.Printf("Listening on: %s", fullAddr)

	// Register HTTP handlers
	http.HandleFunc("/metrics", exporter.MetricsHandler)
	http.HandleFunc("/health", exporter.HealthHandler)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprintf(w, `OpenVPN Prometheus Exporter v%s
Available endpoints:
  GET /metrics  - Prometheus metrics
  GET /health   - Health check
`, version)
	})

	// Start server
	if err := http.ListenAndServe(fullAddr, nil); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
