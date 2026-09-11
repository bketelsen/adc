//go:build !linux

package adc

import "os/exec"

// Process-tree cleanup outside Linux is not qualified for protected execution.
func configureGatewayProcess(cmd *exec.Cmd) {}
func stopGatewayProcess(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
