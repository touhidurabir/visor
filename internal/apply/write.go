// Package apply writes supervisord config files safely (backup first,
// atomic replace, refusing to overwrite edits made elsewhere) and applies
// them with reloadConfig and add/remove of the affected groups, as
// `supervisorctl update` does.
package apply

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

var (
	// ErrChangedOnDisk means the file is no longer what the caller read
	// (edited or deleted elsewhere), so the write would lose that change.
	ErrChangedOnDisk = errors.New("file changed on disk")
	// ErrNotWritable means Visor may not write the file or its folder
	// (e.g. a root-owned /etc/supervisor/conf.d).
	ErrNotWritable = errors.New("config folder is not writable")
	// ErrExists means a new file would replace one that is already there.
	ErrExists = errors.New("file already exists")
)

// newFileMode is the mode of files Write creates: supervisord, which may
// run as another user, must be able to read them.
const newFileMode = 0o644

// Hash is the hash Write expects: the hex SHA-256 of a file's content, as
// config.ConfigFile.Hash has it.
func Hash(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// Write saves content to path, an absolute path.
//
//   - expectedHash "" creates a new file: ErrExists if anything is at path.
//   - Otherwise it is the Hash of the content the caller read. A file that
//     no longer has it (edited or deleted elsewhere) is left alone with
//     ErrChangedOnDisk. The current content is backed up first, and the
//     Backup is returned.
//
// The content goes to a temp file in the same folder, is synced, and
// replaces the file by rename, so a crash leaves the old or the new file,
// never part of one. The file keeps its mode (new files get 0644). A
// symlink stays one: the file it points at is written. When the folder or
// the file is not writable, Write returns ErrNotWritable and leaves no temp
// file and no backup behind.
func Write(path string, content []byte, expectedHash string, b Backups) (Backup, error) {
	if err := checkPath(path); err != nil {
		return Backup{}, err
	}
	path = filepath.Clean(path)
	if expectedHash == "" {
		return Backup{}, create(path, content)
	}

	target, err := filepath.EvalSymlinks(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Backup{}, fmt.Errorf("%s was deleted: %w", path, ErrChangedOnDisk)
	}
	if err != nil {
		return Backup{}, err
	}
	old, err := os.ReadFile(target)
	if err != nil {
		return Backup{}, notWritable(path, err)
	}
	if Hash(old) != expectedHash {
		return Backup{}, fmt.Errorf("%s: %w", path, ErrChangedOnDisk)
	}
	fi, err := os.Stat(target)
	if err != nil {
		return Backup{}, err
	}
	if !fi.Mode().IsRegular() {
		return Backup{}, fmt.Errorf("%s is not a regular file", path)
	}
	if err := checkFileWritable(target); err != nil {
		return Backup{}, notWritable(path, err)
	}
	dir := filepath.Dir(target)
	tmp, err := tempIn(dir, target)
	if err != nil {
		return Backup{}, notWritable(path, err)
	}
	tmpName := tmp.Name()
	bk, err := b.save(path, old, false, time.Now())
	if err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return Backup{}, fmt.Errorf("cannot back up %s, so it was not changed: %w", path, err)
	}
	undo := func() {
		_ = os.Remove(tmpName)
		b.remove(bk)
	}
	if err := tmp.Chmod(fi.Mode().Perm()); err != nil {
		_ = tmp.Close()
		undo()
		return Backup{}, err
	}
	if err := writeSyncClose(tmp, content); err != nil {
		undo()
		return Backup{}, fmt.Errorf("write %s: %w", path, err)
	}
	// Last look: an edit made while the backup and temp file were written
	// still wins.
	if now, err := os.ReadFile(target); err != nil || Hash(now) != expectedHash {
		undo()
		return Backup{}, fmt.Errorf("%s: %w", path, ErrChangedOnDisk)
	}
	if err := os.Rename(tmpName, target); err != nil {
		undo()
		return Backup{}, notWritable(path, err)
	}
	syncDir(dir)
	return bk, nil
}

