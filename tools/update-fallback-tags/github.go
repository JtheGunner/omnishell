package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Releases looks up the SHA-256 digests GitHub reports for a release's assets.
// Injectable so tests need no network.
type Releases interface {
	// AssetDigests returns asset file name -> lowercase hex sha256 for the
	// release of tag in repo. Assets without a sha256 digest are omitted.
	AssetDigests(repo, tag string) (map[string]string, error)
}

type githubReleases struct {
	client  *http.Client
	apiBase string
	token   string
}

func newGitHubReleases(token string) githubReleases {
	return githubReleases{client: &http.Client{Timeout: time.Minute}, apiBase: "https://api.github.com", token: token}
}

func (g githubReleases) AssetDigests(repo, tag string) (map[string]string, error) {
	slug, ok := strings.CutPrefix(repo, "https://github.com/")
	slug = strings.TrimSuffix(strings.TrimSuffix(slug, "/"), ".git")
	if !ok || strings.Count(slug, "/") != 1 {
		return nil, fmt.Errorf("%s is not a GitHub repository", repo)
	}
	endpoint := fmt.Sprintf("%s/repos/%s/releases/tags/%s", g.apiBase, slug, url.PathEscape(tag))
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if g.token != "" {
		req.Header.Set("Authorization", "Bearer "+g.token)
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", endpoint, resp.Status)
	}
	var body struct {
		Assets []struct {
			Name   string  `json:"name"`
			Digest *string `json:"digest"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode %s: %w", endpoint, err)
	}
	digests := map[string]string{}
	for _, a := range body.Assets {
		if a.Digest == nil {
			continue
		}
		if hexSum, ok := strings.CutPrefix(*a.Digest, "sha256:"); ok {
			digests[a.Name] = strings.ToLower(hexSum)
		}
	}
	return digests, nil
}
