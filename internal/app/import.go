package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/touhidurabir/visor/internal/config"
)

// ImportItem is one config file brought in from elsewhere (a repo, a file
// an assistant wrote): the name it gets in the include folder, and its text.
type ImportItem struct {
	FileName string
	Content  string
}

// ImportResultView is what happened to one imported file: Written when it
// is in the include folder now (also when applying it then failed: Error
// says so), Result what supervisord did, and whether its programs were
// started afterwards (StartError says why not).
type ImportResultView struct {
	FileName   string
	Path       string
	Written    bool
	Result     ApplyResultView
	Error      string
	Started    bool
	StartError string
}

// Import limits: a picker or a drop can bring in many files at once.
const (
	maxImportFiles = 50
	maxImportBytes = 1 << 20
)

// CheckImport checks files about to be imported together, each as a new
// file in the include folder: the same checks as Add new, plus its file
// name (free in the folder, and not shared with another file in the
// batch), and its programs against the other files' programs, which are
// not on disk yet. One ExplainView per item, in order.
func (s *ConfigService) CheckImport(items []ImportItem) []ExplainView {
	dir, _ := s.rt.newFileHome()
	env := s.rt.validateEnv("")
	// Each file's programs, read once (not once per other file).
	pending := make([]config.ConfigFile, len(items))
	for i, it := range items {
		pending[i] = config.ConfigFile{Path: filepath.Join(dir, it.FileName)}
		if len(it.Content) > maxImportBytes {
			continue
		}
		for _, p := range programsIn(it.Content) {
			pending[i].Programs = append(pending[i].Programs, config.Program{Name: p, Group: p})
		}
	}
	out := make([]ExplainView, len(items))
	for i, it := range items {
		ps := importProblems(dir, items, i)
		if i >= maxImportFiles {
			// Past the cap: say so, without checking the text.
			ps = append(ps, ProblemView{Level: "error", Message: fmt.Sprintf("Visor imports at most %d files at a time", maxImportFiles), Fix: "Add the others in a second import."})
			out[i] = ExplainView{FileProblems: ps, Errors: len(ps), SaveErrors: len(ps)}
			continue
		}
		e := env
		e.Inventory = withPending(env.Inventory, pending, i)
		out[i] = s.rt.explainWith(it.FileName, it.Content, "", e, ps)
	}
	return out
}

// withPending adds the batch's other files to inv, so a program name two
// of them share is a duplicate.
func withPending(inv config.Inventory, pending []config.ConfigFile, self int) config.Inventory {
	out := inv
	out.Files = slices.Clone(inv.Files)
	for j, f := range pending {
		if j != self {
			out.Files = append(out.Files, f)
		}
	}
	return out
}

// programsIn lists the [program:x] names in content (none when it can't
// be read).
func programsIn(content string) []string {
	f, err := config.ParseINI(strings.NewReader(content), "import")
	if err != nil {
		return nil
	}
	var out []string
	for _, s := range f.SectionsWithPrefix("program:") {
		out = append(out, strings.TrimPrefix(s.Name, "program:"))
	}
	return out
}

// importProblems are the problems with item i as a file: its name, its
// size and whether it is text.
func importProblems(dir string, items []ImportItem, i int) []ProblemView {
	it := items[i]
	var ps []ProblemView
	bad := func(msg, fix string) { ps = append(ps, ProblemView{Level: "error", Message: msg, Fix: fix}) }
	name := it.FileName
	switch {
	case strings.TrimSpace(name) == "":
		bad("Give the file a name", "Type a name like web.ini.")
	case name != filepath.Base(name) || strings.HasPrefix(name, "."):
		bad(fmt.Sprintf("%s is not a file name", name), "Use a plain name like web.ini, without folders.")
	case dir != "":
		if _, err := os.Lstat(filepath.Join(dir, name)); err == nil {
			bad(fmt.Sprintf("%s already exists in %s", name, dir), "Choose another file name, e.g. "+altName(name)+".")
		}
	}
	// Case-blind: macOS folders usually are, so a.ini and A.ini would be one file.
	for j, other := range items {
		if j != i && name != "" && strings.EqualFold(other.FileName, name) {
			bad(fmt.Sprintf("Another file you’re importing is also named %s", name), "Give one of them another name, e.g. "+altName(name)+".")
			break
		}
	}
	if len(it.Content) > maxImportBytes {
		bad("This file is larger than 1 MB, too large for a program config", "Choose the config file itself.")
	} else if !utf8.ValidString(it.Content) || strings.ContainsRune(it.Content, 0) {
		bad("This file isn’t text, so it can’t be a config", "Choose a .ini or .conf file.")
	}
	return ps
}

// altName is name with -2 before its extension: web.ini → web-2.ini.
func altName(name string) string {
	ext := filepath.Ext(name)
	return strings.TrimSuffix(name, ext) + "-2" + ext
}

// Import adds files from elsewhere as new configs, one after another, each
// as Create would (checked, written, watched, its programs added to
// supervisord; a file supervisord refuses is removed again). It refuses
// the whole batch while any file has a problem that blocks saving. With
// start, the programs of every file that was added are then started (ones
// autostart already started stay as they are). One result per item; the
// error is for the batch as a whole.
func (s *ConfigService) Import(items []ImportItem, start bool) ([]ImportResultView, error) {
	if err := s.rt.CheckConfigWrite(); err != nil {
		return nil, err
	}
	switch {
	case len(items) == 0:
		return nil, errors.New("choose at least one file to import")
	case len(items) > maxImportFiles:
		return nil, fmt.Errorf("import at most %d files at a time", maxImportFiles)
	}
	dir, _ := s.rt.newFileHome()
	if dir == "" {
		return nil, errors.New("supervisord's [include] files= setting names no folder Visor can write to")
	}
	if _, err := s.rt.conn(); err != nil {
		return nil, err
	}

	s.rt.applyMu.Lock()
	defer s.rt.markWrites()() // its file changes are Visor's, not outside ones
	for i, v := range s.CheckImport(items) {
		if v.SaveErrors > 0 {
			s.rt.applyMu.Unlock()
			return nil, fmt.Errorf("%w: %s has %s to fix first", ErrInvalidConfig, items[i].FileName, plural(v.SaveErrors, "problem", "problems"))
		}
	}
	out := make([]ImportResultView, len(items))
	for i, it := range items {
		path := filepath.Join(dir, it.FileName)
		out[i] = ImportResultView{FileName: it.FileName, Path: path}
		if err := s.rt.checkNotMain(path); err != nil {
			out[i].Error = err.Error()
			continue
		}
		if _, err := os.Lstat(path); err == nil {
			out[i].Error = fmt.Sprintf("%s already exists in %s. Choose another file name", it.FileName, dir)
			continue
		}
		// Watched from the start, as Create does; put back as it was when
		// the file isn't written after all.
		was := s.rt.watch.watched(path)
		if err := s.rt.watch.set(path, true); err != nil {
			out[i].Error = err.Error()
			continue
		}
		res, err := s.create(path, it.Content)
		out[i].Result = res
		if err != nil {
			out[i].Error = err.Error()
		}
		if _, statErr := os.Lstat(path); statErr == nil {
			out[i].Written = true
		} else if !was {
			_ = s.rt.watch.set(path, false)
		}
	}
	s.rt.applyMu.Unlock()

	if start {
		for i := range out {
			if !out[i].Written {
				continue
			}
			if err := s.rt.fileAction(out[i].Path, false, true); err != nil {
				out[i].StartError = err.Error()
			} else {
				out[i].Started = true
			}
		}
	}
	return out, nil
}
