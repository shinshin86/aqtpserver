package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

type commandRunner interface {
	Run(context.Context, string, []string) error
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, executable string, args []string) error {
	cmd := exec.CommandContext(ctx, executable, args...)
	// Resource files may be resolved relative to the application's directory.
	cmd.Dir = filepath.Dir(executable)
	// Leave stdout/stderr disconnected: application errors can contain input text.
	err := cmd.Run()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

func buildArgs(platform, input, preset, output string) ([]string, error) {
	prefix := "-"
	if platform == "windows" {
		prefix = "/"
	} else if platform != "darwin" {
		return nil, fmt.Errorf("unsupported OS %q", platform)
	}
	args := []string{prefix + "T", input}
	if preset != "" {
		args = append(args, prefix+"P", preset)
	}
	return append(args, prefix+"W", output), nil
}

type synthesizer struct {
	executable string
	platform   string
	runner     commandRunner
	tempRoot   string
	mu         sync.Mutex
	active     chan struct{}
}

// The mutex protects the active process slot; waiters can leave on cancellation.
func (s *synthesizer) acquire(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		s.mu.Lock()
		if s.active == nil {
			s.active = make(chan struct{})
			s.mu.Unlock()
			return nil
		}
		active := s.active
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-active:
		}
	}
}

func (s *synthesizer) release() {
	s.mu.Lock()
	defer s.mu.Unlock()
	close(s.active)
	s.active = nil
}

func (s *synthesizer) Synthesize(ctx context.Context, input, preset string) ([]byte, *apiError) {
	if err := s.acquire(ctx); err != nil {
		return nil, contextError(err)
	}
	defer s.release()
	if !executableExists(s.executable) {
		return nil, executableMissing()
	}
	dir, err := os.MkdirTemp(s.tempRoot, "aqtpserver-")
	if err != nil {
		return nil, internalError("Cannot create a temporary output directory.", "output_error")
	}
	defer os.RemoveAll(dir)
	output, err := filepath.Abs(filepath.Join(dir, "speech.wav"))
	if err != nil {
		return nil, internalError("Cannot resolve the output path.", "output_error")
	}
	args, err := buildArgs(s.platform, input, preset, output)
	if err != nil {
		return nil, internalError("AquesTalkPlayer requires macOS or Windows.", "unsupported_platform")
	}
	if err := ctx.Err(); err != nil {
		return nil, contextError(err)
	}
	if err := s.runner.Run(ctx, s.executable, args); err != nil {
		if ctx.Err() != nil {
			return nil, contextError(ctx.Err())
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return nil, contextError(err)
		}
		if errors.Is(err, os.ErrNotExist) {
			return nil, executableMissing()
		}
		var exitErr interface{ ExitCode() int }
		if errors.As(err, &exitErr) {
			return nil, mapExitCode(s.platform, exitErr.ExitCode())
		}
		return nil, internalError("Cannot run AquesTalkPlayer. Check its installation and executable permissions.", "execution_failed")
	}
	if err := ctx.Err(); err != nil {
		return nil, contextError(err)
	}
	wav, err := os.ReadFile(output)
	if err != nil {
		return nil, internalError("AquesTalkPlayer did not produce a readable WAV file.", "output_error")
	}
	if len(wav) < 12 || string(wav[:4]) != "RIFF" || string(wav[8:12]) != "WAVE" {
		return nil, internalError("AquesTalkPlayer returned an invalid WAV file.", "invalid_audio")
	}
	return wav, nil
}

func contextError(err error) *apiError {
	if errors.Is(err, context.DeadlineExceeded) {
		return &apiError{Status: http.StatusGatewayTimeout, Message: "Synthesis timed out (including queue time).", Type: "server_error", Code: "synthesis_timeout"}
	}
	return &apiError{Status: http.StatusRequestTimeout, Message: "Request canceled.", Type: "invalid_request_error", Code: "request_canceled"}
}

func executableMissing() *apiError {
	return &apiError{Status: http.StatusServiceUnavailable, Message: "AquesTalkPlayer executable not found. Install it yourself and set --aquestalkplayer-path.", Type: "server_error", Code: "executable_not_found"}
}

func mapExitCode(platform string, code int) *apiError {
	if (platform == "windows" && code == 2021) || (platform == "darwin" && code == 29) {
		return invalidRequest("Preset not found or invalid. Create the named preset in the AquesTalkPlayer GUI.", "voice", "preset_not_found")
	}
	if platform == "windows" && code == 2019 {
		return invalidRequest("Invalid preset name.", "voice", "invalid_preset")
	}
	if (platform == "windows" && code == 2014) || (platform == "darwin" && code == 24) {
		return invalidRequest("AquesTalkPlayer received empty text.", "input", "empty_input")
	}
	tooLong := platform == "windows" && code == 2010
	if platform == "darwin" {
		switch code {
		case 120, 121, 122, 135, 200, 201, 202, 204:
			tooLong = true
		}
	}
	if tooLong {
		return invalidRequest("Text or phonetic input is too long for AquesTalkPlayer. Shorten it or split it into sentences.", "input", "input_too_long")
	}
	if (platform == "windows" && code == 2016) || (platform == "darwin" && code == 26) {
		return internalError("AquesTalkPlayer could not write the WAV file. Check temporary directory permissions and free space.", "output_error")
	}
	return internalError(fmt.Sprintf("AquesTalkPlayer synthesis failed (exit code %d). Check the application installation, dictionaries, and presets; see the official manual.", code), "synthesis_failed")
}
