package session

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	sessionStoreVersion       = 2
	legacySessionStoreVersion = 1
)

// "dấu nhận diện file".
// File thật:
// EIFSESSION1
// [random nonce]
// [encrypted data]
// Mục đích:
// Tránh trường hợp: config.json bị đưa nhầm vào decrypt.
// hoặc: file khác bị đưa nhầm vào decrypt.
const sessionStoreMagic = "EIFSESSION1"

var (
	ErrNotFound = errors.New("session not found")
	ErrExpired  = errors.New("session expired")

	// errUnreadableSessionStore là lỗi đặc biệt
	// File bị:
	// 	- corrupt
	// 	- sai key
	// 	- sai format
	// 	- JSON lỗi
	// Thì đánh dấu: session store không đọc được
	errUnreadableSessionStore = errors.New("unreadable session store")
)

type sessionStore struct {
	Version  int       `json:"version"`
	Sessions []Session `json:"sessions"`
}

type Manager struct {
	mu            sync.RWMutex
	items         map[string]*Session
	usernames     map[string]string // mapping username -> session ID -> Không phải loop toàn bộ session khi tìm username.
	skew          time.Duration     // khoảng an toàn trước lúc token thật sự hết hạn
	storePath     string
	encryptionKey []byte
}

func NewManager(
	skew time.Duration,
	storePath string,
	encryptionKey []byte,
) (
	*Manager,
	error,
) {
	if len(encryptionKey) != 32 {
		return nil, fmt.Errorf("session encryption key must be exactly 32 bytes")
	}

	m := &Manager{
		items:     make(map[string]*Session),
		usernames: make(map[string]string),
		skew:      skew,
		storePath: storePath,
		encryptionKey: append(
			[]byte(nil),
			encryptionKey...,
		), // deep copy. Tránh caller sửa slice bên ngoài -> Manager bị đổi key
	}

	// load session
	if err := m.load(); err != nil {
		// shouldResetUnreadableSessionStore windows desktop mode
		// DPAPI quản lý key có thể reset
		// Ví dụ:
		// session file cũ + key mới = không decrypt được
		// Thay vì: app chết
		// Thì:
		// 	- xóa cache session
		// 	- login lại
		if shouldResetUnreadableSessionStore(err) {
			if resetErr := removeUnreadableSessionStore(storePath); resetErr != nil {
				return nil, fmt.Errorf(
					"reset unreadable session store: %w",
					resetErr,
				)
			}
			slog.Warn(
				"unreadable local HDDT GDT session store reset; sign-in is required again",
				"store_path",
				storePath,
				"error",
				err,
			)
			return m, nil
		}
		return nil, err
	}

	return m, nil
}

func shouldResetUnreadableSessionStore(err error) bool {
	return runtime.GOOS == "windows" &&
		strings.TrimSpace(os.Getenv("EIF_SESSION_ENCRYPTION_KEY")) == "" &&
		errors.Is(
			err,
			errUnreadableSessionStore,
		)
}

func removeUnreadableSessionStore(path string) error {
	err := os.Remove(path)
	if err == nil || errors.Is(
		err,
		os.ErrNotExist,
	) {
		return nil
	}
	return err
}

