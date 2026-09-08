//go:build !linux && !darwin

package source

const supported = false

func (m *Manager) run(s *input) { close(s.done) }
