//go:build !unix

package gitrepo

import "os/exec"

func detach(cmd *exec.Cmd) {}
