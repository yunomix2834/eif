//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"
)

// Win 32 Flags
const (
	messageBoxOK        = 0x00000000 // Message Box OK chỉ có nút OK
	messageBoxIconError = 0x00000010 // Message Box Error hiển thị icon ❌
	messageBoxTopmost   = 0x00040000 // Message Box Topmost nằm trên các cửa sổ bình thường
)

var (
	user32DLL = syscall.NewLazyDLL("user32.dll") // user32.dll là Windows system library chứa UI APIs.
	// Chữ W: Unicode/wide-character version (hiển thị tiếng việt tốt)
	// Chữ A: ANSI
	messageBox = user32DLL.NewProc("MessageBoxW")
)

func showStartupError(err error) {
	if err == nil {
		return
	}

	// Ghi diagnostic log trước
	logPath := writeStartupErrorLog(err)
	message := "EIF could not start.\r\n\r\n" + err.Error()
	if logPath != "" {
		// \r\n là Windows line ending: CRLF
		message += "\r\n\r\nDiagnostic log:\r\n" + logPath
	}
	message += "\r\n\r\nClose this message, fix the problem, then open EIF.exe again."

	titlePtr, titleErr := syscall.UTF16PtrFromString("EIF - Startup error")
	messagePtr, messageErr := syscall.UTF16PtrFromString(message)
	if titleErr != nil || messageErr != nil {
		return
	}

	// cần unsafe vì Go pointer *uint16 không thể truyền thẳng cho raw Windows syscall.
	// Phải convert:
	// *uint16
	//  ↓
	// unsafe.Pointer
	//  ↓
	// uintptr
	// để Win32 nhận memory address.
	messageBox.Call(
		0,
		uintptr(unsafe.Pointer(messagePtr)),
		uintptr(unsafe.Pointer(titlePtr)),
		messageBoxOK|messageBoxIconError|messageBoxTopmost,
	)
}

// diagnostic log
func writeStartupErrorLog(err error) string {
	base, baseErr := os.UserCacheDir()
	if baseErr != nil || base == "" {
		return ""
	}

	// Ví dụ: C:\Users\yunom\AppData\Local\
	// YunoTools\EIF\startup-error.log
	dir := filepath.Join(
		base,
		"YunoTools",
		"EIF",
	)
	if mkdirErr := os.MkdirAll(
		dir,
		0700,
	); mkdirErr != nil {
		return ""
	}

	path := filepath.Join(
		dir,
		"startup-error.log",
	)

	// 2026-09-08T21:47:00+07:00 -> RFC3339
	// 2026-09-08    → ngày
	// T             → ngăn cách ngày và giờ
	// 21:47:00      → giờ
	// +07:00        → múi giờ UTC+7
	content := fmt.Sprintf(
		"time=%s\r\nerror=%s\r\n",
		time.Now().Format(time.RFC3339),
		err.Error(),
	)

	if writeErr := os.WriteFile(
		path,
		[]byte(content),
		0600,
	); writeErr != nil {
		return ""
	}

	return path
}
