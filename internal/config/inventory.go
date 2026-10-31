package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Program is one [program:x] section and the supervisord group it belongs to.
// Autorestart is autorestart= lower-cased ("unexpected" when unset).
type Program struct {
	Name, Group string
	Section     Section
	NumProcs    int
	ExitCodes   []int
	Autorestart string
}

// ConfigFile is one include file. Err is a read or parse error; the file is
// still listed so the UI can point at it.
type ConfigFile struct {
	Path, Hash string
	ModTime    time.Time
	Programs   []Program
	Err        error
}

// Inventory is every include file, sorted by Path.
type Inventory struct {
	Files []ConfigFile

	groupFile map[string]string // [group:x] section -> file
}

// expandGlobs returns the sorted, de-duplicated files matched by the include
// globs.
func expandGlobs(d Discovery) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, g := range d.IncludeGlobs {
		m, err := filepath.Glob(g)
		if err != nil {
			return nil, fmt.Errorf("include glob %q: %w", g, err)
		}
		for _, p := range m {
			if fi, err := os.Stat(p); err != nil || fi.IsDir() || seen[p] {
				continue
			}
			seen[p] = true
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out, nil
}

// LoadInventory reads every file matched by d.IncludeGlobs. A file that
// cannot be read or parsed is listed with Err set.
func LoadInventory(d Discovery) (Inventory, error) {
	paths, err := expandGlobs(d)
	if err != nil {
		return Inventory{}, err
	}
	inv := Inventory{groupFile: map[string]string{}}
	parsed := make([]File, len(paths))
	groupOf := map[string]string{} // program name -> group
	for i, p := range paths {
		cf := ConfigFile{Path: p}
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			cf.Err = rerr
			inv.Files = append(inv.Files, cf)
			continue
		}
		sum := sha256.Sum256(data)
		cf.Hash = hex.EncodeToString(sum[:])
		if fi, serr := os.Stat(p); serr == nil {
			cf.ModTime = fi.ModTime()
		}
		f, perr := ParseINI(bytes.NewReader(data), p)
		cf.Err = perr
		parsed[i] = f
		for _, s := range f.SectionsWithPrefix("group:") {
			g := strings.TrimPrefix(s.Name, "group:")
			if _, dup := inv.groupFile[g]; !dup {
				inv.groupFile[g] = p
			}
			list, _ := s.Get("programs")
			for _, n := range strings.Split(list, ",") {
				if n = strings.TrimSpace(n); n != "" {
					groupOf[n] = g
				}
			}
		}
		inv.Files = append(inv.Files, cf)
	}
	for i := range inv.Files {
		for _, s := range parsed[i].SectionsWithPrefix("program:") {
			name := strings.TrimPrefix(s.Name, "program:")
			g, ok := groupOf[name]
			if !ok {
				g = name
			}
			inv.Files[i].Programs = append(inv.Files[i].Programs, Program{
				Name: name, Group: g, Section: s,
				NumProcs:    intOr(s, "numprocs", 1),
				ExitCodes:   exitCodes(s),
				Autorestart: autorestart(s),
			})
		}
	}
	return inv, nil
}

func intOr(s Section, key string, def int) int {
	v, ok := s.Get(key)
	if !ok {
		return def
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return def
	}
	return n
}

func exitCodes(s Section) []int {
	v, ok := s.Get("exitcodes")
	if !ok {
		return []int{0}
	}
	var out []int
	for _, part := range strings.Split(v, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			return []int{0}
		}
		out = append(out, n)
	}
	if len(out) == 0 {
		return []int{0}
	}
	return out
}

func autorestart(s Section) string {
	v, _ := s.Get("autorestart")
	if v = strings.ToLower(strings.TrimSpace(v)); v != "" {
		return v
	}
	return "unexpected"
}

// FileForGroup returns the file defining the group: its [group:x] section if
// there is one, otherwise the file of a program in that group.
func (inv Inventory) FileForGroup(group string) (string, bool) {
	if p, ok := inv.groupFile[group]; ok {
		return p, true
	}
	for _, f := range inv.Files {
		for _, p := range f.Programs {
			if p.Group == group {
				return f.Path, true
			}
		}
	}
	return "", false
}

// ExpectedExit returns the exit codes the program behind fullName
// ("group:process") treats as expected; [0] if it cannot be found.
func (inv Inventory) ExpectedExit(fullName string) []int {
	if _, p, ok := inv.Lookup(fullName); ok {
		return p.ExitCodes
	}
	return []int{0}
}

// Autorestart returns the autorestart= of the program behind fullName,
// lower-cased; "unexpected" (supervisord's default) if it is unset or the
// program cannot be found.
func (inv Inventory) Autorestart(fullName string) string {
	if _, p, ok := inv.Lookup(fullName); ok && p.Autorestart != "" {
		return p.Autorestart
	}
	return "unexpected"
}

// Lookup finds the program behind a process full name ("group:process")
// and the file that defines it, expanding each program's process_name for
// every process number in its numprocs range.
func (inv Inventory) Lookup(fullName string) (path string, prog Program, ok bool) {
	group, proc, found := strings.Cut(fullName, ":")
	if !found {
		return "", Program{}, false
	}
	for _, f := range inv.Files {
		here := filepath.Dir(f.Path)
		for _, p := range f.Programs {
			if p.Group != group {
				continue
			}
			pn, _ := p.Section.Get("process_name")
			if pn == "" {
				pn = "%(program_name)s"
			}
			start := intOr(p.Section, "numprocs_start", 0)
			for i := start; i < start+p.NumProcs; i++ {
				got := Expand(pn, map[string]string{
					"program_name": p.Name,
					"group_name":   group,
					"process_num":  strconv.Itoa(i),
					"numprocs":     strconv.Itoa(p.NumProcs),
					"here":         here,
				})
				if got == proc {
					return f.Path, p, true
				}
			}
		}
	}
	return "", Program{}, false
}
