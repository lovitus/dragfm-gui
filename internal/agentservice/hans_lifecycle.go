package agentservice

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/lovitus/dragfm-gui/internal/agentlease"
	"github.com/lovitus/dragfm-gui/internal/agentproto"
)

const processOutputLimit = 8192

type processOutput struct {
	mu   sync.Mutex
	data []byte
}

func (b *processOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	if n >= processOutputLimit {
		b.data = append(b.data[:0], p[n-processOutputLimit:]...)
	} else {
		if excess := len(b.data) + n - processOutputLimit; excess > 0 {
			b.data = b.data[excess:]
		}
		b.data = append(b.data, p...)
	}
	return n, nil
}

func (b *processOutput) text(secrets []string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	text := strings.TrimSpace(string(b.data))
	for _, secret := range secrets {
		if secret != "" {
			text = strings.ReplaceAll(text, secret, "***")
		}
	}
	return text
}

type processJob struct {
	command  *exec.Cmd
	done     chan struct{}
	err      error // Written before done closes; readers wait for that close.
	output   *processOutput
	secrets  []string
	stopOnce sync.Once
	stopErr  error
}

func (job *processJob) failure() error {
	return fmt.Errorf("Hans exited unexpectedly (%v): %s", job.err, job.output.text(job.secrets))
}

func (s *Service) startHans(request agentproto.Request) (string, error) {
	binary, identity, jobID := request.Options["binary"], request.Options["identity"], request.Options["job"]
	passphrase := request.Secret["passphrase"]
	if binary == "" || identity == "" || jobID == "" || passphrase == "" {
		return "", errors.New("Hans process options are incomplete")
	}
	passphrasePath := "/proc/self/fd/3"
	if runtime.GOOS != "linux" {
		passphrasePath = "/dev/fd/3"
	}
	args := []string{"-f", "--require-v5", "--passphrase-file", passphrasePath, "--identity-file", identity}
	fingerprint := ""
	if request.Action == "hans-server-start" {
		network, lease := request.Options["network"], request.Options["lease"]
		if net.ParseIP(network).To4() == nil || lease == "" {
			return "", errors.New("invalid Hans server network or lease path")
		}
		identityCommand := exec.CommandContext(s.ctx, binary, "--show-identity", "--identity-file", identity)
		configureChildLifecycle(identityCommand)
		identityCommand.WaitDelay = 2 * time.Second
		if err := agentlease.Attach(s.ctx, identityCommand); err != nil {
			return "", err
		}
		output, err := identityCommand.Output()
		if err != nil {
			return "", fmt.Errorf("Hans identity generation: %w", err)
		}
		fields := strings.Fields(string(output))
		if len(fields) == 0 {
			return "", errors.New("Hans returned no server fingerprint")
		}
		fingerprint = fields[len(fields)-1]
		args = append(args, "-s", network, "--lease-file", lease)
	} else {
		server, socks, pin := request.Options["server"], request.Options["socks"], request.Secret["fingerprint"]
		if server == "" || socks == "" || pin == "" {
			return "", errors.New("invalid Hans client server, SOCKS, or fingerprint")
		}
		// Keep even the device identity inside the owned temporary directory,
		// not Hans' default persistent application-data location.
		args = append(args, "-c", server, "--feature", "userspace", "--socks5", socks, "--server-fingerprint", pin, "--device-id-file", identity+".device-id")
	}
	command := exec.CommandContext(s.ctx, binary, args...)
	configureChildLifecycle(command)
	command.Cancel = func() error { return signalChildGroup(command, os.Interrupt) }
	command.WaitDelay = 2 * time.Second
	reader, writer, err := os.Pipe()
	if err != nil {
		return "", err
	}
	command.ExtraFiles = []*os.File{reader}
	if err := agentlease.Attach(s.ctx, command); err != nil {
		_ = reader.Close()
		_ = writer.Close()
		return "", err
	}
	job := &processJob{command: command, done: make(chan struct{}), output: &processOutput{}}
	for _, secret := range request.Secret {
		job.secrets = append(job.secrets, secret)
	}
	command.Stdout, command.Stderr = job.output, job.output
	if err := command.Start(); err != nil {
		reader.Close()
		writer.Close()
		return "", err
	}
	go func() { job.err = command.Wait(); close(job.done) }()
	reader.Close()
	if _, err := io.WriteString(writer, passphrase+"\n"); err != nil {
		writer.Close()
		return "", errors.Join(err, job.stop())
	}
	writer.Close()
	s.mu.Lock()
	if _, exists := s.processes[jobID]; exists {
		s.mu.Unlock()
		return "", errors.Join(errors.New("duplicate process job"), job.stop())
	}
	s.processes[jobID] = job
	s.mu.Unlock()
	select {
	case <-job.done:
		err := job.failure()
		_ = s.stopProcess(jobID)
		return "", err
	case <-time.After(100 * time.Millisecond):
		return fingerprint, nil
	}
}

func (s *Service) processStatus(jobID string) error {
	s.mu.Lock()
	job := s.processes[jobID]
	s.mu.Unlock()
	if job == nil {
		return errors.New("Hans process does not exist")
	}
	select {
	case <-job.done:
		return job.failure()
	default:
		return nil
	}
}

func (s *Service) processDiagnostics(jobID string) (string, error) {
	s.mu.Lock()
	job := s.processes[jobID]
	s.mu.Unlock()
	if job == nil {
		return "", errors.New("Hans process does not exist")
	}
	return job.output.text(job.secrets), nil
}

func (s *Service) stopProcess(jobID string) error {
	s.mu.Lock()
	job := s.processes[jobID]
	s.mu.Unlock()
	if job == nil {
		return nil
	}
	err := job.stop()
	if err == nil {
		s.mu.Lock()
		if s.processes[jobID] == job {
			delete(s.processes, jobID)
		}
		s.mu.Unlock()
	}
	return err
}

func (job *processJob) stop() error {
	job.stopOnce.Do(func() {
		select {
		case <-job.done:
		default:
			_ = signalChildGroup(job.command, os.Interrupt)
			select {
			case <-job.done:
			case <-time.After(2 * time.Second):
				_ = signalChildGroup(job.command, os.Kill)
				select {
				case <-job.done:
				case <-time.After(2 * time.Second):
					job.stopErr = errors.New("Hans exit unconfirmed; installation retained")
					return
				}
			}
		}
		var exit *exec.ExitError
		if job.err != nil && !errors.Is(job.err, context.Canceled) && !errors.As(job.err, &exit) {
			job.stopErr = job.err
		}
	})
	return job.stopErr
}
