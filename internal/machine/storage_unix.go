//go:build darwin || linux

package machine

import "golang.org/x/sys/unix"

func storage(dir string) *int64 {
	var st unix.Statfs_t
	if unix.Statfs(dir, &st) != nil {
		return nil
	}
	n := int64(st.Bavail) * int64(st.Bsize)
	return &n
}
