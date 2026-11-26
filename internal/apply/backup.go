package apply

import (
	"crypto/sha1" // a folder name per path, not a security hash
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Backup is one saved copy of a config file. ID is the UTC time it was
// taken, at nanosecond precision, in a file-name-safe RFC 3339 form
// (2026-11-01T09-30-00.123456789Z); IDs of one path are unique and sort
// in time order. Removed marks the copy taken when the file was removed
// from supervisor (moved here).
type Backup struct {
	ID      string
	Path    string
	At      time.Time
	Removed bool
}

// Backups keeps copies of config files outside the include folder, in
// Dir/<sha1(path)>/<id>.ini (or <id>.removed.ini), with meta.json naming
// the original path. Folders are 0700 and files 0600: configs may hold
// secrets in environment=.
type Backups struct{ Dir string }

const (
	idLayout      = "2006-01-02T15-04-05.000000000Z"
	backupExt     = ".ini"
	removedSuffix = ".removed"
	metaFile      = "meta.json"
)

type backupMeta struct {
	Version int    `json:"version"`
	Path    string `json:"path"`
}

// folder is where path's backups live.
func (b Backups) folder(path string) string {
	sum := sha1.Sum([]byte(path))
	return filepath.Join(b.Dir, hex.EncodeToString(sum[:]))
}

func fileName(id string, removed bool) string {
	if removed {
		return id + removedSuffix + backupExt
	}
	return id + backupExt
}

// Save copies the file at path into the backups.
func (b Backups) Save(path string, removed bool) (Backup, error) {
	if err := checkPath(path); err != nil {
		return Backup{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Backup{}, err
	}
	return b.save(filepath.Clean(path), data, removed, time.Now())
}

// save stores data as a backup of path, taken at now (moved on by a
// nanosecond at a time until the ID is free).
func (b Backups) save(path string, data []byte, removed bool, now time.Time) (Backup, error) {
	if b.Dir == "" {
		return Backup{}, errors.New("no backups folder set")
	}
	dir := b.folder(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Backup{}, fmt.Errorf("create the backups folder: %w", err)
	}
	if err := b.writeMeta(dir, path); err != nil {
		return Backup{}, err
	}
	at := now.UTC()
	for {
		id := at.Format(idLayout)
		if _, err := os.Lstat(filepath.Join(dir, fileName(id, !removed))); err == nil {
			at = at.Add(time.Nanosecond)
			continue
		}
		name := filepath.Join(dir, fileName(id, removed))
		f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, fs.ErrExist) {
			at = at.Add(time.Nanosecond)
			continue
		}
		if err != nil {
			return Backup{}, fmt.Errorf("save the backup: %w", err)
		}
		if err := writeSyncClose(f, data); err != nil {
			_ = os.Remove(name)
			return Backup{}, fmt.Errorf("save the backup: %w", err)
		}
		syncDir(dir)
		return Backup{ID: id, Path: path, At: at, Removed: removed}, nil
	}
}

// writeMeta records path in dir/meta.json once.
func (b Backups) writeMeta(dir, path string) error {
	name := filepath.Join(dir, metaFile)
	if _, err := os.Stat(name); err == nil {
		return nil
	}
	raw, err := json.Marshal(backupMeta{Version: 1, Path: path})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "meta-*.tmp")
	if err != nil {
		return fmt.Errorf("save the backup: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if err := writeSyncClose(tmp, raw); err != nil {
		return fmt.Errorf("save the backup: %w", err)
	}
	if err := os.Rename(tmp.Name(), name); err != nil {
		return fmt.Errorf("save the backup: %w", err)
	}
	return nil
}

// List returns path's backups, newest first. A path without backups has
// none (and no error).
func (b Backups) List(path string) ([]Backup, error) {
	if err := checkPath(path); err != nil {
		return nil, err
	}
	path = filepath.Clean(path)
	es, err := os.ReadDir(b.folder(path))
	if errors.Is(err, fs.ErrNotExist) {
		return []Backup{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list backups: %w", err)
	}
	out := []Backup{}
	for _, e := range es {
		id, removed, ok := parseName(e.Name())
		if !ok || !e.Type().IsRegular() {
			continue
		}
		at, _ := time.Parse(idLayout, id)
		out = append(out, Backup{ID: id, Path: path, At: at, Removed: removed})
	}
	slices.SortFunc(out, func(a, b Backup) int { return strings.Compare(b.ID, a.ID) })
	return out, nil
}

// parseName splits a backup file name into its ID and Removed flag.
func parseName(name string) (id string, removed bool, ok bool) {
	base, found := strings.CutSuffix(name, backupExt)
	if !found {
		return "", false, false
	}
	if b, r := strings.CutSuffix(base, removedSuffix); r {
		base, removed = b, true
	}
	if !validID(base) {
		return "", false, false
	}
	return base, removed, true
}

// validID reports whether id is a backup ID (so it is safe in a path).
func validID(id string) bool {
	t, err := time.Parse(idLayout, id)
	return err == nil && t.Format(idLayout) == id
}

// Removed lists, newest first, the newest backup of each file that was
// removed from supervisor (its newest backup is marked Removed) and is not
// on d now: the files a Restore can bring back.
func (b Backups) Removed(d Disk) ([]Backup, error) {
	es, err := os.ReadDir(b.Dir)
	if errors.Is(err, fs.ErrNotExist) {
		return []Backup{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list backups: %w", err)
	}
	out := []Backup{}
	for _, e := range es {
		if !e.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(b.Dir, e.Name(), metaFile))
		if err != nil {
			continue
		}
		var m backupMeta
		if json.Unmarshal(raw, &m) != nil || checkPath(m.Path) != nil || b.folder(filepath.Clean(m.Path)) != filepath.Join(b.Dir, e.Name()) {
			continue // not a folder Visor wrote for that path
		}
		list, err := b.List(m.Path)
		if err != nil || len(list) == 0 || !list[0].Removed {
			continue
		}
		if there, err := d.Exists(m.Path); there || err != nil {
			continue
		}
		out = append(out, list[0])
	}
	slices.SortFunc(out, func(x, y Backup) int { return y.At.Compare(x.At) })
	return out, nil
}

// modeExt names the file beside a removal backup that records the removed
// file's permissions, so a restore doesn't make it readable by everyone.
const modeExt = ".mode"

// saveMode records perm as the mode of the file bk was taken of; 0 records
// nothing.
func (b Backups) saveMode(bk Backup, perm fs.FileMode) error {
	if perm == 0 {
		return nil
	}
	name, err := b.file(bk)
	if err != nil {
		return err
	}
	return os.WriteFile(name+modeExt, []byte(strconv.FormatUint(uint64(perm.Perm()), 8)), 0o600)
}

// Mode is the mode of the file a removal backup was taken of, or 0 when the
// backup didn't record one (backups taken before Visor recorded modes).
func (b Backups) Mode(bk Backup) fs.FileMode {
	name, err := b.file(bk)
	if err != nil {
		return 0
	}
	raw, err := os.ReadFile(name + modeExt)
	if err != nil {
		return 0
	}
	n, err := strconv.ParseUint(strings.TrimSpace(string(raw)), 8, 32)
	if err != nil {
		return 0
	}
	return fs.FileMode(n).Perm()
}

// Read returns the content of a backup.
func (b Backups) Read(bk Backup) ([]byte, error) {
	name, err := b.file(bk)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(name)
}

// Discard deletes a backup that was taken for a write or removal that did
// not happen, and the path's folder with it if that was its only backup.
func (b Backups) Discard(bk Backup) {
	name, err := b.file(bk)
	if err != nil {
		return
	}
	_ = os.Remove(name)
	_ = os.Remove(name + modeExt)
	if left, err := b.List(bk.Path); err == nil && len(left) == 0 {
		dir := filepath.Dir(name)
		_ = os.Remove(filepath.Join(dir, metaFile))
		_ = os.Remove(dir) // only if empty
	}
}

func (b Backups) file(bk Backup) (string, error) {
	if err := checkPath(bk.Path); err != nil {
		return "", err
	}
	if !validID(bk.ID) {
		return "", fmt.Errorf("%q is not a backup", bk.ID)
	}
	return filepath.Join(b.folder(filepath.Clean(bk.Path)), fileName(bk.ID, bk.Removed)), nil
}

func checkPath(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("%q is not an absolute path", path)
	}
	return nil
}
