package filesystems

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

var (
	// mountInfoPath is the path to the mountinfo file.
	mountInfoPath = "/proc/self/mountinfo"

	// devDiskByUUIDPath is the directory containing UUID symlinks to devices.
	devDiskByUUIDPath = "/dev/disk/by-uuid"

	// devDiskByPartUUIDPath is the directory containing PARTUUID symlinks to devices.
	devDiskByPartUUIDPath = "/dev/disk/by-partuuid"

	// readMountInfo reads and parses the system mount info. Replaceable for testing.
	readMountInfo = defaultReadMountInfo

	// resolveDeviceLink resolves a path through any symlinks. Replaceable for testing.
	resolveDeviceLink = defaultResolveDeviceLink
)

// MountInfoEntry represents a parsed line from /proc/self/mountinfo.
type MountInfoEntry struct {
	MountID    int
	ParentID   int
	Major      int
	Minor      int
	Root       string
	Mountpoint string
	Options    string
	FSType     string
	Source     string
	SuperOpts  string
}

// String returns a human-readable representation of a MountInfoEntry.
func (e *MountInfoEntry) String() string {
	return fmt.Sprintf("TARGET=%s SOURCE=%s FSTYPE=%s OPTIONS=%s",
		e.Mountpoint, e.Source, e.FSType, e.Options)
}

func defaultReadMountInfo() ([]*MountInfoEntry, error) {
	return parseMountInfoFile(mountInfoPath)
}

func defaultResolveDeviceLink(path string) (string, error) {
	return filepath.EvalSymlinks(path)
}

// parseMountInfoFile parses a mountinfo-formatted file.
func parseMountInfoFile(path string) ([]*MountInfoEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var entries []*MountInfoEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		entry, err := parseMountInfoLine(line)
		if err != nil {
			continue // skip malformed lines
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

// parseMountInfoLine parses a single line from /proc/self/mountinfo.
// Format: mount_id parent_id major:minor root mountpoint options [optional...] - fstype source super_options
func parseMountInfoLine(line string) (*MountInfoEntry, error) {
	parts := strings.SplitN(line, " - ", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("malformed mountinfo line: no separator")
	}

	left := strings.Fields(parts[0])
	right := strings.Fields(parts[1])

	if len(left) < 6 {
		return nil, fmt.Errorf("malformed mountinfo line: %d left fields, need >= 6", len(left))
	}
	if len(right) < 2 {
		return nil, fmt.Errorf("malformed mountinfo line: %d right fields, need >= 2", len(right))
	}

	mountID, _ := strconv.Atoi(left[0])
	parentID, _ := strconv.Atoi(left[1])

	var major, minor int
	if _, err := fmt.Sscanf(left[2], "%d:%d", &major, &minor); err != nil {
		return nil, fmt.Errorf("malformed major:minor field: %s", left[2])
	}

	entry := &MountInfoEntry{
		MountID:    mountID,
		ParentID:   parentID,
		Major:      major,
		Minor:      minor,
		Root:       unescapeOctal(left[3]),
		Mountpoint: unescapeOctal(left[4]),
		Options:    left[5],
		FSType:     right[0],
		Source:     right[1],
	}
	if len(right) >= 3 {
		entry.SuperOpts = right[2]
	}

	return entry, nil
}

// unescapeOctal decodes octal escapes in mountinfo fields.
// Common escapes: \040 (space), \011 (tab), \012 (newline), \134 (backslash).
func unescapeOctal(s string) string {
	if !strings.ContainsRune(s, '\\') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			o1 := s[i+1] - '0'
			o2 := s[i+2] - '0'
			o3 := s[i+3] - '0'
			if o1 <= 7 && o2 <= 7 && o3 <= 7 {
				b.WriteByte(o1*64 + o2*8 + o3)
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// isPathUnderMount returns true if path equals mountpoint or is a descendant.
func isPathUnderMount(path, mountpoint string) bool {
	if mountpoint == "/" {
		return strings.HasPrefix(path, "/")
	}
	return path == mountpoint || strings.HasPrefix(path, mountpoint+"/")
}

// findMountByTarget returns the mount entry for an exact mountpoint match.
// When multiple mounts exist at the same target, the last (most recent) is returned.
func findMountByTarget(mnt string) (*MountInfoEntry, error) {
	entries, err := readMountInfo()
	if err != nil {
		return nil, err
	}
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Mountpoint == mnt {
			return entries[i], nil
		}
	}
	return nil, fmt.Errorf("no mount found for target %s", mnt)
}

// findMountContainingPath returns the entry whose mountpoint is the longest
// prefix of path (equivalent to findmnt -T <path>).
func findMountContainingPath(path string) (*MountInfoEntry, error) {
	entries, err := readMountInfo()
	if err != nil {
		return nil, err
	}
	var best *MountInfoEntry
	bestLen := -1
	for i := range entries {
		mp := entries[i].Mountpoint
		if isPathUnderMount(path, mp) && len(mp) > bestLen {
			bestLen = len(mp)
			best = entries[i]
		}
	}
	if best == nil {
		return nil, fmt.Errorf("no mount found containing path %s", path)
	}
	return best, nil
}

// listMountsByPrefix returns entries whose mountpoint starts with prefix.
func listMountsByPrefix(prefix string) ([]*MountInfoEntry, error) {
	entries, err := readMountInfo()
	if err != nil {
		return nil, err
	}
	var result []*MountInfoEntry
	for _, e := range entries {
		if strings.HasPrefix(e.Mountpoint, prefix) {
			result = append(result, e)
		}
	}
	return result, nil
}

// findMountsBySource returns entries whose Source matches source.
func findMountsBySource(source string) ([]*MountInfoEntry, error) {
	entries, err := readMountInfo()
	if err != nil {
		return nil, err
	}
	var result []*MountInfoEntry
	for _, e := range entries {
		if e.Source == source {
			result = append(result, e)
		}
	}
	return result, nil
}

// isMounted returns true if there is a mount at the exact mountpoint.
func isMounted(mnt string) (bool, error) {
	_, err := findMountByTarget(mnt)
	if err != nil {
		return false, nil
	}
	return true, nil
}

// formatMountEntries formats mount entries for debug logging.
func formatMountEntries(entries []*MountInfoEntry) string {
	if len(entries) == 0 {
		return "(no mounts)"
	}
	var lines []string
	for _, e := range entries {
		lines = append(lines, e.String())
	}
	return strings.Join(lines, "\n")
}

// resolveDeviceAttribute looks up a device attribute (UUID, PARTUUID, etc.)
// by scanning a /dev/disk/by-* directory for a symlink resolving to the device.
func resolveDeviceAttribute(devPath, attrDir string) (string, error) {
	devReal, err := resolveDeviceLink(devPath)
	if err != nil {
		return "", fmt.Errorf("cannot resolve device path %s: %w", devPath, err)
	}

	entries, err := os.ReadDir(attrDir)
	if err != nil {
		return "", fmt.Errorf("cannot read %s: %w", attrDir, err)
	}

	for _, e := range entries {
		link := filepath.Join(attrDir, e.Name())
		linkReal, err := resolveDeviceLink(link)
		if err != nil {
			continue
		}
		if linkReal == devReal {
			return e.Name(), nil
		}
	}
	return "", fmt.Errorf("no match for device %s in %s", devPath, attrDir)
}
