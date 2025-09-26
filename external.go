package ecrm

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

type ExternalCommand struct {
	Command []string          `json:"command"`
	Env     map[string]string `json:"env,omitempty"`
	Dir     string            `json:"dir,omitempty"`
	Timeout time.Duration     `json:"timeout,omitempty"`
}

func (s *Scanner) scanExternalCommands(ctx context.Context, commands []*ExternalCommand) error {
	for _, ext := range commands {
		b, err := ext.Run(ctx)
		if err != nil {
			return err
		}
		imgs := make(Images)
		src := "external_command: " + strings.Join(ext.Command, " ")
		if err := imgs.LoadExternalJSON(src, b); err != nil {
			return err
		}
		s.Images.Merge(imgs)
	}
	return nil
}

func (ext *ExternalCommand) Run(ctx context.Context) ([]byte, error) {
	log.Printf("[info] scanning by external command: %v", ext.Command)
	var cancel context.CancelFunc
	if ext.Timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, ext.Timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, ext.Command[0], ext.Command[1:]...)
	cmd.Env = os.Environ()
	for k, v := range ext.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}
	buf := &bytes.Buffer{}
	cmd.Stdout = buf
	cmd.Stderr = os.Stderr
	cmd.WaitDelay = 5 * time.Second // SIGKILL after 5 seconds of SIGTERM
	if ext.Dir != "" {
		cmd.Dir = ext.Dir
	}
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to run external command %v: %w", ext.Command, err)
	}
	return buf.Bytes(), nil
}
