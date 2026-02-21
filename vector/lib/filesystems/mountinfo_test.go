package filesystems

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupMockMountInfo replaces readMountInfo with a function returning the given entries.
func setupMockMountInfo(t *testing.T, entries []*MountInfoEntry) {
	orig := readMountInfo
	readMountInfo = func() ([]*MountInfoEntry, error) {
		return entries, nil
	}
	t.Cleanup(func() { readMountInfo = orig })
}

// setupMockMountInfoFail replaces readMountInfo with a function that always returns an error.
func setupMockMountInfoFail(t *testing.T) {
	orig := readMountInfo
	readMountInfo = func() ([]*MountInfoEntry, error) {
		return nil, fmt.Errorf("mock mountinfo read failure")
	}
	t.Cleanup(func() { readMountInfo = orig })
}

// setupMockDevDisk creates temp directories simulating /dev/disk/by-uuid/ and
// /dev/disk/by-partuuid/, and sets the package-level variables to point to them.
// Returns the uuid and partuuid directory paths.
func setupMockDevDisk(t *testing.T) (uuidDir, partuuidDir string) {
	tmpDir := t.TempDir()
	uuidDir = filepath.Join(tmpDir, "by-uuid")
	partuuidDir = filepath.Join(tmpDir, "by-partuuid")
	if err := os.MkdirAll(uuidDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(partuuidDir, 0755); err != nil {
		t.Fatal(err)
	}

	origUUID := devDiskByUUIDPath
	origPartUUID := devDiskByPartUUIDPath
	devDiskByUUIDPath = uuidDir
	devDiskByPartUUIDPath = partuuidDir

	t.Cleanup(func() {
		devDiskByUUIDPath = origUUID
		devDiskByPartUUIDPath = origPartUUID
	})
	return uuidDir, partuuidDir
}

func TestParseMountInfoLine(t *testing.T) {
	t.Run("Standard", func(t *testing.T) {
		line := "22 1 8:1 / / rw,relatime shared:1 - ext4 /dev/sda1 rw,errors=continue"
		entry, err := parseMountInfoLine(line)
		if err != nil {
			t.Fatalf("parseMountInfoLine failed: %v", err)
		}
		if entry.MountID != 22 {
			t.Errorf("Expected MountID 22, got %d", entry.MountID)
		}
		if entry.ParentID != 1 {
			t.Errorf("Expected ParentID 1, got %d", entry.ParentID)
		}
		if entry.Major != 8 || entry.Minor != 1 {
			t.Errorf("Expected 8:1, got %d:%d", entry.Major, entry.Minor)
		}
		if entry.Root != "/" {
			t.Errorf("Expected Root /, got %s", entry.Root)
		}
		if entry.Mountpoint != "/" {
			t.Errorf("Expected Mountpoint /, got %s", entry.Mountpoint)
		}
		if entry.Options != "rw,relatime" {
			t.Errorf("Expected Options rw,relatime, got %s", entry.Options)
		}
		if entry.FSType != "ext4" {
			t.Errorf("Expected FSType ext4, got %s", entry.FSType)
		}
		if entry.Source != "/dev/sda1" {
			t.Errorf("Expected Source /dev/sda1, got %s", entry.Source)
		}
		if entry.SuperOpts != "rw,errors=continue" {
			t.Errorf("Expected SuperOpts rw,errors=continue, got %s", entry.SuperOpts)
		}
	})

	t.Run("OptionalFields", func(t *testing.T) {
		line := "36 35 98:0 /mnt1 /mnt2 rw,noatime master:1 propagate_from:2 - ext3 /dev/root rw"
		entry, err := parseMountInfoLine(line)
		if err != nil {
			t.Fatalf("parseMountInfoLine failed: %v", err)
		}
		if entry.Mountpoint != "/mnt2" {
			t.Errorf("Expected Mountpoint /mnt2, got %s", entry.Mountpoint)
		}
		if entry.FSType != "ext3" {
			t.Errorf("Expected FSType ext3, got %s", entry.FSType)
		}
		if entry.Source != "/dev/root" {
			t.Errorf("Expected Source /dev/root, got %s", entry.Source)
		}
	})

	t.Run("OctalEscapes", func(t *testing.T) {
		line := `25 1 8:1 / /mnt/my\040dir rw,relatime shared:1 - ext4 /dev/sda1 rw`
		entry, err := parseMountInfoLine(line)
		if err != nil {
			t.Fatalf("parseMountInfoLine failed: %v", err)
		}
		if entry.Mountpoint != "/mnt/my dir" {
			t.Errorf("Expected '/mnt/my dir', got %q", entry.Mountpoint)
		}
	})

	t.Run("Tmpfs", func(t *testing.T) {
		line := "30 22 0:20 / /dev/shm rw,nosuid,nodev - tmpfs tmpfs rw"
		entry, err := parseMountInfoLine(line)
		if err != nil {
			t.Fatalf("parseMountInfoLine failed: %v", err)
		}
		if entry.FSType != "tmpfs" {
			t.Errorf("Expected FSType tmpfs, got %s", entry.FSType)
		}
		if entry.Source != "tmpfs" {
			t.Errorf("Expected Source tmpfs, got %s", entry.Source)
		}
	})

	t.Run("Malformed_NoSeparator", func(t *testing.T) {
		_, err := parseMountInfoLine("no separator here")
		if err == nil {
			t.Error("Expected error for malformed line without separator")
		}
	})

	t.Run("Malformed_TooFewLeftFields", func(t *testing.T) {
		_, err := parseMountInfoLine("22 1 8:1 - ext4 /dev/sda1 rw")
		if err == nil {
			t.Error("Expected error for too few left fields")
		}
	})

	t.Run("Malformed_TooFewRightFields", func(t *testing.T) {
		_, err := parseMountInfoLine("22 1 8:1 / /mnt rw shared:1 - ext4")
		if err == nil {
			t.Error("Expected error for too few right fields")
		}
	})
}

func TestParseMountInfoFile(t *testing.T) {
	content := `22 1 8:1 / / rw,relatime shared:1 - ext4 /dev/sda1 rw,errors=continue
25 22 8:2 / /boot rw,nosuid shared:2 - ext4 /dev/sda2 rw
28 22 8:3 / /boot/efi rw - vfat /dev/sda3 rw,fmask=0077
malformed line without separator
30 22 0:20 / /proc rw,nosuid,nodev,noexec - proc proc rw
`
	tmpFile := filepath.Join(t.TempDir(), "mountinfo")
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	entries, err := parseMountInfoFile(tmpFile)
	if err != nil {
		t.Fatalf("parseMountInfoFile failed: %v", err)
	}
	// 4 valid lines, 1 malformed (skipped)
	if len(entries) != 4 {
		t.Fatalf("Expected 4 entries, got %d", len(entries))
	}
	if entries[0].Mountpoint != "/" {
		t.Errorf("Expected first entry mountpoint /, got %s", entries[0].Mountpoint)
	}
	if entries[1].Mountpoint != "/boot" || entries[1].FSType != "ext4" {
		t.Errorf("Unexpected second entry: %+v", entries[1])
	}
	if entries[2].Mountpoint != "/boot/efi" || entries[2].FSType != "vfat" {
		t.Errorf("Unexpected third entry: %+v", entries[2])
	}
	if entries[3].Mountpoint != "/proc" || entries[3].FSType != "proc" {
		t.Errorf("Unexpected fourth entry: %+v", entries[3])
	}

	t.Run("NonExistentFile", func(t *testing.T) {
		_, err := parseMountInfoFile("/nonexistent/file")
		if err == nil {
			t.Error("Expected error for non-existent file")
		}
	})
}

func TestUnescapeOctal(t *testing.T) {
	tests := []struct {
		input, expected string
	}{
		{"hello", "hello"},
		{`/mnt/my\040dir`, "/mnt/my dir"},
		{`\011tab`, "\ttab"},
		{`\134`, "\\"},
		{`/no\escape`, `/no\escape`},
		{`end\`, `end\`},
		{`a\040\040b`, "a  b"},
		{`/mnt/back\134slash`, `/mnt/back\slash`},
	}
	for _, tt := range tests {
		got := unescapeOctal(tt.input)
		if got != tt.expected {
			t.Errorf("unescapeOctal(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestIsPathUnderMount(t *testing.T) {
	tests := []struct {
		path, mountpoint string
		expected         bool
	}{
		{"/boot/efi/EFI", "/boot/efi", true},
		{"/boot/efi", "/boot/efi", true},
		{"/boot/efimount", "/boot/efi", false},
		{"/boot", "/boot/efi", false},
		{"/home/user", "/", true},
		{"/", "/", true},
		{"/mnt/test/sub", "/mnt/test", true},
		{"/mnt/testing", "/mnt/test", false},
	}
	for _, tt := range tests {
		got := isPathUnderMount(tt.path, tt.mountpoint)
		if got != tt.expected {
			t.Errorf("isPathUnderMount(%q, %q) = %v, want %v",
				tt.path, tt.mountpoint, got, tt.expected)
		}
	}
}

func TestFindMountByTarget(t *testing.T) {
	entries := []*MountInfoEntry{
		{MountID: 1, Mountpoint: "/", Source: "/dev/sda1", FSType: "ext4"},
		{MountID: 2, Mountpoint: "/boot", Source: "/dev/sda2", FSType: "ext4"},
	}

	t.Run("Found", func(t *testing.T) {
		setupMockMountInfo(t, entries)
		entry, err := findMountByTarget("/boot")
		if err != nil {
			t.Fatalf("findMountByTarget failed: %v", err)
		}
		if entry.Source != "/dev/sda2" {
			t.Errorf("Expected /dev/sda2, got %s", entry.Source)
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		setupMockMountInfo(t, entries)
		_, err := findMountByTarget("/nonexistent")
		if err == nil {
			t.Error("Expected error for non-existent target")
		}
	})

	t.Run("LastEntryWins", func(t *testing.T) {
		stacked := []*MountInfoEntry{
			{MountID: 1, Mountpoint: "/mnt", Source: "/dev/sda1"},
			{MountID: 2, Mountpoint: "/mnt", Source: "/dev/sda2"},
		}
		setupMockMountInfo(t, stacked)
		entry, err := findMountByTarget("/mnt")
		if err != nil {
			t.Fatalf("findMountByTarget failed: %v", err)
		}
		if entry.Source != "/dev/sda2" {
			t.Errorf("Expected last entry /dev/sda2, got %s", entry.Source)
		}
	})
}

func TestFindMountContainingPath(t *testing.T) {
	entries := []*MountInfoEntry{
		{MountID: 1, Mountpoint: "/", Source: "/dev/sda1", FSType: "ext4"},
		{MountID: 2, Mountpoint: "/boot", Source: "/dev/sda2", FSType: "ext4"},
		{MountID: 3, Mountpoint: "/boot/efi", Source: "/dev/sda3", FSType: "vfat"},
	}

	t.Run("ExactMatch", func(t *testing.T) {
		setupMockMountInfo(t, entries)
		entry, err := findMountContainingPath("/boot/efi")
		if err != nil {
			t.Fatalf("findMountContainingPath failed: %v", err)
		}
		if entry.Source != "/dev/sda3" {
			t.Errorf("Expected /dev/sda3, got %s", entry.Source)
		}
	})

	t.Run("LongestPrefix", func(t *testing.T) {
		setupMockMountInfo(t, entries)
		entry, err := findMountContainingPath("/boot/efi/EFI/matrixos")
		if err != nil {
			t.Fatalf("findMountContainingPath failed: %v", err)
		}
		if entry.Source != "/dev/sda3" {
			t.Errorf("Expected /dev/sda3, got %s", entry.Source)
		}
	})

	t.Run("FallbackToRoot", func(t *testing.T) {
		setupMockMountInfo(t, entries)
		entry, err := findMountContainingPath("/home/user")
		if err != nil {
			t.Fatalf("findMountContainingPath failed: %v", err)
		}
		if entry.Source != "/dev/sda1" {
			t.Errorf("Expected /dev/sda1, got %s", entry.Source)
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		setupMockMountInfo(t, []*MountInfoEntry{})
		_, err := findMountContainingPath("/mnt")
		if err == nil {
			t.Error("Expected error for no matching mount")
		}
	})
}

func TestListMountsByPrefix(t *testing.T) {
	entries := []*MountInfoEntry{
		{Mountpoint: "/mnt/test"},
		{Mountpoint: "/mnt/test/sub1"},
		{Mountpoint: "/mnt/test/sub2"},
		{Mountpoint: "/mnt/other"},
	}

	t.Run("MatchingPrefix", func(t *testing.T) {
		setupMockMountInfo(t, entries)
		result, err := listMountsByPrefix("/mnt/test")
		if err != nil {
			t.Fatalf("listMountsByPrefix failed: %v", err)
		}
		if len(result) != 3 {
			t.Errorf("Expected 3 entries, got %d", len(result))
		}
	})

	t.Run("NoMatches", func(t *testing.T) {
		setupMockMountInfo(t, entries)
		result, err := listMountsByPrefix("/nonexistent")
		if err != nil {
			t.Fatalf("listMountsByPrefix failed: %v", err)
		}
		if len(result) != 0 {
			t.Errorf("Expected 0 entries, got %d", len(result))
		}
	})
}

func TestFindMountsBySource(t *testing.T) {
	entries := []*MountInfoEntry{
		{Mountpoint: "/", Source: "/dev/sda1"},
		{Mountpoint: "/boot", Source: "/dev/sda2"},
		{Mountpoint: "/data", Source: "/dev/sda1"},
	}

	t.Run("Found", func(t *testing.T) {
		setupMockMountInfo(t, entries)
		result, err := findMountsBySource("/dev/sda1")
		if err != nil {
			t.Fatalf("findMountsBySource failed: %v", err)
		}
		if len(result) != 2 {
			t.Errorf("Expected 2 entries, got %d", len(result))
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		setupMockMountInfo(t, entries)
		result, err := findMountsBySource("/dev/sda99")
		if err != nil {
			t.Fatalf("findMountsBySource failed: %v", err)
		}
		if len(result) != 0 {
			t.Errorf("Expected 0 entries, got %d", len(result))
		}
	})
}

func TestIsMounted(t *testing.T) {
	entries := []*MountInfoEntry{
		{Mountpoint: "/mnt/test"},
	}

	t.Run("Mounted", func(t *testing.T) {
		setupMockMountInfo(t, entries)
		mounted, err := isMounted("/mnt/test")
		if err != nil {
			t.Fatalf("isMounted failed: %v", err)
		}
		if !mounted {
			t.Error("Expected mounted=true")
		}
	})

	t.Run("NotMounted", func(t *testing.T) {
		setupMockMountInfo(t, entries)
		mounted, err := isMounted("/mnt/other")
		if err != nil {
			t.Fatalf("isMounted failed: %v", err)
		}
		if mounted {
			t.Error("Expected mounted=false")
		}
	})
}

func TestResolveDeviceAttribute(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		attrDir := filepath.Join(t.TempDir(), "by-uuid")
		if err := os.MkdirAll(attrDir, 0755); err != nil {
			t.Fatal(err)
		}
		devFile := filepath.Join(t.TempDir(), "device")
		if err := os.WriteFile(devFile, nil, 0644); err != nil {
			t.Fatal(err)
		}

		expectedUUID := "1234-5678"
		if err := os.Symlink(devFile, filepath.Join(attrDir, expectedUUID)); err != nil {
			t.Fatal(err)
		}

		uuid, err := resolveDeviceAttribute(devFile, attrDir)
		if err != nil {
			t.Fatalf("resolveDeviceAttribute failed: %v", err)
		}
		if uuid != expectedUUID {
			t.Errorf("Expected %s, got %s", expectedUUID, uuid)
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		attrDir := filepath.Join(t.TempDir(), "by-uuid")
		if err := os.MkdirAll(attrDir, 0755); err != nil {
			t.Fatal(err)
		}
		devFile := filepath.Join(t.TempDir(), "device")
		if err := os.WriteFile(devFile, nil, 0644); err != nil {
			t.Fatal(err)
		}

		_, err := resolveDeviceAttribute(devFile, attrDir)
		if err == nil {
			t.Error("Expected error for device not found")
		}
	})

	t.Run("DirNotExist", func(t *testing.T) {
		_, err := resolveDeviceAttribute("/dev/null", "/nonexistent/dir")
		if err == nil {
			t.Error("Expected error for missing attribute dir")
		}
	})

	t.Run("DeviceNotExist", func(t *testing.T) {
		attrDir := filepath.Join(t.TempDir(), "by-uuid")
		if err := os.MkdirAll(attrDir, 0755); err != nil {
			t.Fatal(err)
		}
		_, err := resolveDeviceAttribute("/dev/nonexistent_device_xyz", attrDir)
		if err == nil {
			t.Error("Expected error for nonexistent device path")
		}
	})
}

func TestMountInfoEntryString(t *testing.T) {
	e := &MountInfoEntry{
		Mountpoint: "/mnt/test",
		Source:     "/dev/sda1",
		FSType:     "ext4",
		Options:    "rw,relatime",
	}
	s := e.String()
	expected := "TARGET=/mnt/test SOURCE=/dev/sda1 FSTYPE=ext4 OPTIONS=rw,relatime"
	if s != expected {
		t.Errorf("Expected %q, got %q", expected, s)
	}
}

func TestFormatMountEntries(t *testing.T) {
	t.Run("Empty", func(t *testing.T) {
		s := formatMountEntries(nil)
		if s != "(no mounts)" {
			t.Errorf("Expected '(no mounts)', got %q", s)
		}
	})

	t.Run("Multiple", func(t *testing.T) {
		entries := []*MountInfoEntry{
			{Mountpoint: "/a", Source: "s1", FSType: "ext4", Options: "rw"},
			{Mountpoint: "/b", Source: "s2", FSType: "tmpfs", Options: "rw"},
		}
		s := formatMountEntries(entries)
		if !strings.Contains(s, "/a") || !strings.Contains(s, "/b") {
			t.Errorf("Expected both entries in output: %s", s)
		}
	})
}
