//go:build !unix

package github

import "os/exec"

func detach(cmd *exec.Cmd) {}
