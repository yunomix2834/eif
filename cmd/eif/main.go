package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yunomix2834/eif/internal/core/buildinfo"
	"github.com/yunomix2834/eif/internal/core/config"
	"github.com/yunomix2834/eif/internal/core/logger"
	coremodule "github.com/yunomix2834/eif/internal/core/module"
	corehttp "github.com/yunomix2834/eif/internal/core/protocol/httpclient"
	"github.com/yunomix2834/eif/internal/core/router"
	hddtgdt "github.com/yunomix2834/eif/internal/modules/hoadondientu.gdt.gov.vn"
	"github.com/yunomix2834/eif/internal/modules/updater"
)

func main() {
	if err := run(); err != nil {
		slog.Error(
			"EIF stopped",
			"error",
			err,
		)
		showStartupError(err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--version", "version":
			fmt.Println(buildinfo.String())
			return nil
		}
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf(
			"load config: %w",
			err,
		)
	}

	appLogger := logger.Initialize(
		cfg.Logger.Level,
		cfg.Logger.Format,
	)

	httpClient := corehttp.New(cfg.HDDTGDT.Timeout)
	hddtModule, err := hddtgdt.New(
		httpClient,
		cfg.HDDTGDT,
	)
	if err != nil {
		return fmt.Errorf(
			"initialize HDDT GDT module: %w",
			err,
		)
	}

	updateReady := make(chan struct{}, 1)
	registrars := []coremodule.Registrar{hddtModule}
	if cfg.Updater.IsEnabled {
		updateModule, err := updater.New(
			cfg.Updater,
			buildinfo.Version,
			func() {
				select {
				case updateReady <- struct{}{}:
				default:
				}
			},
		)
		if err != nil {
			if errors.Is(err, updater.ErrUnsupportedPlatform) {
				appLogger.Warn("automatic updates disabled", "error", err)
			} else {
				return fmt.Errorf("initialize updater module: %w", err)
			}
		} else {
			registrars = append(registrars, updateModule)
		}
	}
	engine := router.BuildEngine(
		cfg,
		registrars...,
	)

	// Dùng net.Listen() riêng
	// -> phát hiện được lỗi bind port trước khi chạy server
	// -> giúp user nhìn thấy lỗi ngay
	server := &http.Server{
		Addr:              cfg.Server.BuildListenAddress(),
		Handler:           engine,
		ReadHeaderTimeout: cfg.Server.ReadHeaderTimeout,
	}

	listener, err := net.Listen(
		"tcp",
		server.Addr,
	)
	if err != nil {
		return fmt.Errorf(
			"listen on %s: %w",
			server.Addr,
			err,
		)
	}
	defer listener.Close()

	// channel buffer = 1
	// Khi graceful shutdown:
	// main goroutine
	// ↓
	// server.Shutdown()
	//
	// Serve goroutine
	// ↓
	// Serve returns ErrServerClosed
	// ↓
	// serverErr <- nil
	// Lúc này main có thể không còn đọc channel nữa.
	// Nếu channel unbuffered: make(chan error)
	// --> goroutine có thể bị block tại: serverErr <- nil
	// Với: make(chan error, 1) --> nó gửi vào buffer rồi thoát.
	serverErr := make(
		chan error,
		1,
	)
	go func() {
		appLogger.Info(
			"server started",
			"addr",
			listener.Addr().String(),
		)
		err := server.Serve(listener)
		if errors.Is(
			err,
			http.ErrServerClosed,
		) {
			serverErr <- nil
			return
		}
		serverErr <- err
	}()

	if cfg.Server.ShouldOpenBrowser {
		// browser không mở được không có nghĩa backend bị hỏng.
		browserURL := cfg.Server.BuildBrowserURL()
		if err := openBrowser(browserURL); err != nil {
			appLogger.Warn(
				"open browser failed",
				"url",
				browserURL,
				"error",
				err,
			)
		} else {
			appLogger.Info(
				"browser opened",
				"url",
				browserURL,
			)
		}
	}

	// bắt signal -> Context sẽ bị cancel khi nhận
	sigCtx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	select {
	case err := <-serverErr:
		if err != nil {
			return fmt.Errorf(
				"HTTP server stopped unexpectedly: %w",
				err,
			)
		}
		return errors.New("HTTP server stopped unexpectedly")
	case <-sigCtx.Done():
		appLogger.Info("shutdown signal received")
	case <-updateReady:
		appLogger.Info("update prepared; restarting EIF")
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		cfg.Server.ShutdownTimeout,
	)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		return fmt.Errorf(
			"graceful shutdown failed: %w",
			err,
		)
	}

	time.Sleep(10 * time.Millisecond)
	return nil
}
