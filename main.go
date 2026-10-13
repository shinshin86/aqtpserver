package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"time"
)

var version = "dev"

func parseFlags(args []string, platform string, output io.Writer) (serverConfig, bool, error) {
	cfg := defaultConfig(platform)
	flags := flag.NewFlagSet("aqtpserver", flag.ContinueOnError)
	flags.SetOutput(output)
	flags.StringVar(&cfg.host, "host", cfg.host, "Loopback host only: 127.0.0.1, localhost, or ::1")
	flags.IntVar(&cfg.port, "port", cfg.port, "HTTP port")
	flags.StringVar(&cfg.executable, "aquestalkplayer-path", cfg.executable, "Path to the installed AquesTalkPlayer executable")
	flags.StringVar(&cfg.defaultPreset, "default-preset", "", "Preset used when voice is omitted (otherwise use the last GUI preset)")
	flags.DurationVar(&cfg.timeout, "timeout", cfg.timeout, "Request timeout, including synthesis queue time")
	flags.IntVar(&cfg.maxInputChars, "max-input-chars", cfg.maxInputChars, "Maximum input length in Unicode code points")
	flags.Func("allowed-origin", "Additional exact CORS origins; repeat or separate with spaces", func(value string) error {
		cfg.allowedOrigins = append(cfg.allowedOrigins, strings.Fields(value)...)
		return nil
	})
	showVersion := flags.Bool("version", false, "Show version")
	if err := flags.Parse(args); err != nil {
		return cfg, false, err
	}
	if flags.NArg() != 0 {
		return cfg, false, errors.New("unexpected positional arguments; use --help for usage")
	}
	return cfg, *showVersion, nil
}

func run(args []string) error {
	cfg, showVersion, err := parseFlags(args, runtime.GOOS, os.Stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if showVersion {
		fmt.Println(version)
		return nil
	}
	if err := cfg.validate(runtime.GOOS); err != nil {
		return err
	}
	logger := log.New(os.Stderr, "", log.LstdFlags)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	listener, err := net.Listen("tcp", cfg.listenAddress())
	if err != nil {
		return err
	}
	defer listener.Close()
	synth := &synthesizer{executable: cfg.executable, platform: runtime.GOOS, runner: execRunner{}}
	server := &http.Server{
		Handler:           newServer(cfg, synth, logger),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       cfg.timeout,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
		}
	}()
	logger.Printf("aqtpserver %s listening at http://%s", version, listener.Addr())
	err = server.Serve(listener)
	stop()
	<-done
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
