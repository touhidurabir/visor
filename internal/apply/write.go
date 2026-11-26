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
	// ErrUnknownOutcome means the change was sent but no answer came back
	// (a server's connection dropped or timed out): it may have been made,
	// so its backup is kept.
	ErrUnknownOutcome = errors.New("no answer came back, so the change may or may not have been made; Visor kept its backup")
)

// newFileMode is the mode of new files (perm 0 to Create): supervisord,
// which may run as another user, must be able to read them.
const newFileMode = 0o644

// Hash is the hash Write expects: the hex SHA-256 of a file's content, as
// config.ConfigFile.Hash has it.
func Hash(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// Disk is the machine whose config files Write, Revert and Remove change:
// Local (this one), or a server's through Visor's helper. Paths are
// absolute and clean.
type Disk interface {
	// ReadFile reads a file, following symlinks.
	ReadFile(path string) ([]byte, error)
	// Exists reports whether anything, a symlink too, is at path.
	Exists(path string) (bool, error)
	// Replace swaps the content of the file at path (the file a symlink
	// points to) for content if it still hashes to expectedHash
	// (ErrChangedOnDisk otherwise, a deleted file too), atomically,
	// keeping the file's mode.
	Replace(path string, content []byte, expectedHash string) error
	// Create writes a new file with mode perm (0644 when 0), never
	// replacing one (ErrExists).
	Create(path string, content []byte, perm fs.FileMode) error
	// Stat describes path, following symlinks.
	Stat(path string) (fs.FileInfo, error)
	// Delete removes path, a symlink itself, if what it reads as still
	// hashes to expectedHash (ErrChangedOnDisk otherwise).
	Delete(path string, expectedHash string) error
	// CheckWritable is ErrNotWritable when Replace could not write path.
	CheckWritable(path string) error
	// CheckRemovable is ErrNotWritable when Delete could not remove path.
	CheckRemovable(path string) error
}

// Local is this machine's disk.
var Local Disk = localDisk{}

type localDisk struct{}

// Write saves content to path, an absolute path, on d.
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
func Write(d Disk, path string, content []byte, expectedHash string, b Backups) (Backup, error) {
	if err := checkPath(path); err != nil {
		return Backup{}, err
	}
	path = filepath.Clean(path)
	if expectedHash == "" {
		return Backup{}, d.Create(path, content, 0)
	}
	return replace(d, path, content, expectedHash, &b)
}

// Create writes a new file at path, an absolute path, on d with mode perm
// (0644 when 0): ErrExists if anything is at path. Restoring a removed file
// passes the mode its backup recorded (Backups.Mode).
func Create(d Disk, path string, content []byte, perm fs.FileMode) error {
	if err := checkPath(path); err != nil {
		return err
	}
	return d.Create(filepath.Clean(path), content, perm)
}

// Revert undoes a Write that supervisord refused: it puts prev back at
// path, as Write does but without a new backup, if the file still holds
// written (ErrChangedOnDisk otherwise, and nothing changes), then drops
// bk, the backup that Write took of prev.
func Revert(d Disk, path string, prev, written []byte, bk Backup, b Backups) error {
	if err := checkPath(path); err != nil {
		return err
	}
	if _, err := replace(d, filepath.Clean(path), prev, Hash(written), nil); err != nil {
		return err
	}
	b.Discard(bk)
	return nil
}

// RevertCreate undoes a Write that created path: it deletes the file if it
// still holds written (ErrChangedOnDisk otherwise, and it stays).
func RevertCreate(d Disk, path string, written []byte) error {
	if err := checkPath(path); err != nil {
		return err
	}
	return d.Delete(filepath.Clean(path), Hash(written))
}

// replace is Write of an existing file; b nil takes no backup. It checks
// the hash and that the file is writable before the backup, so a refused
// write leaves no backup behind; d.Replace checks the hash again.
func replace(d Disk, path string, content []byte, expectedHash string, b *Backups) (Backup, error) {
	old, err := d.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Backup{}, fmt.Errorf("%s was deleted: %w", path, ErrChangedOnDisk)
	}
	if err != nil {
		return Backup{}, notWritable(path, err)
	}
	if Hash(old) != expectedHash {
		return Backup{}, fmt.Errorf("%s: %w", path, ErrChangedOnDisk)
	}
	if err := d.CheckWritable(path); err != nil {
		return Backup{}, err
	}
	var bk Backup
	if b != nil {
		if bk, err = b.save(path, old, false, time.Now()); err != nil {
			return Backup{}, fmt.Errorf("cannot back up %s, so it was not changed: %w", path, err)
		}
	}
	if err := d.Replace(path, content, expectedHash); err != nil {
		if b != nil && !errors.Is(err, ErrUnknownOutcome) {
			b.Discard(bk)
		}
		return Backup{}, err
	}
	return bk, nil
}

