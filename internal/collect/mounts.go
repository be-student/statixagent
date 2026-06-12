package collect

import (
	"bufio"
	"io"
	"strings"
)

// Mount is one entry of /proc/self/mounts.
type Mount struct {
	Device     string
	MountPoint string
	FSType     string
}

// pseudoFS are filesystem types that never represent user disk space.
var pseudoFS = map[string]bool{
	"proc": true, "sysfs": true, "devtmpfs": true, "devpts": true,
	"tmpfs": true, "cgroup": true, "cgroup2": true, "pstore": true,
	"securityfs": true, "debugfs": true, "tracefs": true, "configfs": true,
	"fusectl": true, "mqueue": true, "hugetlbfs": true, "bpf": true,
	"binfmt_misc": true, "autofs": true, "rpc_pipefs": true, "nsfs": true,
	"overlay": true, "squashfs": true, "ramfs": true, "efivarfs": true,
	"fuse.snapfuse": true, "fuse.gvfsd-fuse": true, "fuse.portal": true,
}

// ParseMounts parses /proc/self/mounts and returns real, space-bearing
// filesystems, deduplicated by device (bind mounts appear once, at the
// shortest mount point).
func ParseMounts(r io.Reader) ([]Mount, error) {
	var out []Mount
	byDevice := map[string]int{} // device → index in out
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 3 {
			continue
		}
		m := Mount{Device: unescapeMountField(f[0]), MountPoint: unescapeMountField(f[1]), FSType: f[2]}
		if pseudoFS[m.FSType] || strings.HasPrefix(m.FSType, "fuse.") {
			continue
		}
		// Real block/network filesystems: device path, ZFS dataset, or remote.
		isReal := strings.HasPrefix(m.Device, "/dev/") ||
			m.FSType == "zfs" || m.FSType == "btrfs" ||
			strings.Contains(m.Device, ":/") // NFS host:/export
		if !isReal {
			continue
		}
		if i, seen := byDevice[m.Device]; seen {
			if len(m.MountPoint) < len(out[i].MountPoint) {
				out[i] = m
			}
			continue
		}
		byDevice[m.Device] = len(out)
		out = append(out, m)
	}
	return out, sc.Err()
}

// unescapeMountField decodes the octal escapes used in /proc mounts fields
// (\040 for space, \011 tab, \012 newline, \134 backslash).
func unescapeMountField(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) && isOctal(s[i+1]) && isOctal(s[i+2]) && isOctal(s[i+3]) {
			b.WriteByte((s[i+1]-'0')<<6 | (s[i+2]-'0')<<3 | (s[i+3] - '0'))
			i += 3
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func isOctal(c byte) bool { return c >= '0' && c <= '7' }
