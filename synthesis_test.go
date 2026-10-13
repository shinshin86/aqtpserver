package main

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestMain also makes the test binary a fake CLI, exercising exec on every OS.
func TestMain(m *testing.M) {
	if os.Getenv("AQTPSERVER_TEST_HELPER") == "1" {
		input, output := "", ""
		for i := 1; i+1 < len(os.Args); i += 2 {
			switch os.Args[i] {
			case "-T", "/T":
				input = os.Args[i+1]
			case "-W", "/W":
				output = os.Args[i+1]
			}
		}
		if code, err := strconv.Atoi(strings.TrimPrefix(input, "exit:")); strings.HasPrefix(input, "exit:") && err == nil {
			os.Exit(code)
		}
		if input == "block" {
			if marker := os.Getenv("AQTPSERVER_TEST_STARTED"); marker != "" {
				_ = os.WriteFile(marker, []byte("started"), 0600)
			}
			time.Sleep(30 * time.Second)
		}
		if !filepath.IsAbs(output) {
			os.Exit(2)
		}
		if err := os.WriteFile(output, testWAV(), 0600); err != nil {
			os.Exit(3)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func testWAV() []byte {
	data := make([]byte, 44+160)
	copy(data, "RIFF")
	binary.LittleEndian.PutUint32(data[4:], uint32(len(data)-8))
	copy(data[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(data[16:], 16)
	binary.LittleEndian.PutUint16(data[20:], 1)
	binary.LittleEndian.PutUint16(data[22:], 1)
	binary.LittleEndian.PutUint32(data[24:], 8000)
	binary.LittleEndian.PutUint32(data[28:], 16000)
	binary.LittleEndian.PutUint16(data[32:], 2)
	binary.LittleEndian.PutUint16(data[34:], 16)
	copy(data[36:], "data")
	binary.LittleEndian.PutUint32(data[40:], 160)
	return data
}

type runnerFunc func(context.Context, string, []string) error

func (fn runnerFunc) Run(ctx context.Context, executable string, args []string) error {
	return fn(ctx, executable, args)
}

type testExit int

func (e testExit) Error() string { return fmt.Sprintf("exit %d", e) }
func (e testExit) ExitCode() int { return int(e) }

func testSynth(t *testing.T, runner commandRunner) *synthesizer {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return &synthesizer{executable: executable, platform: "darwin", runner: runner, tempRoot: t.TempDir()}
}

func assertClean(t *testing.T, root string) {
	t.Helper()
	files, err := os.ReadDir(root)
	if err != nil || len(files) != 0 {
		t.Fatalf("temporary files remain: %v (%v)", files, err)
	}
}

func TestBuildArgs(t *testing.T) {
	input := "#>テ'スト。 \"quoted\"; $(echo unsafe)\nnext"
	for _, platform := range []string{"darwin", "windows"} {
		for _, preset := range []string{"", "A preset 日本語"} {
			prefix := "-"
			if platform == "windows" {
				prefix = "/"
			}
			want := []string{prefix + "T", input}
			if preset != "" {
				want = append(want, prefix+"P", preset)
			}
			want = append(want, prefix+"W", "output with spaces.wav")
			got, err := buildArgs(platform, input, preset, "output with spaces.wav")
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("%s: %q, %v; want %q", platform, got, err, want)
			}
		}
	}
	if _, err := buildArgs("linux", input, "", "out.wav"); err == nil {
		t.Fatal("unsupported platform accepted")
	}
}

func TestExitCodeMapping(t *testing.T) {
	for _, tc := range []struct {
		platform string
		codes    []int
		status   int
		code     string
		param    string
	}{
		{"windows", []int{2021}, 400, "preset_not_found", "voice"},
		{"windows", []int{2019}, 400, "invalid_preset", "voice"},
		{"darwin", []int{29}, 400, "preset_not_found", "voice"},
		{"windows", []int{2014}, 400, "empty_input", "input"},
		{"darwin", []int{24}, 400, "empty_input", "input"},
		{"windows", []int{2010}, 400, "input_too_long", "input"},
		{"darwin", []int{120, 121, 122, 135, 200, 201, 202, 204}, 400, "input_too_long", "input"},
		{"windows", []int{2016}, 500, "output_error", ""},
		{"darwin", []int{26}, 500, "output_error", ""},
		{"windows", []int{2000, 2001, 2005, 2017, 2018, 2020, 2022, 999}, 500, "synthesis_failed", ""},
		{"darwin", []int{10, 11, 14, 15, 21, 32, 100, 101, 102, 103, 104, 130, 131, 134, 136, 137, 138, 139, 203, 999}, 500, "synthesis_failed", ""},
	} {
		for _, code := range tc.codes {
			got := mapExitCode(tc.platform, code)
			param := ""
			if got.Param != nil {
				param = *got.Param
			}
			if got.Status != tc.status || got.Code != tc.code || param != tc.param || got.Message == "" {
				t.Errorf("%s/%d: %+v", tc.platform, code, got)
			}
		}
	}
}

func TestSynthesisOutputAndCleanup(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
		err  error
		code string
	}{
		{"valid", testWAV(), nil, ""},
		{"missing", nil, nil, "output_error"},
		{"empty", []byte{}, nil, "invalid_audio"},
		{"short", []byte("RIFF"), nil, "invalid_audio"},
		{"wrong signature", []byte("RIFF1234xxxx"), nil, "invalid_audio"},
		{"wrong container", []byte("xxxx1234WAVE"), nil, "invalid_audio"},
		{"exit", nil, testExit(29), "preset_not_found"},
		{"launch failure", nil, errors.New("secret input"), "execution_failed"},
		{"missing exe", nil, os.ErrNotExist, "executable_not_found"},
		{"timeout", nil, context.DeadlineExceeded, "synthesis_timeout"},
		{"canceled", nil, context.Canceled, "request_canceled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := testSynth(t, runnerFunc(func(_ context.Context, _ string, args []string) error {
				path := args[len(args)-1]
				if !filepath.IsAbs(path) {
					t.Fatal("output path is relative")
				}
				if tc.data != nil {
					if err := os.WriteFile(path, tc.data, 0600); err != nil {
						t.Fatal(err)
					}
				}
				return tc.err
			}))
			wav, err := s.Synthesize(context.Background(), "text", "preset")
			if tc.code == "" {
				if err != nil || !reflect.DeepEqual(wav, tc.data) {
					t.Fatalf("%v %v", wav, err)
				}
			} else if err == nil || err.Code != tc.code || strings.Contains(err.Message, "secret input") {
				t.Fatalf("want %s, got %+v", tc.code, err)
			}
			assertClean(t, s.tempRoot)
		})
	}
}

