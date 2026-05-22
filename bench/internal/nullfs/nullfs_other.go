//go:build !linux

package nullfs

func mountFUSE(parent string) (*Mount, error) {
	return mountTmpDir(parent)
}
