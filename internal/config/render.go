package config

import (
	"errors"
	"strings"
)

// RenderProgram writes a [program:name] section with the form's settings in
// FormKeys order; empty values are left out.
func RenderProgram(name string, values map[string]string) string {
	var b strings.Builder
	b.WriteString("[program:" + name + "]\n")
	for _, k := range FormKeys {
		if v := strings.TrimSpace(values[k]); v != "" {
			b.WriteString(k + "=" + v + "\n")
		}
	}
	return b.String()
}

// ProgramValues reads the first [program:x] section of content: its name and
// the FormKeys settings it sets. Settings the form has no field for are not
// returned.
func ProgramValues(content string) (string, map[string]string, error) {
	f, err := ParseINI(strings.NewReader(content), "new config")
	if err != nil {
		return "", nil, err
	}
	progs := f.SectionsWithPrefix("program:")
	if len(progs) == 0 {
		return "", nil, errors.New("there is no [program:name] section")
	}
	s := progs[0]
	values := map[string]string{}
	for _, k := range FormKeys {
		if v, ok := s.Get(k); ok {
			values[k] = v
		}
	}
	return strings.TrimSpace(strings.TrimPrefix(s.Name, "program:")), values, nil
}
