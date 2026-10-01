//go:build windows

package core

// ownerDead is not checked on Windows: a leftover lock is reported as busy.
func ownerDead(owner []byte) bool { return false }

// ProcStart is not available on Windows.
func ProcStart(pid int) string { return "" }
