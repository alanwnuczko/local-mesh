package transfer

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/alanwnuczko/local-mesh/internal/config"
)

// TarEntry is one filesystem object captured in a folder snapshot so a later
// stream can reproduce the same tar headers. Content is re-read on each pass.
type TarEntry struct {
	FSPath  string
	Name    string // tar name, forward slashes, directories end with /
	Size    int64
	Mode    os.FileMode
	ModTime int64 // Unix seconds — tar headers are second-precision
	IsDir   bool
}

// FolderPlan is a snapshot of a directory used to hash then stream an identical
// tar without staging a temp archive on disk. Stream aborts if a file's size or
// mtime changed since the snapshot.
type FolderPlan struct {
	Root     string
	Entries  []TarEntry
	Size     int64
	Checksum string
}

// PlanFolder walks srcDir and hashes the tar it would produce. The returned
// plan can later Stream the same bytes, or abort if the tree changed.
func PlanFolder(srcDir string) (*FolderPlan, error) {
	entries, err := snapshotFolder(srcDir)
	if err != nil {
		return nil, err
	}
	h := sha256.New()
	cw := &countingWriter{w: h}
	if err := writeTar(entries, cw, false); err != nil {
		return nil, err
	}
	return &FolderPlan{
		Root:     filepath.Clean(srcDir),
		Entries:  entries,
		Size:     cw.n,
		Checksum: hex.EncodeToString(h.Sum(nil)),
	}, nil
}

// Stream writes the planned tar to w. Regular files are re-read; if size or
// mtime no longer match the snapshot, Stream returns an error instead of
// producing a tar that would fail the receiver checksum.
func (p *FolderPlan) Stream(w io.Writer) error {
	if p == nil {
		return fmt.Errorf("nil folder plan")
	}
	return writeTar(p.Entries, w, true)
}

// TarFolder writes a tar stream of the directory at srcDir into w.
// Files are stored with paths relative to srcDir's parent, so the
// archive contains the folder itself (e.g., "myfolder/file.txt").
func TarFolder(srcDir string, w io.Writer) error {
	entries, err := snapshotFolder(srcDir)
	if err != nil {
		return err
	}
	return writeTar(entries, w, false)
}

func snapshotFolder(srcDir string) ([]TarEntry, error) {
	srcDir = filepath.Clean(srcDir)
	var entries []TarEntry
	err := filepath.WalkDir(srcDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return nil
		}

		rel, err := filepath.Rel(filepath.Dir(srcDir), p)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		size := info.Size()
		if info.IsDir() {
			size = 0
			if !strings.HasSuffix(name, "/") {
				name += "/"
			}
		}

		entries = append(entries, TarEntry{
			FSPath:  p,
			Name:    name,
			Size:    size,
			Mode:    info.Mode(),
			ModTime: info.ModTime().Unix(),
			IsDir:   info.IsDir(),
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("snapshot folder: %w", err)
	}
	return entries, nil
}

func writeTar(entries []TarEntry, w io.Writer, verify bool) error {
	tw := tar.NewWriter(w)
	for _, e := range entries {
		hdr := &tar.Header{
			Name:    e.Name,
			Mode:    int64(e.Mode.Perm()),
			Size:    e.Size,
			ModTime: time.Unix(e.ModTime, 0).UTC(),
		}
		if e.IsDir {
			hdr.Typeflag = tar.TypeDir
			hdr.Size = 0
			if !strings.HasSuffix(hdr.Name, "/") {
				hdr.Name += "/"
			}
		} else {
			hdr.Typeflag = tar.TypeReg
			if verify {
				if err := assertUnchanged(e); err != nil {
					return err
				}
			}
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if e.IsDir {
			continue
		}
		f, err := os.Open(e.FSPath)
		if err != nil {
			return err
		}
		n, err := io.Copy(tw, io.LimitReader(f, e.Size))
		f.Close()
		if err != nil {
			return err
		}
		if n != e.Size {
			return fmt.Errorf("file size changed during transfer: %s", e.FSPath)
		}
	}
	if err := tw.Close(); err != nil {
		return fmt.Errorf("tar close: %w", err)
	}
	return nil
}

func assertUnchanged(e TarEntry) error {
	info, err := os.Stat(e.FSPath)
	if err != nil {
		return fmt.Errorf("file changed during transfer: %s: %w", e.FSPath, err)
	}
	if info.Size() != e.Size || info.ModTime().Unix() != e.ModTime {
		return fmt.Errorf("file changed during transfer: %s", e.FSPath)
	}
	return nil
}

// UntarFolder extracts a tar stream from r into destDir.
// Entries whose resolved path would escape destDir are rejected.
// On any error, a partial extraction may exist in destDir (caller deletes it).
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

		target, err := confinedTarTarget(destDir, hdr.Name)
		if err != nil {
			return err
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0644)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
		default:
			// Skip symlinks, devices, hard links, etc. for v1.
		}
	}
	return nil
}

func confinedTarTarget(destDir, name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("tar entry has empty path")
	}
	// Normalise separators then reuse the shared confinement helper so tar
	// names and UniqueDestPath obey the same rules.
	rel := strings.ReplaceAll(name, "\\", "/")
	rel = strings.TrimPrefix(rel, "./")
	if rel == "." || rel == "" {
		return filepath.Abs(destDir)
	}
	// path.Clean keeps a trailing-slash directory as a file-like name; strip
	// it so ConfineRel joins a directory name, not "foo/".
	rel = strings.TrimSuffix(rel, "/")
	if rel == "" {
		return filepath.Abs(destDir)
	}
	if path.IsAbs(rel) {
		return "", fmt.Errorf("tar entry has unsafe path: %q", name)
	}
	target, err := config.ConfineRel(destDir, rel)
	if err != nil {
		return "", fmt.Errorf("tar entry has unsafe path: %q", name)
	}
	return target, nil
}
