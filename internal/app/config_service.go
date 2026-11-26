package app

import (
	"errors"
	"fmt"
	"github.com/touhidurabir/visor/internal/remote"
	"github.com/wailsapp/wails/v3/pkg/application"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/touhidurabir/visor/internal/apply"
	"github.com/touhidurabir/visor/internal/config"
	"github.com/touhidurabir/visor/internal/describe"
	"github.com/touhidurabir/visor/internal/platform"
	"github.com/touhidurabir/visor/internal/supervisor"
	"github.com/touhidurabir/visor/internal/validate"
)

// ErrInvalidConfig means a save was refused because its checks found
// errors. The checks are in Validate's answer.
var ErrInvalidConfig = errors.New("config has errors")

// ErrRejected means supervisord refused a saved file when it re-read its
// config (CANT_REREAD), so Visor put the previous version back (or removed
// a new file again). The error says what supervisord objected to.
var ErrRejected = errors.New("supervisord refused this config")

// ErrMainConfig means the path is supervisord.conf itself (directly, or
// through a symlink or an include glob that matches it): Visor never edits
// supervisord's own settings.
var ErrMainConfig = errors.New("this is supervisord's main config file, which Visor doesn't edit")

// maxBackupsListed bounds how many backups Backups returns (newest first).
const maxBackupsListed = 50

// ConfigService edits supervisord's include files: read, check, describe,
// save and apply, back up and restore. Writes and applies are one at a
// time, together with ServerService's (Runtime.applyMu).
type ConfigService struct {
	rt *Runtime
}

// NewConfigService returns a service backed by rt.
func NewConfigService(rt *Runtime) *ConfigService {
	return &ConfigService{rt: rt}
}

// FileView is a config file as read from disk. Hash identifies this
// content: Save refuses to write when the file no longer has it.
// Writable is false when Visor may not replace the file.
type FileView struct {
	Path, Content, Hash string
	Writable            bool
}

// LoadedDiffView holds the two texts to compare for "changed on disk vs.
// what supervisord is running"; the frontend computes the line diff.
// Known is false when Visor holds no copy of what supervisord loaded
// (apply to be sure).
type LoadedDiffView struct {
	Known        bool
	Loaded, Disk string
	// Confirmed: supervisord was just asked and still runs something else.
	// CheckError: asking it failed.
	Confirmed  bool
	CheckError string
}

// RemovedView is a config file removed from supervisor that is not back:
// BackupID is the backup Restore brings it back from, taken At.
type RemovedView struct {
	Path, FileName, BackupID string
	At                       time.Time
}

// BackupView is one saved copy of a file. At is when it was taken.
// Removed marks the copy made when the file was removed from supervisor.
type BackupView struct {
	ID      string
	At      time.Time
	Removed bool
	Content string
}

// Read returns an include file's content, hash and whether it is writable.
func (s *ConfigService) Read(path string) (FileView, error) {
	if _, err := s.rt.includeFile(path); err != nil {
		return FileView{}, err
	}
	data, err := s.rt.files().ReadFile(path)
	if err != nil {
		return FileView{}, fmt.Errorf("read %s: %w", path, err)
	}
	return FileView{
		Path: path, Content: string(data), Hash: apply.Hash(data),
		Writable: s.rt.CheckConfigWrite() == nil && s.rt.disk().CheckWritable(path) == nil,
	}, nil
}

// Validate checks content as the file named fileName. selfPath is the file
// being edited ("" for a new one), so its own programs are not duplicates.
func (s *ConfigService) Validate(fileName, content, selfPath string) []validate.Check {
	return validate.File(fileName, content, s.rt.validateEnv(selfPath))
}

// Describe explains each program in content in plain English.
func (s *ConfigService) Describe(content string) ([]describe.Program, error) {
	return describe.File(content)
}

// Explain explains content line by line as the file fileName: what each
// line means, its problems and how to fix them, and the paths it points
// to. selfPath is the file on disk being edited ("" for a new one).
func (s *ConfigService) Explain(fileName, content, selfPath string) ExplainView {
	return s.rt.explain(fileName, content, selfPath)
}

