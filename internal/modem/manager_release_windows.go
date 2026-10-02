//go:build windows

package modem

// forceReleasePort is a no-op on Windows. COM ports are opened exclusively
// and other processes holding them are never terminated.
func (m *Manager) forceReleasePort(string) {}
