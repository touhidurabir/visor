package app

import "github.com/touhidurabir/visor/internal/platform"

// SystemService tells the frontend which OS Visor runs on, so it can say ⌘
// or Ctrl, Keychain or keyring, Finder or the file manager.
type SystemService struct{ info platform.Info }

// NewSystemService describes the OS this build runs on.
func NewSystemService() *SystemService {
	return &SystemService{info: platform.Describe(platform.Current())}
}

// Platform is the OS and its wording (lib/platform.ts loads it once).
func (s *SystemService) Platform() platform.Info { return s.info }
