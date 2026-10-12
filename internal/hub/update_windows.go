package hub

import "os/exec"

// ownGroup does nothing on Windows: a cancel ends the update process only.
func ownGroup(cmd *exec.Cmd) {}
