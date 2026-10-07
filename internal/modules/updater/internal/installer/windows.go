//go:build windows

package installer

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

const windowsAssetName = "EIF-Windows-x64.zip"

type WindowsInstaller struct {
	executablePath string
	processID      int
}

func New() (*WindowsInstaller, error) {
	if runtime.GOARCH != "amd64" {
		return nil, fmt.Errorf("%w: windows/%s", ErrUnsupportedPlatform, runtime.GOARCH)
	}
	executablePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("locate the EIF executable: %w", err)
	}
	executablePath, err = filepath.Abs(executablePath)
	if err != nil {
		return nil, fmt.Errorf("resolve the EIF executable path: %w", err)
	}

	return &WindowsInstaller{
		executablePath: executablePath,
		processID:      os.Getpid(),
	}, nil
}

func (i *WindowsInstaller) GetAssetName() string {
	return windowsAssetName
}

func (i *WindowsInstaller) StageUpdate(asset []byte) error {
	binary, err := extractWindowsBinary(asset)
	if err != nil {
		return err
	}

	dir := filepath.Dir(i.executablePath)
	pendingFile, err := os.CreateTemp(dir, ".eif-update-*.exe")
	if err != nil {
		return fmt.Errorf("create staged executable: %w", err)
	}
	pendingPath := pendingFile.Name()
	defer func() {
		if err != nil {
			os.Remove(pendingPath)
		}
	}()
	if _, err = pendingFile.Write(binary); err != nil {
		pendingFile.Close()
		return fmt.Errorf("write staged executable: %w", err)
	}
	if err = pendingFile.Sync(); err != nil {
		pendingFile.Close()
		return fmt.Errorf("sync staged executable: %w", err)
	}
	if err = pendingFile.Close(); err != nil {
		return fmt.Errorf("close staged executable: %w", err)
	}

	helperFile, err := os.CreateTemp(dir, ".eif-update-*.ps1")
	if err != nil {
		return fmt.Errorf("create update helper: %w", err)
	}
	helperPath := helperFile.Name()
	helper := `param(
  [int]$TargetProcessId,
  [string]$CurrentPath,
  [string]$PendingPath
)
$ErrorActionPreference = 'Stop'
Wait-Process -Id $TargetProcessId -ErrorAction SilentlyContinue
for ($attempt = 0; $attempt -lt 100; $attempt++) {
  try {
    Move-Item -LiteralPath $PendingPath -Destination $CurrentPath -Force
    $env:EIF_OPEN_BROWSER = 'false'
    Start-Process -FilePath $CurrentPath -WorkingDirectory (Split-Path -Parent $CurrentPath)
    Remove-Item -LiteralPath $PSCommandPath -Force
    exit 0
  } catch {
    Start-Sleep -Milliseconds 200
  }
}
exit 1
`
	if _, err = io.WriteString(helperFile, helper); err != nil {
		helperFile.Close()
		os.Remove(helperPath)
		return fmt.Errorf("write update helper: %w", err)
	}
	if err = helperFile.Close(); err != nil {
		os.Remove(helperPath)
		return fmt.Errorf("close update helper: %w", err)
	}

	command := exec.Command(
		"powershell.exe",
		"-NoLogo",
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy", "Bypass",
		"-WindowStyle", "Hidden",
		"-File", helperPath,
		strconv.Itoa(i.processID),
		i.executablePath,
		pendingPath,
	)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	command.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x00000008,
	}
	if err = command.Start(); err != nil {
		os.Remove(helperPath)
		return fmt.Errorf("start update helper: %w", err)
	}
	_ = command.Process.Release()

	return nil
}

func extractWindowsBinary(asset []byte) ([]byte, error) {
	archive, err := zip.NewReader(bytes.NewReader(asset), int64(len(asset)))
	if err != nil {
		return nil, fmt.Errorf("open Windows update archive: %w", err)
	}

	for _, file := range archive.File {
		if !strings.EqualFold(filepath.Base(file.Name), "EIF.exe") {
			continue
		}
		if file.UncompressedSize64 > 250<<20 {
			return nil, fmt.Errorf("EIF.exe exceeds the size limit")
		}
		reader, err := file.Open()
		if err != nil {
			return nil, fmt.Errorf("open EIF.exe in update archive: %w", err)
		}
		binary, readErr := io.ReadAll(io.LimitReader(reader, (250<<20)+1))
		closeErr := reader.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read EIF.exe from update archive: %w", readErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close EIF.exe in update archive: %w", closeErr)
		}
		if len(binary) > 250<<20 {
			return nil, fmt.Errorf("EIF.exe exceeds the size limit")
		}
		return binary, nil
	}

	return nil, fmt.Errorf("Windows update archive does not contain EIF.exe")
}
