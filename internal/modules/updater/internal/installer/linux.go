//go:build !windows

package installer

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
)

const linuxAssetName = "eif-linux-amd64"

type LinuxInstaller struct {
	executablePath string
	processID      int
}

func New() (*LinuxInstaller, error) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		return nil, fmt.Errorf("%w: %s/%s", ErrUnsupportedPlatform, runtime.GOOS, runtime.GOARCH)
	}
	executablePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("locate the EIF executable: %w", err)
	}
	executablePath, err = filepath.Abs(executablePath)
	if err != nil {
		return nil, fmt.Errorf("resolve the EIF executable path: %w", err)
	}

	return &LinuxInstaller{
		executablePath: executablePath,
		processID:      os.Getpid(),
	}, nil
}

func (i *LinuxInstaller) GetAssetName() string {
	return linuxAssetName
}

func (i *LinuxInstaller) StageUpdate(asset []byte) error {
	dir := filepath.Dir(i.executablePath)
	pendingFile, err := os.CreateTemp(dir, ".eif-update-*")
	if err != nil {
		return fmt.Errorf("create staged executable: %w", err)
	}
	pendingPath := pendingFile.Name()
	defer func() {
		if err != nil {
			os.Remove(pendingPath)
		}
	}()
	if err = pendingFile.Chmod(0755); err != nil {
		pendingFile.Close()
		return fmt.Errorf("make staged executable runnable: %w", err)
	}
	if _, err = pendingFile.Write(asset); err != nil {
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

	helperFile, err := os.CreateTemp(dir, ".eif-update-*.sh")
	if err != nil {
		return fmt.Errorf("create update helper: %w", err)
	}
	helperPath := helperFile.Name()
	helper := `#!/bin/sh
target_pid=$1
current_path=$2
pending_path=$3
while kill -0 "$target_pid" 2>/dev/null; do
  sleep 1
done
mv -f -- "$pending_path" "$current_path" || exit 1
chmod 0755 "$current_path" || exit 1
EIF_OPEN_BROWSER=false nohup "$current_path" >/dev/null 2>&1 &
rm -f -- "$0"
`
	if _, err = io.WriteString(helperFile, helper); err != nil {
		helperFile.Close()
		os.Remove(helperPath)
		return fmt.Errorf("write update helper: %w", err)
	}
	if err = helperFile.Chmod(0700); err != nil {
		helperFile.Close()
		os.Remove(helperPath)
		return fmt.Errorf("make update helper runnable: %w", err)
	}
	if err = helperFile.Close(); err != nil {
		os.Remove(helperPath)
		return fmt.Errorf("close update helper: %w", err)
	}

	command := exec.Command(
		"/bin/sh",
		helperPath,
		strconv.Itoa(i.processID),
		i.executablePath,
		pendingPath,
	)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err = command.Start(); err != nil {
		os.Remove(helperPath)
		return fmt.Errorf("start update helper: %w", err)
	}
	_ = command.Process.Release()

	return nil
}
