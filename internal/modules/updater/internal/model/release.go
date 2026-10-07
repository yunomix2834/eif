package model

type Asset struct {
	Name        string
	DownloadURL string
}

type Release struct {
	Version  string
	NotesURL string
	Assets   []Asset
}

func (r Release) FindAsset(name string) (Asset, bool) {
	for _, asset := range r.Assets {
		if asset.Name == name {
			return asset, true
		}
	}

	return Asset{}, false
}

type UpdateInfo struct {
	CurrentVersion    string `json:"current_version"`
	LatestVersion     string `json:"latest_version"`
	IsUpdateAvailable bool   `json:"is_update_available"`
	ReleaseNotesURL   string `json:"release_notes_url,omitempty"`
}
