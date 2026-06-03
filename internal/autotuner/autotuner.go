// Package autotuner handles hardware probing and resource budgeting for multi-role nodes.
package autotuner

import (
	"bytes"
	"os/exec"
	"strconv"
	"strings"
)

// HardwareProfile describes the hardware capabilities of a node.
type HardwareProfile struct {
	Accelerator string // "cuda", "rocm", "metal", "cpu"
	VramGB      int    // total VRAM in GB
	RamGB       int    // total RAM in GB
	DiskGB      int    // total disk in GB
	NumCPUs     int    // number of CPU cores
}

// Probe returns the HardwareProfile of the local node.
func Probe() (*HardwareProfile, error) {
	profile := &HardwareProfile{}

	// Detect accelerator and VRAM
	profile.Accelerator, profile.VramGB = detectAccelerator()

	// Get RAM in GB
	profile.RamGB = getRamGB()

	// Get disk in GB
	profile.DiskGB = getDiskGB()

	// Get number of CPUs
	profile.NumCPUs = getNumCPUs()

	return profile, nil
}

// detectAccelerator detects GPU type and VRAM on the local system.
func detectAccelerator() (string, int) {
	// Check for NVIDIA GPU
	if out, err := runCmd("which nvidia-smi"); err == nil && strings.TrimSpace(out) != "" {
		return detectNvidiaGPU()
	}

	// Check for AMD GPU
	if out, err := runCmd("which rocm-smi"); err == nil && strings.TrimSpace(out) != "" {
		return detectAMDGPU()
	}

	// Check for Apple Silicon
	if out, err := runCmd("system_profiler SPDisplaysDataType 2>/dev/null | grep -i gpu"); err == nil && strings.TrimSpace(out) != "" {
		return "metal", getAppleSiliconVRAM()
	}

	return "cpu", 0
}

// detectNvidiaGPU queries NVIDIA GPU info.
func detectNvidiaGPU() (string, int) {
	out, err := runCmd("nvidia-smi --query-gpu=memory.total --format=csv,noheader,nounits | head -1")
	if err != nil || strings.TrimSpace(out) == "" {
		return "cuda", 0
	}

	vram := 0
	fields := strings.Fields(strings.TrimSpace(out))
	if len(fields) > 0 {
		if mb, err := strconv.Atoi(fields[0]); err == nil {
			vram = (mb + 512) / 1024 // Convert MB to GB with rounding
		}
	}
	return "cuda", vram
}

// detectAMDGPU queries AMD GPU info.
func detectAMDGPU() (string, int) {
	out, err := runCmd("rocm-smi --showmeminfo 2>/dev/null | grep -i 'total' | head -1")
	if err != nil || strings.TrimSpace(out) == "" {
		return "rocm", 0
	}

	vram := 0
	fields := strings.Fields(strings.TrimSpace(out))
	for _, field := range fields {
		if mb, err := strconv.Atoi(strings.TrimSuffix(field, "MB")); err == nil {
			vram = (mb + 512) / 1024 // Convert MB to GB with rounding
			break
		}
	}
	return "rocm", vram
}

// getAppleSiliconVRAM gets unified memory size on Apple Silicon.
func getAppleSiliconVRAM() int {
	out, err := runCmd("sysctl -n hw.memsize")
	if err != nil || strings.TrimSpace(out) == "" {
		return 0
	}

	bytes, err := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
	if err != nil {
		return 0
	}
	return int(bytes / (1024 * 1024 * 1024))
}

// getRamGB gets system RAM in GB.
func getRamGB() int {
	// Try Linux first
	out, err := runCmd("grep MemTotal /proc/meminfo 2>/dev/null | awk '{print $2}'")
	if err == nil && strings.TrimSpace(out) != "" {
		if kb, err := strconv.Atoi(strings.TrimSpace(out)); err == nil {
			return kb / (1024 * 1024)
		}
	}

	// Try macOS
	out, err = runCmd("sysctl -n hw.memsize")
	if err == nil && strings.TrimSpace(out) != "" {
		if bytes, err := strconv.ParseInt(strings.TrimSpace(out), 10, 64); err == nil {
			return int(bytes / (1024 * 1024 * 1024))
		}
	}

	return 0
}

// getDiskGB gets root filesystem size in GB.
func getDiskGB() int {
	out, err := runCmd("df / 2>/dev/null | tail -1 | awk '{print $2}'")
	if err == nil && strings.TrimSpace(out) != "" {
		if kb, err := strconv.Atoi(strings.TrimSpace(out)); err == nil {
			return kb / (1024 * 1024)
		}
	}

	return 0
}

// getNumCPUs gets the number of CPU cores.
func getNumCPUs() int {
	// Try nproc (Linux/Unix)
	out, err := runCmd("nproc")
	if err == nil && strings.TrimSpace(out) != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(out)); err == nil {
			return n
		}
	}

	// Try sysctl on macOS
	out, err = runCmd("sysctl -n hw.ncpu")
	if err == nil && strings.TrimSpace(out) != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(out)); err == nil {
			return n
		}
	}

	return 0
}

// runCmd executes a shell command and returns its output.
func runCmd(cmd string) (string, error) {
	var buf bytes.Buffer
	c := exec.Command("sh", "-c", cmd)
	c.Stdout = &buf
	err := c.Run()
	return buf.String(), err
}
