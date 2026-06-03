//go:build darwin
// +build darwin

package main

import "github.com/opd-ai/cluster/internal/serviceinstall"

func writeDarwinUnit(unit *serviceinstall.SystemdUnit, dryRun bool) (string, error) {
	return serviceinstall.WriteDarwinUnit(unit, dryRun)
}