// create writes a new file at path without ever replacing one.
func create(path string, content []byte) error {
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("%s: %w", path, ErrExists)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	dir := filepath.Dir(path)
	tmp, err := tempIn(dir, path)
	if err != nil {
		return notWritable(path, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(newFileMode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := writeSyncClose(tmp, content); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	// A hard link fails if a file appeared at path meanwhile, where a
	// rename would replace it.
	err = os.Link(tmpName, path)
	switch {
	case errors.Is(err, fs.ErrExist):
		return fmt.Errorf("%s: %w", path, ErrExists)
	case err != nil && !errors.Is(err, fs.ErrPermission):
		// No hard links on this file system: check again and rename.
		if _, lerr := os.Lstat(path); lerr == nil {
			return fmt.Errorf("%s: %w", path, ErrExists)
		}
		err = os.Rename(tmpName, path)
	}
	if err != nil {
		return notWritable(path, err)
	}
	syncDir(dir)
	return nil
}

// Remove takes path out of the include folder: it saves a backup marked
// Removed, then deletes the file, so the backup is where it went. If the
// file cannot be deleted, the backup is dropped again and the file stays.
// A symlink is removed itself; the file it points at stays.
func Remove(path string, b Backups) (Backup, error) {
	if err := checkPath(path); err != nil {
		return Backup{}, err
	}
	path = filepath.Clean(path)
	data, err := os.ReadFile(path)
	if err != nil {
		return Backup{}, err
	}
	bk, err := b.save(path, data, true, time.Now())
	if err != nil {
		return Backup{}, fmt.Errorf("cannot back up %s, so it was not removed: %w", path, err)
	}
	if err := os.Remove(path); err != nil {
		b.remove(bk)
		return Backup{}, notWritable(path, err)
	}
	syncDir(filepath.Dir(path))
	return bk, nil
}

// CheckRemovable reports whether Remove could delete path: ErrNotWritable
// when its folder does not allow it. Callers check before stopping
// anything, so a refused remove leaves the processes as they were.
func CheckRemovable(path string) error {
	if err := checkPath(path); err != nil {
		return err
	}
	if _, err := os.Lstat(path); err != nil {
		return err
	}
	const wx = 0x2 | 0x1 // W_OK | X_OK: create and delete entries
	if err := syscall.Access(filepath.Dir(filepath.Clean(path)), wx); err != nil {
		if errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EROFS) {
			return fmt.Errorf("cannot remove %s: %w", path, ErrNotWritable)
		}
		return fmt.Errorf("cannot remove %s: %w", path, err)
	}
	return nil
}

// tempIn creates a hidden temp file next to name, in dir. Hidden, so an
// include glob such as conf.d/* does not pick it up.
func tempIn(dir, name string) (*os.File, error) {
	return os.CreateTemp(dir, "."+filepath.Base(name)+".visor-*.tmp")
}

// checkFileWritable opens path for writing without changing it.
func checkFileWritable(path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	return f.Close()
}

// notWritable maps permission and read-only file system errors to
// ErrNotWritable.
func notWritable(path string, err error) error {
	if errors.Is(err, fs.ErrPermission) || errors.Is(err, syscall.EROFS) {
		return fmt.Errorf("cannot write %s: %w", path, ErrNotWritable)
	}
	return fmt.Errorf("cannot write %s: %w", path, err)
}

func writeSyncClose(f *os.File, data []byte) error {
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// syncDir makes a rename or delete in dir durable. Best effort: some file
// systems cannot sync a folder.
func syncDir(dir string) {
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
}

// CheckWritable reports whether Write could replace the existing file at
// path: ErrNotWritable when the file or its folder (where the temp file is
// made) does not allow it. For a symlink it looks at the file it points to.
func CheckWritable(path string) error {
	if err := checkPath(path); err != nil {
		return err
	}
	target, err := filepath.EvalSymlinks(filepath.Clean(path))
	if err != nil {
		return err
	}
	const w, wx = 0x2, 0x2 | 0x1 // W_OK; W_OK | X_OK
	for name, mode := range map[string]uint32{target: w, filepath.Dir(target): wx} {
		if err := syscall.Access(name, mode); err != nil {
			if errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EROFS) {
				return fmt.Errorf("cannot write %s: %w", path, ErrNotWritable)
			}
			return fmt.Errorf("cannot write %s: %w", path, err)
		}
	}
	return nil
}
