package app

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// EventFilesDropped carries files dropped on Visor's main window, read for
// Add new → Import files ([]DroppedFileView). The window takes the drop
// natively (EnableFileDrop): left to the web view, WebKitGTK would load a
// dropped file in place of Visor's page.
const EventFilesDropped = "visor:files-dropped"

func init() { application.RegisterEvent[[]DroppedFileView](EventFilesDropped) }

// DroppedFileView is one dropped file: its name and text, or why it can't
// be used (Error).
type DroppedFileView struct {
	Name    string
	Content string
	Mode    uint32 // permissions, kept by Import
	Error   string
}

// ReadDropped reads dropped files for Import: at most maxImportFiles, each
// a regular file of at most maxImportBytes. A folder or an unreadable file
// comes back with Error set, so the page can say why.
func ReadDropped(paths []string) []DroppedFileView {
	out := []DroppedFileView{}
	for _, p := range paths {
		if len(out) == maxImportFiles {
			break
		}
		v := DroppedFileView{Name: filepath.Base(p)}
		fi, err := os.Stat(p)
		switch {
		case err != nil:
			v.Error = fmt.Sprintf("Couldn’t read it: %v", err)
		case fi.IsDir():
			v.Error = "This is a folder: drop the config files in it instead."
		case !fi.Mode().IsRegular():
			v.Error = "This isn’t a regular file."
		case fi.Size() > maxImportBytes:
			v.Error = "Larger than 1 MB, too large for a program config."
		default:
			b, err := os.ReadFile(p)
			if err != nil {
				v.Error = fmt.Sprintf("Couldn’t read it: %v", err)
			} else {
				v.Content, v.Mode = string(b), uint32(fi.Mode().Perm())
			}
		}
		out = append(out, v)
	}
	return out
}

// FilesDropped reads files dropped on the main window and hands them to
// the page, which opens them in Add new → Import files.
func FilesDropped(paths []string) {
	if len(paths) > 0 {
		WailsEmit(EventFilesDropped, ReadDropped(paths))
	}
}
