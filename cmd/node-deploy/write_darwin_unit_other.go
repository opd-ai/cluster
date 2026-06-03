//go:build !darwin
// +build !darwin

package main

import (
	"fmt"

	"github.com/opd-ai/cluster/internal/serviceinstall"
)

func writeDarwinUnit(_ *serviceinstall.SystemdUnit, _ bool) (string, error) {
	return "", fmt.Errorf("darwin service installation is only supported on darwin")
}
