//go:build windows

package config

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

const (
	cryptProtectUIForbidden = 0x1
	windowsErrorInvalidData = syscall.Errno(13)
)

var (
	crypt32DLL           = syscall.NewLazyDLL("crypt32.dll")
	kernel32DLL          = syscall.NewLazyDLL("kernel32.dll")
	cryptProtectData     = crypt32DLL.NewProc("CryptProtectData")
	cryptUnprotectData   = crypt32DLL.NewProc("CryptUnprotectData")
	localFree            = kernel32DLL.NewProc("LocalFree")
	dpapiOptionalEntropy = []byte("YunoTools EIF session-key v1")
)

type dataBlob struct {
	Size uint32
	Data *byte
}

func getDefaultServerHost() string {
	return "127.0.0.1"
}

func shouldOpenBrowserByDefault() bool {
	return true
}

func getDefaultSessionStorePath() string {
	base, err := os.UserCacheDir()
	if err != nil || base == "" {
		base = "."
	}
	return filepath.Join(
		base,
		"YunoTools",
		"EIF",
		"hddtgdt-sessions.enc",
	)
}

func loadOrCreatePlatformSessionKey(sessionStorePath string) (
	[]byte,
	error,
) {
	keyPath := sessionStorePath + ".key.dpapi"
	protected, err := os.ReadFile(keyPath)
	if err == nil {
		plain, unprotectErr := dpapiUnprotect(protected)
		if unprotectErr == nil && len(plain) == 32 {
			return plain, nil
		}

		// ERROR_INVALID_DATA means this DPAPI blob no longer belongs to the
		// current Windows user/profile (or it was corrupted). The encrypted
		// session store cannot be recovered without that key, so reset only the
		// local encrypted session state and let the user authenticate again.
		if unprotectErr != nil && !errors.Is(
			unprotectErr,
			windowsErrorInvalidData,
		) {
			return nil, fmt.Errorf(
				"unprotect local EIF session key: %w",
				unprotectErr,
			)
		}
		if resetErr := resetUnreadableLocalSessionState(
			sessionStorePath,
			keyPath,
		); resetErr != nil {
			return nil, fmt.Errorf(
				"reset unreadable local EIF session state: %w",
				resetErr,
			)
		}
		return createPlatformSessionKey(keyPath)
	}
	if !errors.Is(
		err,
		os.ErrNotExist,
	) {
		return nil, fmt.Errorf(
			"read local EIF session key: %w",
			err,
		)
	}

	// A session store without its DPAPI key is unusable. Remove that orphaned
	// ciphertext before creating a new key so startup does not fail while trying
	// to decrypt it with an unrelated key.
	if err := removeIfExists(sessionStorePath); err != nil {
		return nil, fmt.Errorf(
			"remove orphaned EIF session store: %w",
			err,
		)
	}
	return createPlatformSessionKey(keyPath)
}

func createPlatformSessionKey(keyPath string) (
	[]byte,
	error,
) {
	plain := make(
		[]byte,
		32,
	)
	if _, err := rand.Read(plain); err != nil {
		return nil, fmt.Errorf(
			"generate local EIF session key: %w",
			err,
		)
	}

	protected, err := dpapiProtect(plain)
	if err != nil {
		return nil, fmt.Errorf(
			"protect local EIF session key: %w",
			err,
		)
	}
	if err := writePrivateFile(
		keyPath,
		protected,
	); err != nil {
		return nil, fmt.Errorf(
			"persist local EIF session key: %w",
			err,
		)
	}
	return plain, nil
}

func resetUnreadableLocalSessionState(sessionStorePath, keyPath string) error {
	if err := removeIfExists(sessionStorePath); err != nil {
		return err
	}
	return removeIfExists(keyPath)
}

func removeIfExists(path string) error {
	err := os.Remove(path)
	if err == nil || errors.Is(
		err,
		os.ErrNotExist,
	) {
		return nil
	}
	return err
}

func dpapiProtect(data []byte) (
	[]byte,
	error,
) {
	return dpapiCall(
		cryptProtectData,
		data,
	)
}

func dpapiUnprotect(data []byte) (
	[]byte,
	error,
) {
	return dpapiCall(
		cryptUnprotectData,
		data,
	)
}

func dpapiCall(
	proc *syscall.LazyProc,
	data []byte,
) (
	[]byte,
	error,
) {
	input := bytesBlob(data)
	entropy := bytesBlob(dpapiOptionalEntropy)
	var output dataBlob

	r1, _, callErr := proc.Call(
		uintptr(unsafe.Pointer(&input)),
		0,
		uintptr(unsafe.Pointer(&entropy)),
		0,
		0,
		cryptProtectUIForbidden,
		uintptr(unsafe.Pointer(&output)),
	)
	if r1 == 0 {
		if callErr != syscall.Errno(0) {
			return nil, callErr
		}
		return nil, syscall.EINVAL
	}
	if output.Data == nil || output.Size == 0 {
		return nil, fmt.Errorf("DPAPI returned empty data")
	}
	defer localFree.Call(uintptr(unsafe.Pointer(output.Data)))

	result := append(
		[]byte(nil),
		unsafe.Slice(
			output.Data,
			int(output.Size),
		)...,
	)
	return result, nil
}

func bytesBlob(data []byte) dataBlob {
	if len(data) == 0 {
		return dataBlob{}
	}
	return dataBlob{Size: uint32(len(data)), Data: &data[0]}
}

func writePrivateFile(
	path string,
	data []byte,
) error {
	if err := os.MkdirAll(
		filepath.Dir(path),
		0700,
	); err != nil {
		return err
	}

	temp, err := os.CreateTemp(
		filepath.Dir(path),
		".eif-session-key-*",
	)
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)

	if err := temp.Chmod(0600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}

	if err := os.Rename(
		tempPath,
		path,
	); err == nil {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(
		err,
		os.ErrNotExist,
	) {
		return err
	}
	return os.Rename(
		tempPath,
		path,
	)
}
