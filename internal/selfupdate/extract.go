package selfupdate

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
)

// extractBinary copies the regular file named member out of the .tar.gz at
// archivePath into w, writing at most limit bytes. It never derives an output
// path from the archive, and it rejects archives with absolute or ".." entry
// names it reads before the member.
func extractBinary(archivePath, member string, w io.Writer, limit int64) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("read gzip: %w", err)
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return fmt.Errorf("%q not found in archive", member)
		}
		if err != nil {
			return fmt.Errorf("read tar: %w", err)
		}
		if unsafeName(hdr.Name) {
			return fmt.Errorf("archive entry %q escapes the extraction directory", hdr.Name)
		}
		if hdr.Typeflag != tar.TypeReg || path.Clean(hdr.Name) != member {
			continue
		}
		n, err := io.Copy(w, io.LimitReader(tr, limit+1))
		if err != nil {
			return fmt.Errorf("extract %s: %w", member, err)
		}
		if n > limit {
			return fmt.Errorf("extracted %s exceeds the %d byte limit", member, limit)
		}
		return nil
	}
}

func unsafeName(name string) bool {
	if strings.HasPrefix(name, "/") {
		return true
	}
	for _, segment := range strings.Split(name, "/") {
		if segment == ".." {
			return true
		}
	}
	return false
}
