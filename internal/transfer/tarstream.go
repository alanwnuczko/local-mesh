package transfer

import (
	"archive/tar"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// TarFolder writes a tar stream of the directory at srcDir into w.
// Files are stored with paths relative to srcDir's parent, so the
// archive contains the folder itself (e.g., "myfolder/file.txt").
func TarFolder(srcDir string, w io.Writer) error {
	srcDir = filepath.Clean(srcDir)
	base := filepath.Base(srcDir)
	tw := tar.NewWriter(w)

	// P-5: use WalkDir so we receive d.Type() before stat-ing, which lets us
	// skip symlinks without following them. filepath.Walk follows symlinks by
	// default and would include whatever they point to (e.g. /etc/passwd).
	err := filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip symlinks entirely.
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}

		// Compute the path inside the tar relative to srcDir's parent.
		rel, err := filepath.Rel(filepath.Dir(srcDir), path)
		if err != nil {
			return err
		}
		// Normalise separator to forward slash for portability.
		rel = filepath.ToSlash(rel)
		_ = base // used via rel which already includes the folder name

		info, err := d.Info()
		if err != nil {
			return err
		}

		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = rel
		if info.IsDir() {
			hdr.Name += "/"
		}

		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()

		if _, err := io.Copy(tw, f); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("tar folder walk: %w", err)
	}
	return tw.Close()
}

// UntarFolder extracts a tar stream from r into destDir.
// It enforces path safety: entries with absolute paths or ".." components are
// rejected to prevent directory traversal.
// On any error, a partial extraction may exist in destDir.
func UntarFolder(r io.Reader, destDir string) error {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar read: %w", err)
		}

		// Safety: reject absolute paths and ".." traversal.
		if filepath.IsAbs(hdr.Name) || strings.Contains(hdr.Name, "..") {
			return fmt.Errorf("tar entry has unsafe path: %q", hdr.Name)
		}

		target := filepath.Join(destDir, filepath.FromSlash(hdr.Name))

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			f, err := os.Create(target)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			f.Close()
		default:
			// Skip symlinks, devices, etc. for v1.
		}
	}
	return nil
}