func (m *Manager) Create(
	username string,
	token string,
	cookies []Cookie,
	expiredAt time.Time,
) (
	*Session,
	error,
) {
	username = strings.TrimSpace(username)

	if username == "" {
		return nil, errors.New("session username is required")
	}

	if token == "" {
		return nil, errors.New("session token is required")
	}

	if len(cookies) == 0 {
		return nil, errors.New("session cookies are required")
	}

	if time.Now().Add(m.skew).After(expiredAt) {
		return nil, ErrExpired
	}

	id, err := newSessionID()
	if err != nil {
		return nil, err
	}

	sess := &Session{
		ID:        id,
		Username:  username,
		Token:     token,
		Cookies:   cloneCookies(cookies),
		ExpiredAt: expiredAt,
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.deleteExpiredLocked(time.Now())

	// Mỗi username HDDT GDT chỉ giữ một session hiện hành.
	// Nếu user authenticate lại thì session cũ sẽ bị thay thế.
	// Giữ previous session để có thể rollback.
	var previous *Session
	if previousID, ok := m.usernames[username]; ok {
		if previousSession, exists := m.items[previousID]; exists {
			previousCopy := *previousSession
			previousCopy.Cookies = cloneCookies(previousSession.Cookies)
			previous = &previousCopy
		}
		m.deleteLocked(previousID)
	}

	m.items[sess.ID] = sess
	m.usernames[sess.Username] = sess.ID

	// Persist đồng bộ trước khi trả session về FE để session đã cấp
	// vẫn có thể được restore nếu backend restart ngay sau đó.
	if err := m.persistLocked(); err != nil {
		m.deleteLocked(sess.ID)
		if previous != nil {
			previousCopy := *previous
			m.items[previousCopy.ID] = &previousCopy
			m.usernames[previousCopy.Username] = previousCopy.ID
		}
		return nil, err
	}

	sessClone := *sess
	sessClone.Cookies = cloneCookies(sess.Cookies)

	return &sessClone, nil
}

func (m *Manager) Refresh(
	id string,
	token string,
	cookies []Cookie,
	expiredAt time.Time,
) (
	*Session,
	error,
) {
	if id == "" {
		return nil, ErrNotFound
	}

	if token == "" {
		return nil, errors.New("session token is required")
	}

	if len(cookies) == 0 {
		return nil, errors.New("session cookies are required")
	}

	if time.Now().Add(m.skew).After(expiredAt) {
		return nil, ErrExpired
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	sess, ok := m.items[id]
	if !ok {
		return nil, ErrNotFound
	}

	previous := *sess
	previous.Cookies = cloneCookies(sess.Cookies)

	sess.Token = token
	sess.Cookies = cloneCookies(cookies)
	sess.ExpiredAt = expiredAt

	if err := m.persistLocked(); err != nil {
		*sess = previous
		return nil, err
	}

	clone := *sess
	clone.Cookies = cloneCookies(sess.Cookies)
	return &clone, nil
}

func (m *Manager) Get(id string) (
	*Session,
	error,
) {
	if id == "" {
		return nil, ErrNotFound
	}

	m.mu.RLock()
	sess, ok := m.items[id]
	if ok {
		sessCopy := *sess
		sessCopy.Cookies = cloneCookies(sess.Cookies)
		m.mu.RUnlock()
		if time.Now().Add(m.skew).After(sessCopy.ExpiredAt) {
			m.mu.Lock()
			m.deleteLocked(id)
			if err := m.persistLocked(); err != nil {
				slog.Error(
					"persist expired HDDT GDT session failed",
					"session_id",
					id,
					"error",
					err,
				)
			}
			m.mu.Unlock()
			return nil, ErrExpired
		}
		return &sessCopy, nil
	}
	m.mu.RUnlock()

	return nil, ErrNotFound
}

func (m *Manager) GetByUsername(username string) (
	*Session,
	error,
) {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil, ErrNotFound
	}

	m.mu.RLock()
	id, ok := m.usernames[username]
	m.mu.RUnlock()
	if !ok {
		return nil, ErrNotFound
	}

	return m.Get(id)
}

func (m *Manager) UpdateCookies(
	id string,
	cookies []Cookie,
) error {
	if id == "" {
		return ErrNotFound
	}
	if len(cookies) == 0 {
		return errors.New("session cookies are required")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	sess, ok := m.items[id]
	if !ok {
		return ErrNotFound
	}
	if slices.Equal(sess.Cookies, cookies) {
		return nil
	}

	previous := cloneCookies(sess.Cookies)
	sess.Cookies = cloneCookies(cookies)

	if err := m.persistLocked(); err != nil {
		sess.Cookies = previous
		return err
	}

	return nil
}

func (m *Manager) Delete(id string) error {
	if id == "" {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	sess, ok := m.items[id]
	if !ok {
		return nil
	}

	sessCopy := *sess
	sessCopy.Cookies = cloneCookies(sess.Cookies)
	m.deleteLocked(id)

	if err := m.persistLocked(); err != nil {
		m.items[sessCopy.ID] = &sessCopy
		m.usernames[sessCopy.Username] = sessCopy.ID
		return err
	}

	return nil
}

func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return len(m.items)
}

func (m *Manager) deleteLocked(id string) {
	sess, ok := m.items[id]
	if !ok {
		return
	}

	delete(
		m.items,
		id,
	)
	if currentID, exists := m.usernames[sess.Username]; exists && currentID == id {
		delete(
			m.usernames,
			sess.Username,
		)
	}
}

func (m *Manager) deleteExpiredLocked(now time.Time) bool {
	deleted := false
	threshold := now.Add(m.skew)
	for id, sess := range m.items {
		if threshold.After(sess.ExpiredAt) {
			m.deleteLocked(id)
			deleted = true
		}
	}

	return deleted
}

func (m *Manager) load() error {
	data, err := os.ReadFile(m.storePath)
	if errors.Is(
		err,
		os.ErrNotExist,
	) {
		return nil
	}

	if err != nil {
		return fmt.Errorf(
			"read session store: %w",
			err,
		)
	}

	plainText, err := m.decrypt(data)
	if err != nil {
		return fmt.Errorf(
			"%w: decrypt session store: %v",
			errUnreadableSessionStore,
			err,
		)
	}

	var store sessionStore
	if err := json.Unmarshal(
		plainText,
		&store,
	); err != nil {
		return fmt.Errorf(
			"%w: decode session store: %v",
			errUnreadableSessionStore,
			err,
		)
	}

	if store.Version != sessionStoreVersion &&
		store.Version != legacySessionStoreVersion {
		return fmt.Errorf(
			"%w: unsupported session store version %d",
			errUnreadableSessionStore,
			store.Version,
		)
	}

	dirty := store.Version != sessionStoreVersion
	threshold := time.Now().Add(m.skew)
	for _, storedSession := range store.Sessions {
		storedSession.Username = strings.TrimSpace(storedSession.Username)
		if storedSession.ID == "" ||
			storedSession.Username == "" ||
			storedSession.Token == "" ||
			len(storedSession.Cookies) == 0 ||
			threshold.After(storedSession.ExpiredAt) {
			dirty = true
			continue
		}

		// Username là unique key.
		// Nếu file có duplicate do dữ liệu cũ,
		// thì giữ session có expired_at xa hơn.
		if previousID, ok := m.usernames[storedSession.Username]; ok {
			previous := m.items[previousID]
			if previous != nil && !storedSession.ExpiredAt.After(previous.ExpiredAt) {
				dirty = true
				continue
			}
			m.deleteLocked(previousID)
			dirty = true
		}

		storedSessionCopy := storedSession
		m.items[storedSessionCopy.ID] = &storedSessionCopy
		m.usernames[storedSessionCopy.Username] = storedSessionCopy.ID
	}

	// Sau khi restore, ghi lại file nếu có session hết hạn hoặc duplicate
	// để disk store luôn đồng bộ với state trong memory.
	if dirty {
		if err := m.persistLocked(); err != nil {
			return fmt.Errorf(
				"persist cleaned session store: %w",
				err,
			)
		}
	}

	return nil
}

func (m *Manager) persistLocked() error {
	sessions := make(
		[]Session,
		0,
		len(m.items),
	)
	for _, sess := range m.items {
		sessions = append(
			sessions,
			*sess,
		)
	}

	sort.Slice(
		sessions,
		func(i, j int) bool {
			return sessions[i].ID < sessions[j].ID
		},
	)

	plainText, err := json.Marshal(
		sessionStore{
			Version:  sessionStoreVersion,
			Sessions: sessions,
		},
	)

	if err != nil {
		return fmt.Errorf(
			"encode session store: %w",
			err,
		)
	}

	encrypted, err := m.encrypt(plainText)
	if err != nil {
		return fmt.Errorf(
			"encrypt session store: %w",
			err,
		)
	}

	if err := writeFileAtomic(
		m.storePath,
		encrypted,
	); err != nil {
		return fmt.Errorf(
			"write session store: %w",
			err,
		)
	}

	return nil
}

func (m *Manager) encrypt(plainText []byte) (
	[]byte,
	error,
) {
	gcm, err := newGCM(m.encryptionKey)
	if err != nil {
		return nil, err
	}

	nonce := make(
		[]byte,
		gcm.NonceSize(),
	)
	if _, err := io.ReadFull(
		rand.Reader,
		nonce,
	); err != nil {
		return nil, err
	}

	magic := []byte(sessionStoreMagic)
	cipherText := gcm.Seal(
		nil,
		nonce,
		plainText,
		magic,
	)

	data := make(
		[]byte,
		0,
		len(magic)+len(nonce)+len(cipherText),
	)
	data = append(
		data,
		magic...,
	)
	data = append(
		data,
		nonce...,
	)
	data = append(
		data,
		cipherText...,
	)

	return data, nil
}

func (m *Manager) decrypt(data []byte) (
	[]byte,
	error,
) {
	gcm, err := newGCM(m.encryptionKey)
	if err != nil {
		return nil, err
	}

	magic := []byte(sessionStoreMagic)
	minimumLength := len(magic) + gcm.NonceSize() + gcm.Overhead()
	if len(data) < minimumLength {
		return nil, errors.New("invalid encrypted session store")
	}

	if string(data[:len(magic)]) != sessionStoreMagic {
		return nil, errors.New("invalid session store header")
	}

	nonceStart := len(magic)
	nonceEnd := nonceStart + gcm.NonceSize()
	nonce := data[nonceStart:nonceEnd]
	cipherText := data[nonceEnd:]

	plainText, err := gcm.Open(
		nil,
		nonce,
		cipherText,
		magic,
	)
	if err != nil {
		return nil, err
	}

	return plainText, nil
}

func newGCM(key []byte) (
	cipher.AEAD,
	error,
) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	return cipher.NewGCM(block)
}

func writeFileAtomic(
	path string,
	data []byte,
) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(
		dir,
		0700,
	); err != nil {
		return err
	}

	file, err := os.CreateTemp(
		dir,
		".hddtgdt-sessions-*",
	)
	if err != nil {
		return err
	}

	tempPath := file.Name()
	defer os.Remove(tempPath)

	if err := file.Chmod(0600); err != nil {
		file.Close()
		return err
	}

	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}

	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}

	if err := file.Close(); err != nil {
		return err
	}

	renameErr := os.Rename(
		tempPath,
		path,
	)
	if renameErr == nil {
		return nil
	}

	// Windows không cho rename đè file đã tồn tại.
	// Linux vẫn giữ nguyên cơ chế rename atomic phía trên.
	if runtime.GOOS != "windows" {
		return renameErr
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

func newSessionID() (
	string,
	error,
) {
	buf := make(
		[]byte,
		32,
	)

	if _, err := rand.Read(buf); err != nil {
		return "", err
	}

	return hex.EncodeToString(buf), nil
}

func cloneCookies(source []Cookie) []Cookie {
	if len(source) == 0 {
		return nil
	}

	result := make(
		[]Cookie,
		len(source),
	)
	copy(
		result,
		source,
	)

	return result
}