func (localDisk) ReadFile(path string) ([]byte, error) { return os.ReadFile(path) }

func (localDisk) Exists(path string) (bool, error) {
	_, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

func (localDisk) CheckWritable(path string) error  { return CheckWritable(path) }
func (localDisk) CheckRemovable(path string) error { return CheckRemovable(path) }
func (localDisk) Create(path string, content []byte, perm fs.FileMode) error {
	return create(path, content, perm)
}

func (localDisk) Stat(path string) (fs.FileInfo, error) { return os.Stat(path) }

func (localDisk) Delete(path, expectedHash string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if Hash(data) != expectedHash {
		return fmt.Errorf("%s: %w", path, ErrChangedOnDisk)
	}
	if err := os.Remove(path); err != nil {
		return notWritable(path, err)
	}
	syncDir(filepath.Dir(path))
	return nil
}

func (localDisk) Replace(path string, content []byte, expectedHash string) error {
	target, err := filepath.EvalSymlinks(path)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%s was deleted: %w", path, ErrChangedOnDisk)
	}
	if err != nil {
		return err
	}
	fi, err := os.Stat(target)
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", path)
	}
	if err := checkFileWritable(target); err != nil {
		return notWritable(path, err)
	}
	dir := filepath.Dir(target)
	tmp, err := tempIn(dir, target)
	if err != nil {
		return notWritable(path, err)
	}
	tmpName := tmp.Name()
	if err := tmp.Chmod(fi.Mode().Perm()); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := writeSyncClose(tmp, content); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("write %s: %w", path, err)
	}
	// Last look: an edit made while the temp file was written still wins.
	if now, err := os.ReadFile(target); err != nil || Hash(now) != expectedHash {
		_ = os.Remove(tmpName)
		return fmt.Errorf("%s: %w", path, ErrChangedOnDisk)
	}
	if err := os.Rename(tmpName, target); err != nil {
		_ = os.Remove(tmpName)
		return notWritable(path, err)
	}
	syncDir(dir)
	return nil
}

// create writes a new file at path without ever replacing one.
func create(path string, content []byte, perm fs.FileMode) error {
	if perm == 0 {
		perm = newFileMode
	}
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
	if err := tmp.Chmod(perm.Perm()); err != nil {
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
func Remove(d Disk, path string, b Backups) (Backup, error) {
	bk, err := BackUpForRemove(d, path, b)
	if err != nil {
		return Backup{}, err
	}
	return RemoveBackedUp(d, path, bk, b)
}

// BackUpForRemove is Remove's first half, for callers that must know the
// backup is safe before they stop anything: it saves path's content as a
// backup marked Removed and changes nothing else. Finish with
// RemoveBackedUp, or drop the backup with Discard.
func BackUpForRemove(d Disk, path string, b Backups) (Backup, error) {
	if err := checkPath(path); err != nil {
		return Backup{}, err
	}
	path = filepath.Clean(path)
	data, err := d.ReadFile(path)
	if err != nil {
		return Backup{}, err
	}
	fi, err := d.Stat(path)
	if err != nil {
		return Backup{}, err
	}
	bk, err := b.save(path, data, true, time.Now())
	if err == nil {
		if err = b.saveMode(bk, fi.Mode().Perm()); err != nil {
			b.Discard(bk)
		}
	}
	if err != nil {
		return Backup{}, fmt.Errorf("cannot back up %s, so it was not removed: %w", path, err)
	}
	return bk, nil
}

// RemoveBackedUp is Remove's second half: it deletes path, whose backup bk
// BackUpForRemove took. A file edited since gets a fresh backup in its
// place, so the backup always holds what was removed. If the file cannot
// be deleted, the backup is dropped and the file stays. It returns the
// backup that holds the file.
func RemoveBackedUp(d Disk, path string, bk Backup, b Backups) (Backup, error) {
	if err := checkPath(path); err != nil {
		return Backup{}, err
	}
	path = filepath.Clean(path)
	data, err := d.ReadFile(path)
	if err != nil {
		b.Discard(bk)
		return Backup{}, err
	}
	if saved, err := b.Read(bk); err != nil || Hash(saved) != Hash(data) {
		fresh, err := b.save(path, data, true, time.Now())
		if err == nil {
			if err = b.saveMode(fresh, b.Mode(bk)); err != nil {
				b.Discard(fresh)
			}
		}
		if err != nil {
			b.Discard(bk)
			return Backup{}, fmt.Errorf("cannot back up %s, so it was not removed: %w", path, err)
		}
		b.Discard(bk)
		bk = fresh
	}
	if err := d.Delete(path, Hash(data)); err != nil {
		if !errors.Is(err, ErrUnknownOutcome) {
			b.Discard(bk)
		}
		return Backup{}, err
	}
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
