//go:build !windows

package core

import (
	"os"
	"syscall"
)

func ownedByMe(st os.FileInfo) bool {
	sys, ok := st.Sys().(*syscall.Stat_t)
	return ok && int(sys.Uid) == os.Getuid()
}
