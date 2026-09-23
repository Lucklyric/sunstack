package setup

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var client = &http.Client{Timeout: 60 * time.Second}

// LatestVersion asks GitHub for the newest release tag, without the "v".
func LatestVersion() (string, error) {
	resp, err := client.Get("https://api.github.com/repos/" + Repo + "/releases/latest")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub releases: %s", resp.Status)
	}
	var r struct {
		Tag string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", err
	}
	return strings.TrimPrefix(r.Tag, "v"), nil
}

func archiveName() string {
	ext := ".tar.gz"
	if runtime.GOOS == "windows" {
		ext = ".zip"
	}
	return fmt.Sprintf("sunstack_%s_%s%s", runtime.GOOS, runtime.GOARCH, ext)
}

func fetch(url string) ([]byte, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// SelfUpdate replaces the running binary with release tag v<version> after
// checking it against the release's checksums.txt.
func SelfUpdate(version string) error {
	base := "https://github.com/" + Repo + "/releases/download/v" + version + "/"
	name := archiveName()
	sums, err := fetch(base + "checksums.txt")
	if err != nil {
		return err
	}
	want := ""
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		if f := strings.Fields(sc.Text()); len(f) == 2 && f[1] == name {
			want = f[0]
		}
	}
	if want == "" {
		return fmt.Errorf("no checksum for %s in the release", name)
	}
	arc, err := fetch(base + name)
	if err != nil {
		return err
	}
	got := sha256.Sum256(arc)
	if hex.EncodeToString(got[:]) != want {
		return fmt.Errorf("checksum mismatch for %s", name)
	}
	bin, err := extractBinary(arc)
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return err
	}
	// Stage in a uniquely named file beside the binary, so the final rename is
	// atomic and concurrent updates cannot clobber each other's staging file.
	tmp, err := os.CreateTemp(filepath.Dir(exe), ".sunstack-new-*")
	if err != nil {
		return err
	}
	staged := tmp.Name()
	defer os.Remove(staged)
	if _, err := tmp.Write(bin); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(staged, 0o755); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		return os.Rename(staged, exe)
	}
	// Windows cannot overwrite a running executable, but it can rename it.
	// Move it aside, put the new one in place, and move it back on failure.
	old := fmt.Sprintf("%s.old-%d", exe, time.Now().UnixNano())
	if err := os.Rename(exe, old); err != nil {
		return err
	}
	if err := os.Rename(staged, exe); err != nil {
		if rerr := os.Rename(old, exe); rerr != nil {
			return fmt.Errorf("%v; restoring the old binary also failed, it is at %s", err, old)
		}
		return err
	}
	return nil
}

func extractBinary(arc []byte) ([]byte, error) {
	want := "sunstack"
	if runtime.GOOS == "windows" {
		want = "sunstack.exe"
		zr, err := zip.NewReader(bytes.NewReader(arc), int64(len(arc)))
		if err != nil {
			return nil, err
		}
		for _, f := range zr.File {
			if filepath.Base(f.Name) == want {
				rc, err := f.Open()
				if err != nil {
					return nil, err
				}
				defer rc.Close()
				return io.ReadAll(rc)
			}
		}
		return nil, fmt.Errorf("%s not found in archive", want)
	}
	gz, err := gzip.NewReader(bytes.NewReader(arc))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err != nil {
			return nil, fmt.Errorf("%s not found in archive", want)
		}
		if filepath.Base(h.Name) == want && h.Typeflag == tar.TypeReg {
			return io.ReadAll(tr)
		}
	}
}
