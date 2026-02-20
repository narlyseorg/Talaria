package monitor

import (
	"context"
	"log"
	"os/exec"
)

func RunCmd(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			log.Printf("Subprocess error [%s %v]: %v, stderr: %s", name, args, err, string(exitErr.Stderr))
		} else {
			log.Printf("Subprocess error [%s %v]: %v", name, args, err)
		}
	}
	return out, err
}

func RunCmdPlain(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			log.Printf("Subprocess error [%s %v]: %v, stderr: %s", name, args, err, string(exitErr.Stderr))
		} else {
			log.Printf("Subprocess error [%s %v]: %v", name, args, err)
		}
	}
	return out, err
}