// LoadedDiff returns what supervisord loaded from path (Visor's snapshot)
// and what is on disk now. When path is flagged as changed, supervisord is
// asked first whether it already runs it (not on a Read only server); if
// it does, path is recorded as loaded and no longer flagged.
func (s *ConfigService) LoadedDiff(path string) (LoadedDiffView, error) {
	if _, err := s.rt.includeFile(path); err != nil {
		return LoadedDiffView{}, err
	}
	var v LoadedDiffView
	if s.rt.flagged(path) {
		if ac, err := s.rt.conn(); err == nil {
			asked, err := s.rt.confirmFlagged(ac.ctx, ac.client, s.rt.changeSet(), true)
			if err != nil {
				v.CheckError = err.Error()
			} else {
				v.Confirmed = asked && s.rt.flagged(path)
			}
			if asked {
				s.rt.emitOverview()
			}
		} else {
			v.CheckError = err.Error()
		}
	}
	disk, err := s.rt.files().ReadFile(path)
	if err != nil {
		return LoadedDiffView{}, fmt.Errorf("read %s: %w", path, err)
	}
	v.Disk = string(disk)
	if w := s.rt.watcherOrNil(); w != nil {
		if loaded, ok := w.Loaded(path); ok {
			v.Known, v.Loaded = true, string(loaded)
		}
	}
	return v, nil
}

// Save writes content to path if the file still has hash (ErrChangedOnDisk
// otherwise; the file is then untouched), after a backup, and applies the
// groups of the file (what it defined before and after) with reloadConfig,
// so other programs keep running. Errors in the checks refuse the save with
// ErrInvalidConfig; a file Visor may not write fails with ErrNotWritable.
// Per-group apply failures are in the result's Errors.
//
// When supervisord then refuses the file (it can't re-read its config),
// the previous version is put back and the error is ErrRejected.
func (s *ConfigService) Save(path, content, hash string) (ApplyResultView, error) {
	if err := s.rt.CheckConfigWrite(); err != nil {
		return ApplyResultView{}, err
	}
	s.rt.applyMu.Lock()
	defer s.rt.applyMu.Unlock()
	defer s.rt.markWrites()() // its file changes are Visor's, not outside ones
	return s.save(path, content, hash)
}

func (s *ConfigService) save(path, content, hash string) (ApplyResultView, error) {
	if hash == "" {
		return ApplyResultView{}, errors.New("save needs the hash of the content you edited")
	}
	if _, err := s.rt.includeFile(path); err != nil {
		return ApplyResultView{}, err
	}
	// Without a connection nothing could be applied (and a server's checks
	// can't look there): refuse first, with that reason.
	if _, err := s.rt.conn(); err != nil {
		return ApplyResultView{}, err
	}
	if cs := s.Validate(filepath.Base(path), content, path); validate.HasErrors(cs) {
		return ApplyResultView{}, invalidError(cs)
	}
	old, err := s.rt.files().ReadFile(path)
	if err != nil {
		return ApplyResultView{}, fmt.Errorf("read %s: %w", path, err)
	}
	b, d := s.rt.backups(), s.rt.disk()
	bk, err := apply.Write(d, path, []byte(content), hash, b)
	if err != nil {
		return ApplyResultView{}, err
	}
	// What supervisord loaded from the file may differ from the disk copy
	// just replaced (changed elsewhere, not applied): apply its groups too.
	loaded, _ := s.rt.loadedSnapshot(path)
	res, err := s.applyWritten(path, old, loaded)
	if refused(err) {
		if rerr := apply.Revert(d, path, old, []byte(content), bk, b); rerr != nil {
			return res, fmt.Errorf("%w: %s. Visor could not put the previous version back (%v): the backup from %s has it",
				ErrRejected, faultText(err), rerr, bk.At.Local().Format(time.DateTime))
		}
		return res, fmt.Errorf("%w: %s. Visor put the previous version back; your edits are still in the editor", ErrRejected, faultText(err))
	}
	return res, err
}

// refused reports whether supervisord could not re-read its config.
func refused(err error) bool { return errors.Is(err, supervisor.ErrCantReread) }

