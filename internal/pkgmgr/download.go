package pkgmgr

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Downloader fetches a URL into dst, writing at most limit bytes. Injectable so
// tests never touch the network.
type Downloader interface {
	Download(url string, dst io.Writer, limit int64) error
}

// HTTPDownloader downloads over HTTPS with net/http. It refuses plain-http URLs
// and redirects to them.
type HTTPDownloader struct {
	Client *http.Client
}

// NewHTTPDownloader returns a downloader with a generous overall timeout.
func NewHTTPDownloader() HTTPDownloader {
	return HTTPDownloader{Client: &http.Client{Timeout: 15 * time.Minute}}
}

// Download implements Downloader.
func (d HTTPDownloader) Download(rawURL string, dst io.Writer, limit int64) error {
	if !strings.HasPrefix(rawURL, "https://") {
		return fmt.Errorf("refusing non-https url %s", rawURL)
	}
	client := *d.Client
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" {
			return fmt.Errorf("refusing redirect to non-https url %s", req.URL)
		}
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return nil
	}
	resp, err := client.Get(rawURL)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", rawURL, resp.Status)
	}
	n, err := io.Copy(dst, io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return err
	}
	if n > limit {
		return fmt.Errorf("download exceeds the %d byte limit", limit)
	}
	return nil
}
