package sshwatch

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"time"
)

// Session is one live login from utmp.
type Session struct {
	User  string
	TTY   string
	Host  string // source IP or hostname as sshd recorded it
	Since time.Time
}

// utmpRecordSize is the glibc struct utmp size on Linux (both amd64 and
// arm64): the fields are fixed int32/char arrays, not native longs.
const utmpRecordSize = 384

const userProcess = 7 // ut_type for an active user session

// ParseUtmp reads /var/run/utmp content and returns active user sessions.
func ParseUtmp(r io.Reader) ([]Session, error) {
	var out []Session
	buf := make([]byte, utmpRecordSize)
	for {
		_, err := io.ReadFull(r, buf)
		if err == io.EOF {
			return out, nil
		}
		if err == io.ErrUnexpectedEOF {
			return out, fmt.Errorf("sshwatch: utmp truncated record")
		}
		if err != nil {
			return out, err
		}
		typ := int16(binary.LittleEndian.Uint16(buf[0:2]))
		if typ != userProcess {
			continue
		}
		tvSec := int32(binary.LittleEndian.Uint32(buf[340:344]))
		out = append(out, Session{
			TTY:   cstr(buf[8:40]),
			User:  cstr(buf[44:76]),
			Host:  cstr(buf[76:332]),
			Since: time.Unix(int64(tvSec), 0),
		})
	}
}

func cstr(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return string(b)
}

// EncodeUtmpRecord builds one utmp record; used by tests and kept here so
// the layout constants live in one place.
func EncodeUtmpRecord(typ int16, user, tty, host string, at time.Time) []byte {
	buf := make([]byte, utmpRecordSize)
	binary.LittleEndian.PutUint16(buf[0:2], uint16(typ))
	copy(buf[8:40], tty)
	copy(buf[44:76], user)
	copy(buf[76:332], host)
	binary.LittleEndian.PutUint32(buf[340:344], uint32(at.Unix()))
	return buf
}
