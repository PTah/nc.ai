//go:build !darwin && !windows

package secrets

type osBackend struct{}

func newOSBackend() backend { return osBackend{} }

func (osBackend) Get(string) (string, error) { return "", errNoOS }
func (osBackend) Set(string, string) error   { return errNoOS }
func (osBackend) Delete(string) error        { return errNoOS }
