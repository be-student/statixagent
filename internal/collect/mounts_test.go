package collect

import (
	"strings"
	"testing"
)

const mountsFixture = `sysfs /sys sysfs rw,nosuid 0 0
proc /proc proc rw,nosuid 0 0
udev /dev devtmpfs rw,nosuid 0 0
tmpfs /run tmpfs rw,nosuid 0 0
/dev/vda1 / ext4 rw,relatime 0 0
/dev/vda1 /home/bind ext4 rw,relatime 0 0
/dev/vda15 /boot/efi vfat rw,relatime 0 0
/dev/sdb1 /mnt/my\040data ext4 rw 0 0
cgroup2 /sys/fs/cgroup cgroup2 rw 0 0
overlay /var/lib/docker/overlay2/abc/merged overlay rw 0 0
tank/home /tank/home zfs rw,xattr 0 0
nas:/export /mnt/nas nfs4 rw 0 0
portal /run/user/1000/doc fuse.portal rw 0 0
`

func TestParseMounts(t *testing.T) {
	mounts, err := ParseMounts(strings.NewReader(mountsFixture))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Mount{}
	for _, m := range mounts {
		got[m.MountPoint] = m
	}
	for _, want := range []string{"/", "/boot/efi", "/mnt/my data", "/tank/home", "/mnt/nas"} {
		if _, ok := got[want]; !ok {
			t.Errorf("missing mount %q in %v", want, mounts)
		}
	}
	if len(mounts) != 5 {
		t.Errorf("mounts = %d, want 5 (pseudo FS and bind dupes excluded): %+v", len(mounts), mounts)
	}
	if _, ok := got["/home/bind"]; ok {
		t.Error("bind mount duplicate must collapse to shortest mount point")
	}
}

func TestUnescapeMountField(t *testing.T) {
	cases := map[string]string{
		`/mnt/my\040data`: "/mnt/my data",
		`/plain`:          "/plain",
		`/tab\011here`:    "/tab\there",
		`/back\134slash`:  `/back\slash`,
		`/trunc\04`:       `/trunc\04`, // incomplete escape left as-is
	}
	for in, want := range cases {
		if got := unescapeMountField(in); got != want {
			t.Errorf("unescape(%q) = %q, want %q", in, got, want)
		}
	}
}
