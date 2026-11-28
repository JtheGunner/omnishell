package selfupdate

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// LatestResolver reports the tag of the newest release.
type LatestResolver interface {
	Latest() (string, error)
}

var tagPattern = regexp.MustCompile(`^[0-9A-Za-z._-]+$`)

// RedirectResolver resolves the latest tag from the redirect of the releases
// "latest" URL, like install.sh does. The REST API is rate limited per IP; the
// redirect is not. The redirect is never followed: only its Location header is
// read, and it must stay on the host of the requested URL.
type RedirectResolver struct {
	// URL is the https address of the "latest" redirect.
	URL    string
	Client *http.Client
}

// NewRedirectResolver returns a resolver with a short timeout.
func NewRedirectResolver(latestURL string) RedirectResolver {
	return RedirectResolver{URL: latestURL, Client: &http.Client{Timeout: 30 * time.Second}}
}

// Latest implements LatestResolver.
func (r RedirectResolver) Latest() (string, error) {
	base, err := url.Parse(r.URL)
	if err != nil || base.Scheme != "https" {
		return "", fmt.Errorf("refusing non-https url %s", r.URL)
	}
	client := *r.Client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, err := http.NewRequest(http.MethodHead, r.URL, http.NoBody)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("could not determine latest release: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	loc, err := resp.Location()
	if err != nil {
		hint := ""
		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
			hint = "; GitHub is rate limiting this IP"
		}
		return "", fmt.Errorf("could not determine latest release: HTTP %d from %s%s", resp.StatusCode, r.URL, hint)
	}
	if loc.Scheme != "https" || loc.Host != base.Host {
		return "", fmt.Errorf("could not determine latest release: unexpected redirect to %s", loc)
	}
	_, tag, found := strings.Cut(loc.Path, "/releases/tag/")
	if !found || !tagPattern.MatchString(tag) {
		return "", errors.New("could not determine latest release: redirect does not point to a release tag")
	}
	return tag, nil
}