// faultText is supervisord's own words for a refused re-read, without the
// list of every name it could have expanded (its whole environment).
func faultText(err error) string {
	var f *supervisor.Fault
	if !errors.As(err, &f) {
		return err.Error()
	}
	t := strings.TrimSpace(strings.TrimPrefix(f.String, "CANT_REREAD:"))
	if i := strings.Index(t, ". Available names: "); i >= 0 {
		rest := ""
		if j := strings.Index(t[i:], " in section "); j >= 0 {
			rest = t[i+j:]
		}
		t = t[:i] + rest
	}
	return t
}

// applyWritten applies the groups path defines now and the ones it
// defined in each of olds (earlier versions of it).
func (s *ConfigService) applyWritten(path string, olds ...[]byte) (ApplyResultView, error) {
	groups, err := s.rt.groupsInFile(path)
	if err != nil {
		return ApplyResultView{}, fmt.Errorf("saved %s, but could not apply it: %w", path, err)
	}
	for _, old := range olds {
		for _, g := range groupsIn(old, path) {
			if !slices.Contains(groups, g) {
				groups = append(groups, g)
			}
		}
	}
	res, err := s.rt.applyChanges(groups)
	if err != nil {
		return toApplyResultView(res), fmt.Errorf("saved %s, but applying it failed: %w", filepath.Base(path), err)
	}
	return toApplyResultView(res), nil
}

// ApplyFile applies a file that is already on disk (edited elsewhere) to
// supervisord: only the groups it defines now, and the ones supervisord
// loaded from it before.
func (s *ConfigService) ApplyFile(path string) (ApplyResultView, error) {
	if err := s.rt.CheckConfigWrite(); err != nil {
		return ApplyResultView{}, err
	}
	s.rt.applyMu.Lock()
	defer s.rt.applyMu.Unlock()
	defer s.rt.markWrites()() // its file changes are Visor's, not outside ones
	if _, err := s.rt.includeFile(path); err != nil {
		return ApplyResultView{}, err
	}
	loaded, _ := s.rt.loadedSnapshot(path)
	return s.applyWritten(path, loaded)
}

// LoadFile has supervisord add the programs of the include file at path
// that it doesn't know yet (a file added or extended outside Visor), as
// supervisorctl update <group> would for just those groups. Groups it
// already runs are left as they are, even when they changed on disk.
func (s *ConfigService) LoadFile(path string) (ApplyResultView, error) {
	if err := s.rt.CheckConfigWrite(); err != nil {
		return ApplyResultView{}, err
	}
	ac, err := s.rt.conn()
	if err != nil {
		return ApplyResultView{}, err
	}
	groups, err := s.rt.groupsInFile(path)
	if err != nil {
		return ApplyResultView{}, err
	}
	unloaded, err := s.rt.unloadedGroups(ac, groups)
	if err != nil {
		return ApplyResultView{}, err
	}
	if len(unloaded) == 0 {
		return ApplyResultView{}, nil
	}
	return s.rt.loadGroups(path, unloaded)
}

// Backups lists path's saved copies, newest first (at most 50).
func (s *ConfigService) Backups(path string) ([]BackupView, error) {
	if err := s.rt.checkManaged(path); err != nil {
		return nil, err
	}
	b := s.rt.backups()
	list, err := b.List(path)
	if err != nil {
		return nil, err
	}
	out := make([]BackupView, 0, len(list))
	for _, bk := range list[:min(len(list), maxBackupsListed)] {
		data, err := b.Read(bk)
		if err != nil {
			continue // vanished since the listing
		}
		out = append(out, BackupView{ID: bk.ID, At: bk.At, Removed: bk.Removed, Content: string(data)})
	}
	return out, nil
}

// Restore puts a backup back through the same path as Save: the checks,
// a backup of what is there now, and an apply of the file's groups. A file
// removed from supervisor comes back as a new file.
func (s *ConfigService) Restore(path, backupID string) (ApplyResultView, error) {
	if err := s.rt.CheckConfigWrite(); err != nil {
		return ApplyResultView{}, err
	}
	s.rt.applyMu.Lock()
	defer s.rt.applyMu.Unlock()
	defer s.rt.markWrites()() // its file changes are Visor's, not outside ones
	if err := s.rt.checkManaged(path); err != nil {
		return ApplyResultView{}, err
	}
	b := s.rt.backups()
	list, err := b.List(path)
	if err != nil {
		return ApplyResultView{}, err
	}
	i := slices.IndexFunc(list, func(bk apply.Backup) bool { return bk.ID == backupID })
	if i < 0 {
		return ApplyResultView{}, fmt.Errorf("there is no backup %q of %s", backupID, path)
	}
	data, err := b.Read(list[i])
	if err != nil {
		return ApplyResultView{}, err
	}
	cur, err := s.rt.files().ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		// The removal's backup (the newest) knows the mode the file had.
		return s.create(path, string(data), b.Mode(list[0]))
	}
	if err != nil {
		return ApplyResultView{}, fmt.Errorf("read %s: %w", path, err)
	}
	return s.save(path, string(data), apply.Hash(cur))
}