func TestSynthesisMissingExecutable(t *testing.T) {
	s := testSynth(t, runnerFunc(func(context.Context, string, []string) error {
		t.Fatal("runner called for missing executable")
		return nil
	}))
	s.executable = filepath.Join(t.TempDir(), "missing")
	_, err := s.Synthesize(context.Background(), "text", "")
	if err == nil || err.Status != 503 {
		t.Fatalf("got %+v", err)
	}
	assertClean(t, s.tempRoot)
}

func TestSynthesisSerializes(t *testing.T) {
	var active, maximum atomic.Int32
	s := testSynth(t, runnerFunc(func(_ context.Context, _ string, args []string) error {
		n := active.Add(1)
		if n > maximum.Load() {
			maximum.Store(n)
		}
		defer active.Add(-1)
		time.Sleep(5 * time.Millisecond)
		return os.WriteFile(args[len(args)-1], testWAV(), 0600)
	}))
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.Synthesize(context.Background(), "text", ""); err != nil {
				t.Errorf("%+v", err)
			}
		}()
	}
	wg.Wait()
	if maximum.Load() != 1 {
		t.Fatalf("%d simultaneous processes", maximum.Load())
	}
	assertClean(t, s.tempRoot)
}

func TestQueuedRequestTimesOutWithoutStarting(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	s := testSynth(t, runnerFunc(func(_ context.Context, _ string, args []string) error {
		calls.Add(1)
		close(started)
		<-release
		return os.WriteFile(args[len(args)-1], testWAV(), 0600)
	}))
	done := make(chan *apiError, 1)
	go func() { _, err := s.Synthesize(context.Background(), "first", ""); done <- err }()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := s.Synthesize(ctx, "second", "")
	close(release)
	firstErr := <-done
	if err == nil || err.Status != 504 || firstErr != nil || calls.Load() != 1 {
		t.Fatalf("queued=%+v first=%+v calls=%d", err, firstErr, calls.Load())
	}
	assertClean(t, s.tempRoot)
}

func TestRealExecSynthesis(t *testing.T) {
	t.Setenv("AQTPSERVER_TEST_HELPER", "1")
	s := testSynth(t, execRunner{})
	if runtime.GOOS == "windows" {
		s.platform = "windows"
	}
	wav, err := s.Synthesize(context.Background(), "#>テ'スト。 \"hello\"", "test preset")
	if err != nil || !reflect.DeepEqual(wav, testWAV()) {
		t.Fatalf("real exec: %+v, %d bytes", err, len(wav))
	}
	exitCode := 29
	if s.platform == "windows" {
		exitCode = 2021
	}
	_, err = s.Synthesize(context.Background(), fmt.Sprintf("exit:%d", exitCode), "")
	if err == nil || err.Code != "preset_not_found" {
		t.Fatalf("real exit: %+v", err)
	}
	assertClean(t, s.tempRoot)
}

func TestRealExecTimeoutKillsProcess(t *testing.T) {
	t.Setenv("AQTPSERVER_TEST_HELPER", "1")
	marker := filepath.Join(t.TempDir(), "started")
	t.Setenv("AQTPSERVER_TEST_STARTED", marker)
	s := testSynth(t, execRunner{})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	_, err := s.Synthesize(ctx, "block", "")
	if err == nil || err.Status != 504 {
		t.Fatalf("got %+v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("helper did not start: %v", err)
	}
	if time.Since(start) > 10*time.Second {
		t.Fatal("process was not killed promptly")
	}
	assertClean(t, s.tempRoot)
}
