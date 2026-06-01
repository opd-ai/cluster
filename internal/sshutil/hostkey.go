package sshutil

import (
	"log"
	"os/user"
	"path/filepath"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

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