// RemovedFiles lists the config files removed from supervisor that are not
// back, newest first (at most 20), with the backup Restore brings each back
// from. Files outside the current include setting are left out.
func (s *ConfigService) RemovedFiles() ([]RemovedView, error) {
	list, err := s.rt.backups().Removed(s.rt.disk())
	if err != nil {
		return nil, err
	}
	out := []RemovedView{}
	for _, bk := range list {
		if !s.rt.matchesInclude(bk.Path) || s.rt.checkNotMain(bk.Path) != nil {
			continue
		}
		out = append(out, RemovedView{Path: bk.Path, FileName: filepath.Base(bk.Path), BackupID: bk.ID, At: bk.At})
		if len(out) == maxRemovedListed {
			break
		}
	}
	return out, nil
}

// maxRemovedListed bounds RemovedFiles.
const maxRemovedListed = 20

// create writes content as a new file at path and applies its groups. When
// supervisord refuses it, the file is removed again (ErrRejected).
func (s *ConfigService) create(path, content string, perm fs.FileMode) (ApplyResultView, error) {
	if err := s.rt.checkNotMain(path); err != nil {
		return ApplyResultView{}, err
	}
	if _, err := s.rt.conn(); err != nil {
		return ApplyResultView{}, err
	}
	if cs := s.Validate(filepath.Base(path), content, ""); validate.HasErrors(cs) {
		return ApplyResultView{}, invalidError(cs)
	}
	d := s.rt.disk()
	if err := apply.Create(d, path, []byte(content), perm); err != nil {
		return ApplyResultView{}, err
	}
	res, err := s.applyWritten(path)
	if refused(err) {
		if rerr := apply.RevertCreate(d, path, []byte(content)); rerr != nil {
			return res, fmt.Errorf("%w: %s. Visor could not remove the new file again (%v)", ErrRejected, faultText(err), rerr)
		}
		return res, fmt.Errorf("%w: %s. Visor removed the new file again; your config is still in the editor", ErrRejected, faultText(err))
	}
	return res, err
}

// Templates returns the starting points for a new config.
func (s *ConfigService) Templates() []config.Template { return config.Templates() }

// ProgramForm is the form's view of a config: the first program's name and
// the settings the form has fields for.
type ProgramForm struct {
	Name   string
	Values map[string]string
}

// RenderProgram writes the form's values as a [program:name] section.
func (s *ConfigService) RenderProgram(name string, values map[string]string) string {
	return config.RenderProgram(name, values)
}

// ProgramValues reads the first program of content into form values; it
// fails when the text has no program or does not parse.
func (s *ConfigService) ProgramValues(content string) (ProgramForm, error) {
	name, values, err := config.ProgramValues(content)
	return ProgramForm{Name: name, Values: values}, err
}

// IncludeDir is the folder new configs are written to: the directory of the
// first include pattern. It is empty when supervisord has no usable one.
func (s *ConfigService) IncludeDir() string {
	dir, _ := s.rt.newFileHome()
	return dir
}

// FolderView says whether Visor may add, remove and restore configs: the
// folder new configs go in, whether this user can write it (or it is
// missing), and who owns it ("root").
type FolderView struct {
	Dir      string
	Writable bool
	Missing  bool
	Owner    string
	// Remote: the folder is on a server; User is who Visor's helper runs as
	// there (root with Use sudo). ServerReadOnly: the server is set to Read
	// only, so nothing is written whatever the permissions.
	Remote         bool
	User           string
	ServerReadOnly bool
}

