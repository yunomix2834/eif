package source

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/yunotools/eif/internal/modules/updater/internal/model"
)

const (
	githubAPIBase    = "https://api.github.com"
	maxMetadataBytes = 2 << 20
)

type GitHubSource struct {
	httpClient *http.Client
	repository string
}

type githubRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

func NewGitHubSource(repository string, timeout time.Duration) *GitHubSource {
	return &GitHubSource{
		httpClient: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(request *http.Request, _ []*http.Request) error {
				if !isTrustedGitHubURL(request.URL) {
					return fmt.Errorf("GitHub redirected to an unsafe URL")
				}
				return nil
			},
		},
		repository: repository,
	}
}

func (s *GitHubSource) GetLatestRelease(ctx context.Context) (model.Release, error) {
	requestURL := fmt.Sprintf(
		"%s/repos/%s/releases/latest",
		githubAPIBase,
		s.repository,
	)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return model.Release{}, fmt.Errorf("create GitHub release request: %w", err)
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "EIF-Updater")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	response, err := s.httpClient.Do(request)
	if err != nil {
		return model.Release{}, fmt.Errorf("request latest GitHub release: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 8<<10))
		return model.Release{}, fmt.Errorf(
			"GitHub release request returned %s: %s",
			response.Status,
			strings.TrimSpace(string(body)),
		)
	}

	metadata, err := io.ReadAll(io.LimitReader(response.Body, maxMetadataBytes+1))
	if err != nil {
		return model.Release{}, fmt.Errorf("read GitHub release metadata: %w", err)
	}
	if len(metadata) > maxMetadataBytes {
		return model.Release{}, fmt.Errorf("GitHub release metadata exceeds the size limit")
	}

	var payload githubRelease
	if err := json.Unmarshal(metadata, &payload); err != nil {
		return model.Release{}, fmt.Errorf("decode GitHub release: %w", err)
	}
	if strings.TrimSpace(payload.TagName) == "" {
		return model.Release{}, fmt.Errorf("GitHub release is missing a version tag")
	}

	release := model.Release{
		Version:  payload.TagName,
		NotesURL: payload.HTMLURL,
		Assets:   make([]model.Asset, 0, len(payload.Assets)),
	}
	for _, asset := range payload.Assets {
		release.Assets = append(release.Assets, model.Asset{
			Name:        asset.Name,
			DownloadURL: asset.BrowserDownloadURL,
		})
	}

	return release, nil
}

func (s *GitHubSource) DownloadAsset(
	ctx context.Context,
	downloadURL string,
	maxBytes int64,
) ([]byte, error) {
	parsedURL, err := url.Parse(downloadURL)
	if err != nil || parsedURL.Scheme != "https" || parsedURL.Host != "github.com" {
		return nil, fmt.Errorf("GitHub returned an unsafe asset URL")
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create asset request: %w", err)
	}
	request.Header.Set("Accept", "application/octet-stream")
	request.Header.Set("User-Agent", "EIF-Updater")

	response, err := s.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("download update asset: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("asset download returned %s", response.Status)
	}
	if response.ContentLength > maxBytes {
		return nil, fmt.Errorf("update asset exceeds the size limit")
	}

	data, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read update asset: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("update asset exceeds the size limit")
	}

	return data, nil
}

func isTrustedGitHubURL(candidate *url.URL) bool {
	if candidate == nil || candidate.Scheme != "https" {
		return false
	}

	host := strings.ToLower(candidate.Hostname())
	return host == "github.com" ||
		host == "api.github.com" ||
		strings.HasSuffix(host, ".githubusercontent.com")
}
