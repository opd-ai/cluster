//go:build !windows

package main

import (
	"io/fs"
	"syscall"
	"time"
)

// accessTime returns the last-access time of a file from its FileInfo.
func accessTime(info fs.FileInfo) time.Time {
	sys, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return info.ModTime()
	}
	return time.Unix(sys.Atim.Sec, sys.Atim.Nsec)
}