// Folder is the include folder's state for the read-only banner. With no
// include folder there is nothing to say, so it reads as writable.
func (s *ConfigService) Folder() FolderView {
	dir, _ := s.rt.newFileHome()
	if !s.rt.Remote() {
		return folderView(dir, os.Stat, platform.DirWritable, ownerName)
	}
	f, h := s.rt.serverFS()
	v := folderView(dir, f.Stat, f.FolderWritable, ownerName)
	v.Remote, v.User = true, h.User
	if s.rt.CheckConfigWrite() != nil {
		v.Writable, v.ServerReadOnly = false, true
	}
	return v
}

func folderView(dir string, stat func(string) (os.FileInfo, error), writable func(string) bool, owner func(os.FileInfo) string) FolderView {
	v := FolderView{Dir: dir, Writable: true}
	if dir == "" {
		return v
	}
	fi, err := stat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		v.Writable, v.Missing = false, true
	case err != nil && !remoteDown(err):
		v.Writable = false
	case !writable(dir):
		v.Writable, v.Owner = false, owner(fi)
	}
	return v
}

// IncludeExt is the extension of the first include pattern (".ini", ".conf"),
// which new file names get by default.
func (s *ConfigService) IncludeExt() string {
	_, ext := s.rt.newFileHome()
	return ext
}

func (r *Runtime) newFileHome() (dir, ext string) {
	r.mu.Lock()
	globs := r.d.IncludeGlobs
	r.mu.Unlock()
	if len(globs) == 0 || !filepath.IsAbs(globs[0]) {
		return "", ""
	}
	dir = filepath.Dir(globs[0])
	if strings.ContainsAny(dir, "*?[") {
		return "", ""
	}
	return dir, filepath.Ext(globs[0])
}

// Create writes content as a new config file named fileName in the include
// folder, after the checks (errors refuse it), and has supervisord add just
// its programs, so everything else keeps running. The new config is watched
// for notifications. A file that already exists is never overwritten.
func (s *ConfigService) Create(fileName, content string) (ApplyResultView, error) {
	return s.createNamed(fileName, content, 0)
}

// CreateCopy is Create for a copy of the include file from (CopyDraft): the
// new file gets from's mode, so a config kept private stays private.
func (s *ConfigService) CreateCopy(fileName, content, from string) (ApplyResultView, error) {
	if _, err := s.rt.includeFile(from); err != nil {
		return ApplyResultView{}, err
	}
	fi, err := s.rt.files().Stat(from)
	if err != nil {
		return ApplyResultView{}, fmt.Errorf("read %s: %w", from, err)
	}
	return s.createNamed(fileName, content, keptMode(uint32(fi.Mode().Perm())))
}

// keptMode is a source file's permissions for a new config (0644 when
// unknown): always readable and writable by its owner so Visor can edit it,
// and never writable by the group or others (a file from a USB stick or a
// share is 0777, and supervisord may run its command as root).
func keptMode(m uint32) fs.FileMode {
	if m == 0 {
		return 0
	}
	return fs.FileMode(m)&0o755 | 0o600
}

func (s *ConfigService) createNamed(fileName, content string, perm fs.FileMode) (ApplyResultView, error) {
	if err := s.rt.CheckConfigWrite(); err != nil {
		return ApplyResultView{}, err
	}
	s.rt.applyMu.Lock()
	defer s.rt.applyMu.Unlock()
	defer s.rt.markWrites()() // its file changes are Visor's, not outside ones
	dir, _ := s.rt.newFileHome()
	if dir == "" {
		return ApplyResultView{}, errors.New("supervisord's [include] files= setting names no folder Visor can write to")
	}
	if fileName == "" || fileName != filepath.Base(fileName) || strings.HasPrefix(fileName, ".") {
		return ApplyResultView{}, fmt.Errorf("%q is not a file name: use a plain name like web.ini, without folders", fileName)
	}
	path := filepath.Join(dir, fileName)
	if err := s.rt.checkNotMain(path); err != nil {
		return ApplyResultView{}, err
	}
	if there, _ := s.rt.disk().Exists(path); there {
		return ApplyResultView{}, fmt.Errorf("%s already exists in %s. Choose another file name: %w", fileName, dir, apply.ErrExists)
	}
	if err := s.rt.watch.set(path, true); err != nil {
		return ApplyResultView{}, err
	}
	return s.create(path, content, perm)
}

