package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/touhidurabir/visor/internal/apply"
	"github.com/touhidurabir/visor/internal/config"
	"github.com/touhidurabir/visor/internal/describe"
	"github.com/touhidurabir/visor/internal/validate"
)

// ErrInvalidConfig means a save was refused because its checks found
// errors. The checks are in Validate's answer.
var ErrInvalidConfig = errors.New("config has errors")

// maxBackupsListed bounds how many backups Backups returns (newest first).
const maxBackupsListed = 50

// ConfigService edits supervisord's include files: read, check, describe,
// save and apply, back up and restore. Saves are one at a time.
type ConfigService struct {
	rt *Runtime
	mu sync.Mutex // one save, restore or apply at a time
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
	data, err := os.ReadFile(path)
	if err != nil {
		return FileView{}, fmt.Errorf("read %s: %w", path, err)
	}
	return FileView{
		Path: path, Content: string(data), Hash: apply.Hash(data),
		Writable: apply.CheckWritable(path) == nil,
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

// LoadedDiff returns what supervisord loaded from path (Visor's snapshot)
// and what is on disk now.
func (s *ConfigService) LoadedDiff(path string) (LoadedDiffView, error) {
	if _, err := s.rt.includeFile(path); err != nil {
		return LoadedDiffView{}, err
	}
	disk, err := os.ReadFile(path)
	if err != nil {
		return LoadedDiffView{}, fmt.Errorf("read %s: %w", path, err)
	}
	v := LoadedDiffView{Disk: string(disk)}
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
func (s *ConfigService) Save(path, content, hash string) (ApplyResultView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.save(path, content, hash)
}

func (s *ConfigService) save(path, content, hash string) (ApplyResultView, error) {
	if hash == "" {
		return ApplyResultView{}, errors.New("save needs the hash of the content you edited")
	}
	if _, err := s.rt.includeFile(path); err != nil {
		return ApplyResultView{}, err
	}
	if cs := s.Validate(filepath.Base(path), content, path); validate.HasErrors(cs) {
		return ApplyResultView{}, invalidError(cs)
	}
	// Without a connection nothing could be applied: refuse before writing.
	if _, err := s.rt.conn(); err != nil {
		return ApplyResultView{}, err
	}
	old, err := os.ReadFile(path)
	if err != nil {
		return ApplyResultView{}, fmt.Errorf("read %s: %w", path, err)
	}
	if _, err := apply.Write(path, []byte(content), hash, s.rt.backups()); err != nil {
		return ApplyResultView{}, err
	}
	return s.applyWritten(path, old)
}

// applyWritten applies the groups path defines now and the ones it
// defined in old.
func (s *ConfigService) applyWritten(path string, old []byte) (ApplyResultView, error) {
	groups, err := s.rt.groupsInFile(path)
	if err != nil {
		return ApplyResultView{}, fmt.Errorf("saved %s, but could not apply it: %w", path, err)
	}
	for _, g := range groupsIn(old, path) {
		if !slices.Contains(groups, g) {
			groups = append(groups, g)
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
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.rt.includeFile(path); err != nil {
		return ApplyResultView{}, err
	}
	var old []byte
	if w := s.rt.watcherOrNil(); w != nil {
		old, _ = w.Loaded(path)
	}
	res, err := s.applyWritten(path, old)
	return res, err
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
	s.mu.Lock()
	defer s.mu.Unlock()
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
	cur, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s.create(path, string(data))
	}
	if err != nil {
		return ApplyResultView{}, fmt.Errorf("read %s: %w", path, err)
	}
	return s.save(path, string(data), apply.Hash(cur))
}

// create writes content as a new file at path and applies its groups.
func (s *ConfigService) create(path, content string) (ApplyResultView, error) {
	if cs := s.Validate(filepath.Base(path), content, ""); validate.HasErrors(cs) {
		return ApplyResultView{}, invalidError(cs)
	}
	if _, err := s.rt.conn(); err != nil {
		return ApplyResultView{}, err
	}
	if _, err := apply.Write(path, []byte(content), "", s.rt.backups()); err != nil {
		return ApplyResultView{}, err
	}
	return s.applyWritten(path, nil)
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
	s.mu.Lock()
	defer s.mu.Unlock()
	dir, _ := s.rt.newFileHome()
	if dir == "" {
		return ApplyResultView{}, errors.New("supervisord's [include] files= setting names no folder Visor can write to")
	}
	if fileName == "" || fileName != filepath.Base(fileName) || strings.HasPrefix(fileName, ".") {
		return ApplyResultView{}, fmt.Errorf("%q is not a file name: use a plain name like web.ini, without folders", fileName)
	}
	path := filepath.Join(dir, fileName)
	if _, err := os.Lstat(path); err == nil {
		return ApplyResultView{}, fmt.Errorf("%s already exists in %s. Choose another file name: %w", fileName, dir, apply.ErrExists)
	}
	if err := s.rt.watch.set(path, true); err != nil {
		return ApplyResultView{}, err
	}
	return s.create(path, content)
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
	return validate.Env{Inventory: inv, IncludeGlobs: d.IncludeGlobs, SelfPath: selfPath}
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
			return f, nil
		}
	}
	return config.ConfigFile{}, fmt.Errorf("%s is not one of supervisord's config files: check the [include] files= setting", path)
}

// checkManaged accepts an include file, or a path an include glob matches
// (a file removed from supervisor, which a backup can bring back).
func (r *Runtime) checkManaged(path string) error {
	if _, err := r.includeFile(path); err == nil {
		return nil
	}
	r.mu.Lock()
	globs := r.d.IncludeGlobs
	r.mu.Unlock()
	for _, g := range globs {
		if ok, _ := filepath.Match(g, filepath.Clean(path)); ok && filepath.IsAbs(path) {
			return nil
		}
	}
	return fmt.Errorf("%s is not one of supervisord's config files: check the [include] files= setting", path)
}
