package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/yunomix2834/eif/internal/modules/updater/internal/model"
)

const (
	checksumAssetName = "SHA256SUMS.txt"
	maxChecksumBytes  = 1 << 20
	maxUpdateBytes    = 250 << 20
)

var (
	ErrNoUpdate          = errors.New("EIF is already up to date")
	ErrInstallInProgress = errors.New("an EIF update is already being installed")
	ErrMissingAsset      = errors.New("release is missing a required update asset")
	ErrInvalidChecksum   = errors.New("update checksum verification failed")
)

type ReleaseSource interface {
	GetLatestRelease(ctx context.Context) (model.Release, error)
	DownloadAsset(ctx context.Context, downloadURL string, maxBytes int64) ([]byte, error)
}

type Installer interface {
	GetAssetName() string
	StageUpdate(asset []byte) error
}

type Service struct {
	source         ReleaseSource
	installer      Installer
	currentVersion string
	installMu      sync.Mutex
	isInstalling   bool
}

func New(
	source ReleaseSource,
	installer Installer,
	currentVersion string,
) *Service {
	return &Service{
		source:         source,
		installer:      installer,
		currentVersion: currentVersion,
	}
}

func (s *Service) CheckForUpdate(ctx context.Context) (model.UpdateInfo, error) {
	if !s.isCurrentVersionSupported() {
		return model.UpdateInfo{CurrentVersion: s.currentVersion}, nil
	}

	release, err := s.source.GetLatestRelease(ctx)
	if err != nil {
		return model.UpdateInfo{}, err
	}

	return s.buildUpdateInfo(release), nil
}

func (s *Service) InstallLatestUpdate(ctx context.Context) (model.UpdateInfo, error) {
	if !s.isCurrentVersionSupported() {
		return model.UpdateInfo{CurrentVersion: s.currentVersion}, ErrNoUpdate
	}
	if !s.beginInstall() {
		return model.UpdateInfo{}, ErrInstallInProgress
	}
	isStaged := false
	defer func() {
		if !isStaged {
			s.finishInstall()
		}
	}()

	release, err := s.source.GetLatestRelease(ctx)
	if err != nil {
		return model.UpdateInfo{}, err
	}
	info := s.buildUpdateInfo(release)
	if !info.IsUpdateAvailable {
		return info, ErrNoUpdate
	}

	asset, found := release.FindAsset(s.installer.GetAssetName())
	if !found {
		return info, fmt.Errorf("%w: %s", ErrMissingAsset, s.installer.GetAssetName())
	}
	checksums, found := release.FindAsset(checksumAssetName)
	if !found {
		return info, fmt.Errorf("%w: %s", ErrMissingAsset, checksumAssetName)
	}

	checksumData, err := s.source.DownloadAsset(ctx, checksums.DownloadURL, maxChecksumBytes)
	if err != nil {
		return info, err
	}
	updateData, err := s.source.DownloadAsset(ctx, asset.DownloadURL, maxUpdateBytes)
	if err != nil {
		return info, err
	}
	if !hasValidChecksum(updateData, asset.Name, checksumData) {
		return info, ErrInvalidChecksum
	}
	if err := s.installer.StageUpdate(updateData); err != nil {
		return info, fmt.Errorf("stage EIF update: %w", err)
	}
	isStaged = true

	return info, nil
}

func (s *Service) isCurrentVersionSupported() bool {
	_, isValid := parseVersion(s.currentVersion)
	return isValid
}

func (s *Service) buildUpdateInfo(release model.Release) model.UpdateInfo {
	return model.UpdateInfo{
		CurrentVersion:    s.currentVersion,
		LatestVersion:     release.Version,
		IsUpdateAvailable: isVersionNewer(release.Version, s.currentVersion),
		ReleaseNotesURL:   release.NotesURL,
	}
}

func (s *Service) beginInstall() bool {
	s.installMu.Lock()
	defer s.installMu.Unlock()
	if s.isInstalling {
		return false
	}
	s.isInstalling = true
	return true
}

func (s *Service) finishInstall() {
	s.installMu.Lock()
	s.isInstalling = false
	s.installMu.Unlock()
}

func hasValidChecksum(data []byte, assetName string, checksumData []byte) bool {
	expectedChecksum := ""
	for _, line := range strings.Split(string(checksumData), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		name := strings.TrimPrefix(fields[1], "*")
		if name == assetName {
			expectedChecksum = strings.ToLower(fields[0])
			break
		}
	}
	if len(expectedChecksum) != sha256.Size*2 {
		return false
	}

	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]) == expectedChecksum
}

type semanticVersion struct {
	major      int
	minor      int
	patch      int
	prerelease string
}

func isVersionNewer(latest, current string) bool {
	latestVersion, latestOK := parseVersion(latest)
	currentVersion, currentOK := parseVersion(current)
	if !latestOK || !currentOK {
		return false
	}

	latestParts := [...]int{latestVersion.major, latestVersion.minor, latestVersion.patch}
	currentParts := [...]int{currentVersion.major, currentVersion.minor, currentVersion.patch}
	for i := range latestParts {
		if latestParts[i] != currentParts[i] {
			return latestParts[i] > currentParts[i]
		}
	}
	if latestVersion.prerelease == currentVersion.prerelease {
		return false
	}
	if latestVersion.prerelease == "" {
		return true
	}
	if currentVersion.prerelease == "" {
		return false
	}

	return comparePrerelease(latestVersion.prerelease, currentVersion.prerelease) > 0
}

func parseVersion(value string) (semanticVersion, bool) {
	cleaned := strings.TrimPrefix(strings.TrimSpace(value), "v")
	cleaned = strings.SplitN(cleaned, "+", 2)[0]
	parts := strings.SplitN(cleaned, "-", 2)
	numbers := strings.Split(parts[0], ".")
	if len(numbers) != 3 {
		return semanticVersion{}, false
	}

	parsed := make([]int, 3)
	for i, number := range numbers {
		value, err := strconv.Atoi(number)
		if err != nil || value < 0 {
			return semanticVersion{}, false
		}
		parsed[i] = value
	}

	version := semanticVersion{major: parsed[0], minor: parsed[1], patch: parsed[2]}
	if len(parts) == 2 {
		if parts[1] == "" {
			return semanticVersion{}, false
		}
		version.prerelease = parts[1]
	}
	return version, true
}

func comparePrerelease(left, right string) int {
	leftParts := strings.Split(left, ".")
	rightParts := strings.Split(right, ".")
	limit := min(len(leftParts), len(rightParts))
	for i := 0; i < limit; i++ {
		if leftParts[i] == rightParts[i] {
			continue
		}
		leftNumber, leftErr := strconv.Atoi(leftParts[i])
		rightNumber, rightErr := strconv.Atoi(rightParts[i])
		switch {
		case leftErr == nil && rightErr == nil:
			if leftNumber > rightNumber {
				return 1
			}
			return -1
		case leftErr == nil:
			return -1
		case rightErr == nil:
			return 1
		case leftParts[i] > rightParts[i]:
			return 1
		default:
			return -1
		}
	}

	return len(leftParts) - len(rightParts)
}
