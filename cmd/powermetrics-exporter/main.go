// cmd/powermetrics-exporter is a Prometheus exporter for Apple Silicon / macOS
// nodes. It runs `powermetrics` periodically and translates its JSON output
// into Prometheus text format on :9401/metrics.
//
// Exposed metrics:
//
//	powermetrics_cpu_power_mw           — combined CPU package power (mW)
//	powermetrics_gpu_power_mw           — GPU power draw (mW)
//	powermetrics_ane_power_mw           — Apple Neural Engine power draw (mW)
//	powermetrics_combined_power_mw      — combined package power (mW)
//	powermetrics_thermal_pressure       — thermal pressure level (0=nominal,1=fair,2=serious,3=critical)
//	powermetrics_cpu_freq_mhz{cluster}  — per-cluster CPU frequency (MHz)
//
// Usage:
//
//	powermetrics-exporter [flags]
//
// Flags:
//
//	-addr        listen address (default: :9401)
//	-interval    sample interval in milliseconds (default: 10000)
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"sync"
	"time"
)

// pmSample holds parsed powermetrics JSON fields relevant to the exporter.
type pmSample struct {
	// Nested processor block.
	Processor struct {
		CPUPowerMW      float64 `json:"cpu_power"`
		GPUPowerMW      float64 `json:"gpu_power"`
		ANEPowerMW      float64 `json:"ane_power"`
		CombinedPowerMW float64 `json:"combined_power"`
		Clusters        []struct {
			Name    string  `json:"name"`
			FreqMHz float64 `json:"freq_hz"`
		} `json:"clusters"`
	} `json:"processor"`

	ThermalPressure string `json:"thermal_pressure"`
}

// thermalLevel converts the thermal_pressure string to a numeric level.
func thermalLevel(s string) float64 {
	switch s {
	case "Nominal":
		return 0
	case "Fair":
		return 1
	case "Serious":
		return 2
	case "Critical":
		return 3
	default:
		return 0
	}
}

// exporter holds the latest sample protected by a mutex.
type exporter struct {
	mu     sync.RWMutex
	sample pmSample
}

// collect runs powermetrics once and updates the stored sample.
func (e *exporter) collect(intervalMS int) {
	args := []string{
		"-i", fmt.Sprintf("%d", intervalMS),
		"-n", "1",
		"--samplers", "cpu_power,gpu_power,thermal",
		"-f", "json",
	}
	out, err := exec.Command("powermetrics", args...).Output()
	if err != nil {
		log.Printf("powermetrics: %v", err)
		return
	}
	var s pmSample
	if err := json.Unmarshal(out, &s); err != nil {
		log.Printf("parse: %v", err)
		return
	}
	e.mu.Lock()
	e.sample = s
	e.mu.Unlock()
}

// loop runs collect on the given interval.
func (e *exporter) loop(intervalMS int) {
	e.collect(intervalMS)
	ticker := time.NewTicker(time.Duration(intervalMS) * time.Millisecond)
	defer ticker.Stop()
	for range ticker.C {
		e.collect(intervalMS)
	}
}

// handleMetrics serves the Prometheus text exposition.
func (e *exporter) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	e.mu.RLock()
	s := e.sample
	e.mu.RUnlock()

	p := s.Processor
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, "# HELP powermetrics_cpu_power_mw CPU package power (mW)\n")
	fmt.Fprintf(w, "# TYPE powermetrics_cpu_power_mw gauge\n")
	fmt.Fprintf(w, "powermetrics_cpu_power_mw %g\n", p.CPUPowerMW)
	fmt.Fprintf(w, "# HELP powermetrics_gpu_power_mw GPU power draw (mW)\n")
	fmt.Fprintf(w, "# TYPE powermetrics_gpu_power_mw gauge\n")
	fmt.Fprintf(w, "powermetrics_gpu_power_mw %g\n", p.GPUPowerMW)
	fmt.Fprintf(w, "# HELP powermetrics_ane_power_mw Apple Neural Engine power (mW)\n")
	fmt.Fprintf(w, "# TYPE powermetrics_ane_power_mw gauge\n")
	fmt.Fprintf(w, "powermetrics_ane_power_mw %g\n", p.ANEPowerMW)
	fmt.Fprintf(w, "# HELP powermetrics_combined_power_mw Combined package power (mW)\n")
	fmt.Fprintf(w, "# TYPE powermetrics_combined_power_mw gauge\n")
	fmt.Fprintf(w, "powermetrics_combined_power_mw %g\n", p.CombinedPowerMW)
	fmt.Fprintf(w, "# HELP powermetrics_thermal_pressure Thermal pressure level (0=nominal,1=fair,2=serious,3=critical)\n")
	fmt.Fprintf(w, "# TYPE powermetrics_thermal_pressure gauge\n")
	fmt.Fprintf(w, "powermetrics_thermal_pressure %g\n", thermalLevel(s.ThermalPressure))
	for _, cl := range p.Clusters {
		freqMHz := cl.FreqMHz / 1e6
		fmt.Fprintf(w, "# HELP powermetrics_cpu_freq_mhz CPU cluster frequency (MHz)\n")
		fmt.Fprintf(w, "# TYPE powermetrics_cpu_freq_mhz gauge\n")
		fmt.Fprintf(w, "powermetrics_cpu_freq_mhz{cluster=%q} %g\n", cl.Name, freqMHz)
	}
}

func main() {
	addr := flag.String("addr", ":9401", "Listen address")
	intervalMS := flag.Int("interval", 10000, "Sample interval in milliseconds")
	flag.Parse()

	e := &exporter{}
	go e.loop(*intervalMS)

	http.HandleFunc("/metrics", e.handleMetrics)
	http.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	log.Printf("powermetrics-exporter listening on %s", *addr)
	if err := http.ListenAndServe(*addr, nil); err != nil {
		log.Fatalf("listen: %v", err)
	}
}
