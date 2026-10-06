package pkgmgr

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path"
)

// extractMember copies the single file named member out of a .tar.gz or .zip
// archive into w, writing at most limit bytes. It only matches the named entry
// and never derives an output path from the archive, so archive entry names
// cannot escape anywhere.
func extractMember(archivePath, format, member string, w io.Writer, limit int64) error {
	switch format {
	case "tar.gz":
		return extractTarGz(archivePath, member, w, limit)
	case "zip":
		return extractZip(archivePath, member, w, limit)
	}
	return fmt.Errorf("unsupported archive format %q", format)
}

func extractTarGz(archivePath, member string, w io.Writer, limit int64) error {
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
			break
		}
		if err != nil {
			return fmt.Errorf("read tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg || path.Clean(hdr.Name) != member {
			continue
		}
		return copyLimited(w, tr, limit)
	}
	return fmt.Errorf("member %q not found in archive", member)
}

func extractZip(archivePath, member string, w io.Writer, limit int64) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("read zip: %w", err)
	}
	defer func() { _ = zr.Close() }()
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || path.Clean(f.Name) != member {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("open %s: %w", f.Name, err)
		}
		defer func() { _ = rc.Close() }()
		return copyLimited(w, rc, limit)
	}
	return fmt.Errorf("member %q not found in archive", member)
}

// copyLimited copies r to w and fails once more than limit bytes arrive, so a
// compressed bomb cannot fill the disk.
func copyLimited(w io.Writer, r io.Reader, limit int64) error {
	n, err := io.Copy(w, io.LimitReader(r, limit+1))
	if err != nil {
		return err
	}
	if n > limit {
		return fmt.Errorf("extracted file exceeds the %d byte limit", limit)
	}
	return nil
}
