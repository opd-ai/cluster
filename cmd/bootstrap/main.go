// cmd/bootstrap is the single-command entry point for bootstrapping the
// cluster from scratch.  It SSHes into every node listed in the inventory,
// installs all prerequisites, brings up the k3s control-plane, joins Linux
// worker nodes, and finally prints the manual join command so that new nodes
// can be added at any time without re-running the full bootstrap.
//
// Usage:
//
//	bootstrap [flags]
//
// Flags:
//
//	-inventory  path to cluster/inventory.yaml (default: cluster/inventory.yaml)
//	-key        SSH private key path (default: SSH agent / ~/.ssh/id_rsa)
//	-timeout    SSH timeout in seconds (default: 30)
//	-dry-run    print commands without executing them
//	-insecure-skip-hostkey-check  skip SSH host key verification (not for production)
//
// Unlike cmd/cluster-bootstrap, this binary always runs the full bootstrap
// AND the cluster bring-up in a single invocation — there are no mode flags.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() {
	var (
		inventory    = flag.String("inventory", "cluster/inventory.yaml", "Path to inventory YAML")
		key          = flag.String("key", "", "SSH private key path")
		timeout      = flag.Int("timeout", 30, "SSH timeout in seconds")
		dryRun       = flag.Bool("dry-run", false, "Print commands without executing them")
		insecureSkip = flag.Bool("insecure-skip-hostkey-check", false, "Skip SSH host key verification")
	)
	flag.Parse()

	// Resolve the cluster-bootstrap binary: prefer one next to this binary,
	// then fall back to go run (useful during development).
	cbPath, err := resolveClusterBootstrap()
	if err != nil {
		log.Fatalf("cannot locate cluster-bootstrap: %v\n"+
			"Run `make build` first, or use `go run ./cmd/cluster-bootstrap` directly.", err)
	}

	args := []string{
		"--inventory", *inventory,
		"--up",
		"--timeout", fmt.Sprintf("%d", *timeout),
	}
	if *key != "" {
		args = append(args, "--key", *key)
	}
	if *dryRun {
		args = append(args, "--dry-run")
	}
	if *insecureSkip {
		args = append(args, "--insecure-skip-hostkey-check")
	}

	fmt.Println("=== Single-command cluster bootstrap ===")
	fmt.Printf("Inventory: %s\n\n", *inventory)

	cmd := exec.Command(cbPath, args...) //nolint:gosec
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		log.Fatalf("bootstrap failed: %v", err)
	}
}

// resolveClusterBootstrap finds the cluster-bootstrap binary.  It checks
// (in order):
//  1. A sibling binary in the same directory as the running bootstrap binary.
//  2. The project bin/ directory relative to the working directory.
//  3. cluster-bootstrap on PATH.
func resolveClusterBootstrap() (string, error) {
	// 1. Sibling of the running binary (both built by `make build`).
	if self, err := os.Executable(); err == nil {
		sibling := filepath.Join(filepath.Dir(self), "cluster-bootstrap")
		if _, err := os.Stat(sibling); err == nil {
			return sibling, nil
		}
	}

	// 2. bin/cluster-bootstrap relative to CWD.
	if _, err := os.Stat("bin/cluster-bootstrap"); err == nil {
		abs, err := filepath.Abs("bin/cluster-bootstrap")
		if err == nil {
			return abs, nil
		}
	}

	// 3. PATH.
	p, err := exec.LookPath("cluster-bootstrap")
	if err == nil {
		return p, nil
	}

	candidates := []string{"bin/cluster-bootstrap", "cluster-bootstrap"}
	return "", fmt.Errorf("tried: %s", strings.Join(candidates, ", "))
}