// CopyDraftView is a new config drafted from an existing one: its file name
// and content for Create, what was renamed, and the settings still naming
// the original (Doubts) for the editor to point at.
type CopyDraftView struct {
	FileName, Content string
	Renames           []config.Rename
	Doubts            []config.CopyDoubt
}

// CopyDraft drafts a copy of the include file at path under free names (see
// config.CopyAs): no program or group name of the include files, and no
// file name already in the include folder. Nothing is written; the draft is
// saved with Create like any new config.
func (s *ConfigService) CopyDraft(path string) (CopyDraftView, error) {
	if err := s.rt.CheckConfigWrite(); err != nil {
		return CopyDraftView{}, err
	}
	if _, err := s.rt.includeFile(path); err != nil {
		return CopyDraftView{}, err
	}
	dir, ext := s.rt.newFileHome()
	if dir == "" {
		return CopyDraftView{}, errors.New("supervisord's [include] files= setting names no folder Visor can write to")
	}
	// The copy goes to the include folder, so it takes that folder's
	// extension when the original came from another include pattern.
	name := filepath.Base(path)
	if ext != "" && filepath.Ext(name) != ext {
		name = strings.TrimSuffix(name, filepath.Ext(name)) + ext
	}
	data, err := s.rt.files().ReadFile(path)
	if err != nil {
		return CopyDraftView{}, fmt.Errorf("read %s: %w", path, err)
	}
	entries, err := s.rt.files().Glob(filepath.Join(dir, "*"))
	if err != nil {
		return CopyDraftView{}, fmt.Errorf("read %s: %w", dir, err)
	}
	files := make([]string, 0, len(entries))
	for _, e := range entries {
		files = append(files, filepath.Base(e))
	}
	// The names the checks treat as taken (validateEnv), groups included.
	env := s.rt.validateEnv("")
	c, err := config.CopyAs(name, string(data), env.Inventory.Names(), files, folderPatterns(env.IncludeGlobs, dir))
	if err != nil {
		return CopyDraftView{}, err
	}
	if c.Doubts == nil {
		c.Doubts = []config.CopyDoubt{} // [] rather than null for the frontend
	}
	return CopyDraftView{FileName: c.FileName, Content: c.Content, Renames: c.Renames, Doubts: c.Doubts}, nil
}

// folderPatterns is the file-name part of each include glob for the folder
// dir: the names supervisord loads from it.
func folderPatterns(globs []string, dir string) []string {
	var out []string
	for _, g := range globs {
		if filepath.Dir(g) == dir {
			out = append(out, filepath.Base(g))
		}
	}
	return out
}

// invalidError is ErrInvalidConfig with the first error found.
func invalidError(cs []validate.Check) error {
	for _, c := range cs {
		if c.Level == validate.Error {
			if c.Line > 0 {
				return fmt.Errorf("%w: line %d: %s", ErrInvalidConfig, c.Line, c.Message)
			}
			return fmt.Errorf("%w: %s", ErrInvalidConfig, c.Message)
		}
	}
	return ErrInvalidConfig
}

// validateEnv is the machine the checks run against: the include files and
// globs of the current config.
func (r *Runtime) validateEnv(selfPath string) validate.Env {
	r.mu.Lock()
	d := r.d
	r.mu.Unlock()
	inv, _ := config.LoadInventory(d)
	env := validate.Env{Inventory: inv, IncludeGlobs: d.IncludeGlobs, SelfPath: selfPath, Remote: r.Remote()}
	if env.Remote { // the checks look on the server, through the helper
		f, h := r.serverFS()
		env.Stat, env.LookPath, env.UserExists, env.Writable = f.Stat, r.lookOnServer, f.UserExists, f.FolderWritable
		// Only root sees what supervisord (root) may write.
		env.SkipWritable = h.User == "" || h.UID != 0
	}
	return env
}

func (r *Runtime) watcherOrNil() *config.Watcher {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.watcher
}

