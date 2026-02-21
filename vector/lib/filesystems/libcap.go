package filesystems

import (
	"encoding/binary"
	"fmt"
)

// File capability xattr constants (stable Linux kernel ABI, since 2.6.26).
const (
	xattrNameCaps        = "security.capability"
	vfsCapRevision2      = 0x02000002
	vfsCapFlagsEffective = 0x000001
	vfsCapDataSize       = 20 // 4-byte header + 2×(4-byte permitted + 4-byte inheritable)
)

// vfsCapsV2 represents the on-disk layout of a VFS_CAP_REVISION_2
// security.capability xattr (struct vfs_ns_cap_data, 20 bytes LE).
type vfsCapsV2 struct {
	MagicEtc     uint32 // revision | effective flag
	Permitted0   uint32 // caps 0–31 permitted
	Inheritable0 uint32 // caps 0–31 inheritable
	Permitted1   uint32 // caps 32–63 permitted
	Inheritable1 uint32 // caps 32–63 inheritable
}

// setFileCap sets a file capability using the security.capability xattr.
// capBit is the capability number (e.g., unix.CAP_NET_RAW = 13).
// The capability is set in the permitted and effective sets (VFS_CAP_REVISION_2).
func setFileCap(path string, capBit int) error {
	caps := vfsCapsV2{
		MagicEtc: vfsCapRevision2 | vfsCapFlagsEffective,
	}
	bit := uint32(1) << uint(capBit%32)
	if capBit < 32 {
		caps.Permitted0 = bit
	} else {
		caps.Permitted1 = bit
	}

	var buf [vfsCapDataSize]byte
	binary.LittleEndian.PutUint32(buf[0:], caps.MagicEtc)
	binary.LittleEndian.PutUint32(buf[4:], caps.Permitted0)
	binary.LittleEndian.PutUint32(buf[8:], caps.Inheritable0)
	binary.LittleEndian.PutUint32(buf[12:], caps.Permitted1)
	binary.LittleEndian.PutUint32(buf[16:], caps.Inheritable1)

	return sysLsetxattr(path, xattrNameCaps, buf[:], 0)
}

// getFileCap checks whether a file has a specific capability set.
// Returns true if the capability bit is in the permitted set with the effective flag.
func getFileCap(path string, capBit int) (bool, error) {
	sz, err := sysLgetxattr(path, xattrNameCaps, nil)
	if err != nil {
		return false, err
	}
	buf := make([]byte, sz)
	if _, err := sysLgetxattr(path, xattrNameCaps, buf); err != nil {
		return false, err
	}
	if len(buf) < vfsCapDataSize {
		return false, fmt.Errorf("security.capability xattr too short (%d bytes)", len(buf))
	}

	caps := vfsCapsV2{
		MagicEtc:     binary.LittleEndian.Uint32(buf[0:]),
		Permitted0:   binary.LittleEndian.Uint32(buf[4:]),
		Inheritable0: binary.LittleEndian.Uint32(buf[8:]),
		Permitted1:   binary.LittleEndian.Uint32(buf[12:]),
		Inheritable1: binary.LittleEndian.Uint32(buf[16:]),
	}

	if caps.MagicEtc&vfsCapFlagsEffective == 0 {
		return false, nil
	}

	var permitted uint32
	if capBit < 32 {
		permitted = caps.Permitted0
	} else {
		permitted = caps.Permitted1
	}
	return permitted&(1<<uint(capBit%32)) != 0, nil
}
