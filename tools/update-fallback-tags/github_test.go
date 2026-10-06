package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGitHubReleasesReadsAssetDigests(t *testing.T) {
	var gotPath, gotAuth string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"assets":[
			{"name":"a.tar.gz","digest":"sha256:` + strings.Repeat("c", 64) + `"},
			{"name":"b.zip","digest":null},
			{"name":"c","digest":"md5:zzz"}]}`))
	}))
	defer srv.Close()
	g := githubReleases{client: srv.Client(), apiBase: srv.URL, token: "tok"}

	got, err := g.AssetDigests("https://github.com/o/tool.git", "v1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/repos/o/tool/releases/tags/v1.2.3" || gotAuth != "Bearer tok" {
		t.Fatalf("request path=%q auth=%q", gotPath, gotAuth)
	}
	if len(got) != 1 || got["a.tar.gz"] != strings.Repeat("c", 64) {
		t.Fatalf("digests = %v, want only the sha256 one", got)
	}
}

func TestGitHubReleasesRejectsNonGitHubRepos(t *testing.T) {
	g := githubReleases{client: http.DefaultClient, apiBase: "https://unused"}
	if _, err := g.AssetDigests("https://gitlab.com/o/tool", "v1"); err == nil {
		t.Fatal("want an error for a non-GitHub repository")
	}
}

func TestGitHubReleasesReportsHTTPErrors(t *testing.T) {
	srv := httptest.NewTLSServer(http.NotFoundHandler())
	defer srv.Close()
	g := githubReleases{client: srv.Client(), apiBase: srv.URL}
	if _, err := g.AssetDigests("https://github.com/o/tool", "v9"); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("err = %v, want the 404 reported", err)
	}
}
