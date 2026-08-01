package run

import "os"

// saveEmpty creates a bare run directory. Test-only: Resolve walks directory
// names, so prefix behaviour is testable without a real transcript.
func (s *Store) saveEmpty(sid string) error {
	return os.MkdirAll(s.Dir(sid), 0755)
}
