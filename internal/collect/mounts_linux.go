//go:build linux

package collect

import (
	"os"

	"golang.org/x/sys/unix"
)

// ReadMountUsage parses /proc/self/mounts and statfs's each real mount.
// Mounts that fail statfs (stale NFS, permissions) are skipped.
func ReadMountUsage() ([]MountUsage, error) {
	f, err := os.Open("/proc/self/mounts")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	mounts, err := ParseMounts(f)
	if err != nil {
		return nil, err
	}
	var out []MountUsage
	for _, m := range mounts {
		var st unix.Statfs_t
		if err := unix.Statfs(m.MountPoint, &st); err != nil {
			continue
		}
		total := st.Blocks * uint64(st.Bsize)
		if total == 0 {
			continue
		}
		avail := st.Bavail * uint64(st.Bsize)
		out = append(out, MountUsage{
			MountPoint:  m.MountPoint,
			TotalBytes:  total,
			FreeBytes:   avail,
			UsedPercent: 100 * float64(total-avail) / float64(total),
		})
	}
	return out, nil
}
