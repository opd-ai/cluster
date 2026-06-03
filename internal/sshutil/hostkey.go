// Package sshutil provides SSH helper utilities, including host key
// verification backed by a known_hosts file.
package sshutil

import (
	"log"
	"os/user"
	"path/filepath"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// HostKeyCallback returns an ssh.HostKeyCallback that verifies host keys
// against the given known_hosts file. When knownHostsPath is empty, the
// user's default ~/.ssh/known_hosts file is used. If insecureSkip is true,
// host key verification is disabled and a warning is logged; this is unsafe
// because it allows man-in-the-middle attacks to go undetected.
func HostKeyCallback(knownHostsPath string, insecureSkip bool) (ssh.HostKeyCallback, error) {
	if insecureSkip {
		log.Println("WARNING: SSH host key verification is disabled (insecure-skip-host-key=true); " +
			"man-in-the-middle attacks will not be detected")
		return ssh.InsecureIgnoreHostKey(), nil
	}

	if knownHostsPath == "" {
		var err error
		knownHostsPath, err = defaultKnownHostsPath()
		if err != nil {
			return nil, err
		}
	}

	return knownhosts.New(knownHostsPath)
}

func defaultKnownHostsPath() (string, error) {
	usr, err := user.Current()
	if err != nil {
		return "", err
	}

	return filepath.Join(usr.HomeDir, ".ssh", "known_hosts"), nil
}