// includeFile finds path among supervisord's include files, so the editor
// never reads or writes anything else.
func (r *Runtime) includeFile(path string) (config.ConfigFile, error) {
	r.mu.Lock()
	d := r.d
	r.mu.Unlock()
	inv, err := config.LoadInventory(d)
	if err != nil {
		return config.ConfigFile{}, err
	}
	for _, f := range inv.Files {
		if f.Path == path {
			if err := r.checkNotMain(path); err != nil {
				return config.ConfigFile{}, err
			}
			return f, nil
		}
	}
	return config.ConfigFile{}, fmt.Errorf("%s is not one of supervisord's config files: check the [include] files= setting", path)
}

// checkNotMain refuses supervisord.conf itself, however path reaches it: an
// include glob that matches it, a symlink or a hard link to it.
func (r *Runtime) checkNotMain(path string) error {
	r.mu.Lock()
	main := r.d.ConfigPath
	r.mu.Unlock()
	same := sameFile
	if r.Remote() { // the paths are on the server: the same file there, links too
		same = func(a, b string) bool { return sameOnServer(r.files(), a, b) }
	}
	if main != "" && same(path, main) {
		return fmt.Errorf("%s: %w", path, ErrMainConfig)
	}
	return nil
}

// sameOnServer is sameFile on a server: the same device and inode (a
// symlink or hard link to it), or, for a path not there yet, the same text.
func sameOnServer(f config.FS, a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	fa, ea := f.Stat(a)
	fb, eb := f.Stat(b)
	if ea != nil || eb != nil {
		return false
	}
	ra, oka := fa.Sys().(remote.FileInfo)
	rb, okb := fb.Sys().(remote.FileInfo)
	return oka && okb && ra.Dev == rb.Dev && ra.Ino == rb.Ino
}

// sameFile reports whether a and b are the same file once symlinks are
// followed. For a path that does not exist yet, its folder's symlinks are
// followed.
func sameFile(a, b string) bool {
	fa, ea := os.Stat(a)
	fb, eb := os.Stat(b)
	if ea == nil && eb == nil {
		return os.SameFile(fa, fb)
	}
	return resolve(a) == resolve(b)
}

func resolve(p string) string {
	p = filepath.Clean(p)
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	if d, err := filepath.EvalSymlinks(filepath.Dir(p)); err == nil {
		return filepath.Join(d, filepath.Base(p))
	}
	return p
}

// checkManaged accepts an include file, or a path an include glob matches
// (a file removed from supervisor, which a backup can bring back).
func (r *Runtime) checkManaged(path string) error {
	if err := r.checkNotMain(path); err != nil {
		return err
	}
	if _, err := r.includeFile(path); err == nil || r.matchesInclude(path) {
		return nil
	}
	return fmt.Errorf("%s is not one of supervisord's config files: check the [include] files= setting", path)
}

// matchesInclude reports whether an include glob matches the absolute path.
func (r *Runtime) matchesInclude(path string) bool {
	r.mu.Lock()
	globs := r.d.IncludeGlobs
	r.mu.Unlock()
	for _, g := range globs {
		if ok, _ := filepath.Match(g, filepath.Clean(path)); ok && filepath.IsAbs(path) {
			return true
		}
	}
	return false
}

// Download saves a copy of an include file on a server (or this computer)
// where the person picks, with the save dialog. It returns where it went,
// or "" when they cancelled.
func (s *ConfigService) Download(path string) (string, error) {
	if _, err := s.rt.includeFile(path); err != nil {
		return "", err
	}
	data, err := s.rt.files().ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return saveCopy(filepath.Base(path), data)
}

// saveCopy asks where to save data (named name) and writes it there.
var saveCopy = func(name string, data []byte) (string, error) {
	a := application.Get()
	if a == nil {
		return "", errors.New("cannot show the save dialog: Visor is not running as an app")
	}
	dest, err := a.Dialog.SaveFile().
		SetFilename(name).
		AttachToWindow(a.Window.Current()).
		PromptForSingleSelection()
	if err != nil || dest == "" {
		return "", nil
	}
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		return "", err
	}
	return dest, nil
}

// remoteDown reports a server Visor isn't connected to right now: the
// folder's state is unknown, so the banner says nothing rather than guess.
func remoteDown(err error) bool { return errors.Is(err, remote.ErrNotConnected) }
