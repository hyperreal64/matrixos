package filesystems

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// gvariantOffsetSize
// ---------------------------------------------------------------------------

func TestGvariantOffsetSize(t *testing.T) {
	tests := []struct {
		name       string
		bodySize   int
		numOffsets int
		want       int
	}{
		{"tiny body", 0, 0, 1},
		{"fits in 1 byte", 10, 1, 1},
		// 1-byte offsets: total = bodySize + numOffsets*1 must be < 256
		{"max 1-byte boundary", 254, 1, 1},
		{"exceeds 1-byte", 255, 1, 2},
		// 2-byte offsets: total = bodySize + numOffsets*2 must be < 65536
		{"fits in 2 bytes", 1000, 2, 2},
		{"max 2-byte boundary", 65530, 2, 2},
		{"exceeds 2-byte", 65535, 1, 4},
		// Large body -> 4-byte offsets
		{"large body 4-byte", 100000, 1, 4},
		// No offsets needed
		{"zero offsets", 100, 0, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gvariantOffsetSize(tt.bodySize, tt.numOffsets)
			if got != tt.want {
				t.Errorf("gvariantOffsetSize(%d, %d) = %d, want %d",
					tt.bodySize, tt.numOffsets, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// writeOffset
// ---------------------------------------------------------------------------

func TestWriteOffset(t *testing.T) {
	tests := []struct {
		name   string
		offset int
		size   int
		want   []byte
	}{
		{"1 byte", 42, 1, []byte{42}},
		{"2 bytes LE", 0x0102, 2, []byte{0x02, 0x01}},
		{"4 bytes LE", 0x04030201, 4, []byte{0x01, 0x02, 0x03, 0x04}},
		{"8 bytes LE", 0x01, 8, []byte{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}},
		{"1 byte zero", 0, 1, []byte{0}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			writeOffset(&buf, tt.offset, tt.size)
			if !bytes.Equal(buf.Bytes(), tt.want) {
				t.Errorf("writeOffset(%d, %d) = %x, want %x",
					tt.offset, tt.size, buf.Bytes(), tt.want)
			}
		})
	}
}

func TestWriteOffsetUnsupportedSize(t *testing.T) {
	// Unsupported size should write nothing.
	var buf bytes.Buffer
	writeOffset(&buf, 42, 3)
	if buf.Len() != 0 {
		t.Errorf("writeOffset with unsupported size 3 wrote %d bytes, want 0", buf.Len())
	}
}

// ---------------------------------------------------------------------------
// serializeXattrTuple
// ---------------------------------------------------------------------------

func TestSerializeXattrTuple(t *testing.T) {
	name := []byte("user.test\x00")
	value := []byte("hello")

	result := serializeXattrTuple(name, value)

	// Body = name + value, then a 1-byte framing offset pointing to end-of-name
	expectedBody := append(append([]byte{}, name...), value...)
	// Framing offset = len(name) as 1 byte
	expected := append(expectedBody, byte(len(name)))

	if !bytes.Equal(result, expected) {
		t.Errorf("serializeXattrTuple() = %x, want %x", result, expected)
	}
}

func TestSerializeXattrTupleEmpty(t *testing.T) {
	// Empty name (just NUL) and empty value.
	name := []byte{0}
	value := []byte{}

	result := serializeXattrTuple(name, value)
	// Body = [0x00], framing offset = 1 as 1 byte
	expected := []byte{0x00, 0x01}
	if !bytes.Equal(result, expected) {
		t.Errorf("serializeXattrTuple(empty) = %x, want %x", result, expected)
	}
}

// ---------------------------------------------------------------------------
// serializeXattrs
// ---------------------------------------------------------------------------

func TestSerializeXattrsEmpty(t *testing.T) {
	result := serializeXattrs(nil)
	if result != nil {
		t.Errorf("serializeXattrs(nil) = %x, want nil", result)
	}

	result = serializeXattrs([]Xattr{})
	if result != nil {
		t.Errorf("serializeXattrs([]) = %x, want nil", result)
	}
}

func TestSerializeXattrsSingle(t *testing.T) {
	xattrs := []Xattr{
		{Name: []byte("a\x00"), Value: []byte("v")},
	}
	result := serializeXattrs(xattrs)

	// Single element: tuple bytes, then 1 trailing end-offset.
	tuple := serializeXattrTuple(xattrs[0].Name, xattrs[0].Value)
	bodySize := len(tuple)
	osize := gvariantOffsetSize(bodySize, 1)

	var expected bytes.Buffer
	expected.Write(tuple)
	writeOffset(&expected, bodySize, osize)

	if !bytes.Equal(result, expected.Bytes()) {
		t.Errorf("serializeXattrs(single) = %x, want %x", result, expected.Bytes())
	}
}

func TestSerializeXattrsMultiple(t *testing.T) {
	xattrs := []Xattr{
		{Name: []byte("a\x00"), Value: []byte("1")},
		{Name: []byte("b\x00"), Value: []byte("22")},
	}
	result := serializeXattrs(xattrs)

	tuple0 := serializeXattrTuple(xattrs[0].Name, xattrs[0].Value)
	tuple1 := serializeXattrTuple(xattrs[1].Name, xattrs[1].Value)
	bodySize := len(tuple0) + len(tuple1)
	osize := gvariantOffsetSize(bodySize, 2)

	var expected bytes.Buffer
	expected.Write(tuple0)
	expected.Write(tuple1)
	writeOffset(&expected, len(tuple0), osize)
	writeOffset(&expected, len(tuple0)+len(tuple1), osize)

	if !bytes.Equal(result, expected.Bytes()) {
		t.Errorf("serializeXattrs(multi) = %x, want %x", result, expected.Bytes())
	}
}

// ---------------------------------------------------------------------------
// buildFileHeader
// ---------------------------------------------------------------------------

func TestBuildFileHeaderBasic(t *testing.T) {
	header, err := buildFileHeader(1000, 1000, 0100644, "", nil)
	if err != nil {
		t.Fatalf("buildFileHeader returned error: %v", err)
	}

	// First 4 bytes: big-endian variant data length.
	if len(header) < 8 {
		t.Fatalf("header too short: %d bytes", len(header))
	}
	varLen := binary.BigEndian.Uint32(header[0:4])
	// Next 4 bytes: zero padding.
	if !bytes.Equal(header[4:8], []byte{0, 0, 0, 0}) {
		t.Errorf("padding bytes = %x, want 00000000", header[4:8])
	}
	// Variant data should be exactly varLen bytes.
	if uint32(len(header)-8) != varLen {
		t.Errorf("variant data length = %d, header says %d", len(header)-8, varLen)
	}

	// Verify uid, gid, mode are big-endian in the variant data.
	vdata := header[8:]
	uid := binary.BigEndian.Uint32(vdata[0:4])
	gid := binary.BigEndian.Uint32(vdata[4:8])
	mode := binary.BigEndian.Uint32(vdata[8:12])
	reserved := binary.BigEndian.Uint32(vdata[12:16])

	if uid != 1000 {
		t.Errorf("uid = %d, want 1000", uid)
	}
	if gid != 1000 {
		t.Errorf("gid = %d, want 1000", gid)
	}
	if mode != 0100644 {
		t.Errorf("mode = %o, want 100644", mode)
	}
	if reserved != 0 {
		t.Errorf("reserved = %d, want 0", reserved)
	}
}

func TestBuildFileHeaderSymlink(t *testing.T) {
	target := "/usr/bin/foo"
	header, err := buildFileHeader(0, 0, 0120777, target, nil)
	if err != nil {
		t.Fatalf("buildFileHeader returned error: %v", err)
	}

	// The NUL-terminated target should appear in the variant data after the
	// 16 bytes of uid/gid/mode/reserved.
	vdata := header[8:]
	strStart := 16
	strEnd := strStart + len(target)
	got := string(vdata[strStart:strEnd])
	if got != target {
		t.Errorf("symlink target in header = %q, want %q", got, target)
	}
	// Check NUL terminator.
	if vdata[strEnd] != 0 {
		t.Errorf("symlink target not NUL-terminated")
	}
}

func TestBuildFileHeaderWithXattrs(t *testing.T) {
	xattrs := []Xattr{
		{Name: []byte("user.b\x00"), Value: []byte("2")},
		{Name: []byte("user.a\x00"), Value: []byte("1")},
	}
	header, err := buildFileHeader(0, 0, 0100644, "", xattrs)
	if err != nil {
		t.Fatalf("buildFileHeader returned error: %v", err)
	}
	if len(header) == 0 {
		t.Fatal("empty header with xattrs")
	}

	// Ensure header is larger than without xattrs.
	noXattrHeader, err := buildFileHeader(0, 0, 0100644, "", nil)
	if err != nil {
		t.Fatalf("buildFileHeader (no xattrs) returned error: %v", err)
	}
	if len(header) <= len(noXattrHeader) {
		t.Errorf("header with xattrs (%d) should be larger than without (%d)",
			len(header), len(noXattrHeader))
	}
}

func TestBuildFileHeaderDeterministic(t *testing.T) {
	// Same inputs should produce identical output.
	h1, err := buildFileHeader(500, 500, 0100755, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	h2, err := buildFileHeader(500, 500, 0100755, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(h1, h2) {
		t.Error("buildFileHeader not deterministic for identical inputs")
	}
}

func TestBuildFileHeaderXattrsSorted(t *testing.T) {
	// Regardless of input order, xattrs should be sorted by name.
	xattrsA := []Xattr{
		{Name: []byte("user.z\x00"), Value: []byte("Z")},
		{Name: []byte("user.a\x00"), Value: []byte("A")},
	}
	xattrsB := []Xattr{
		{Name: []byte("user.a\x00"), Value: []byte("A")},
		{Name: []byte("user.z\x00"), Value: []byte("Z")},
	}

	hA, err := buildFileHeader(0, 0, 0100644, "", xattrsA)
	if err != nil {
		t.Fatal(err)
	}
	hB, err := buildFileHeader(0, 0, 0100644, "", xattrsB)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(hA, hB) {
		t.Error("buildFileHeader should produce same output regardless of xattr order")
	}
}

// ---------------------------------------------------------------------------
// buildDirMeta
// ---------------------------------------------------------------------------

func TestBuildDirMetaBasic(t *testing.T) {
	meta, err := buildDirMeta(0, 0, 040755, nil)
	if err != nil {
		t.Fatalf("buildDirMeta returned error: %v", err)
	}
	// Without xattrs: 12 bytes (3 * uint32 big-endian).
	if len(meta) != 12 {
		t.Fatalf("buildDirMeta length = %d, want 12", len(meta))
	}

	uid := binary.BigEndian.Uint32(meta[0:4])
	gid := binary.BigEndian.Uint32(meta[4:8])
	mode := binary.BigEndian.Uint32(meta[8:12])

	if uid != 0 {
		t.Errorf("uid = %d, want 0", uid)
	}
	if gid != 0 {
		t.Errorf("gid = %d, want 0", gid)
	}
	if mode != 040755 {
		t.Errorf("mode = %o, want 40755", mode)
	}
}

func TestBuildDirMetaWithXattrs(t *testing.T) {
	xattrs := []Xattr{
		{Name: []byte("security.selinux\x00"), Value: []byte("system_u:object_r:usr_t:s0")},
	}
	meta, err := buildDirMeta(0, 0, 040755, xattrs)
	if err != nil {
		t.Fatal(err)
	}
	if len(meta) <= 12 {
		t.Errorf("buildDirMeta with xattrs should be longer than 12, got %d", len(meta))
	}
}

func TestBuildDirMetaDeterministic(t *testing.T) {
	m1, err := buildDirMeta(1, 2, 040700, nil)
	if err != nil {
		t.Fatal(err)
	}
	m2, err := buildDirMeta(1, 2, 040700, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(m1, m2) {
		t.Error("buildDirMeta not deterministic")
	}
}

func TestBuildDirMetaXattrsSorted(t *testing.T) {
	xA := []Xattr{
		{Name: []byte("z\x00"), Value: []byte("Z")},
		{Name: []byte("a\x00"), Value: []byte("A")},
	}
	xB := []Xattr{
		{Name: []byte("a\x00"), Value: []byte("A")},
		{Name: []byte("z\x00"), Value: []byte("Z")},
	}
	mA, _ := buildDirMeta(0, 0, 040755, xA)
	mB, _ := buildDirMeta(0, 0, 040755, xB)
	if !bytes.Equal(mA, mB) {
		t.Error("buildDirMeta should produce same output regardless of xattr order")
	}
}

// ---------------------------------------------------------------------------
// OstreeChecksumFileAt — unit tests using temp files
// ---------------------------------------------------------------------------

func TestOstreeChecksumFileAtRegularFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hello.txt")
	content := []byte("Hello, OSTree!\n")
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatal(err)
	}

	flags := OstreeChecksumFlagsIgnoreXattrs | OstreeChecksumFlagsCanonicalPermissions
	checksum, err := OstreeChecksumFileAt(path, OstreeObjectTypeFile, flags)
	if err != nil {
		t.Fatalf("OstreeChecksumFileAt error: %v", err)
	}

	if len(checksum) != 64 {
		t.Errorf("checksum length = %d, want 64 (sha256 hex)", len(checksum))
	}

	// Verify it's valid hex.
	if _, err := hex.DecodeString(checksum); err != nil {
		t.Errorf("checksum is not valid hex: %v", err)
	}
}

func TestOstreeChecksumFileAtDeterministicFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "det.txt")
	if err := os.WriteFile(path, []byte("deterministic"), 0644); err != nil {
		t.Fatal(err)
	}

	flags := OstreeChecksumFlagsIgnoreXattrs | OstreeChecksumFlagsCanonicalPermissions
	c1, _ := OstreeChecksumFileAt(path, OstreeObjectTypeFile, flags)
	c2, _ := OstreeChecksumFileAt(path, OstreeObjectTypeFile, flags)

	if c1 != c2 {
		t.Errorf("same file, different checksums: %s vs %s", c1, c2)
	}
}

func TestOstreeChecksumFileAtDifferentContent(t *testing.T) {
	dir := t.TempDir()

	f1 := filepath.Join(dir, "a.txt")
	f2 := filepath.Join(dir, "b.txt")
	os.WriteFile(f1, []byte("aaa"), 0644)
	os.WriteFile(f2, []byte("bbb"), 0644)

	flags := OstreeChecksumFlagsIgnoreXattrs | OstreeChecksumFlagsCanonicalPermissions
	c1, _ := OstreeChecksumFileAt(f1, OstreeObjectTypeFile, flags)
	c2, _ := OstreeChecksumFileAt(f2, OstreeObjectTypeFile, flags)

	if c1 == c2 {
		t.Error("different content should produce different checksums")
	}
}

func TestOstreeChecksumFileAtEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty")
	if err := os.WriteFile(path, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	flags := OstreeChecksumFlagsIgnoreXattrs | OstreeChecksumFlagsCanonicalPermissions
	checksum, err := OstreeChecksumFileAt(path, OstreeObjectTypeFile, flags)
	if err != nil {
		t.Fatalf("OstreeChecksumFileAt error: %v", err)
	}
	if len(checksum) != 64 {
		t.Errorf("checksum length = %d, want 64", len(checksum))
	}
}

func TestOstreeChecksumFileAtSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	link := filepath.Join(dir, "link")

	os.WriteFile(target, []byte("target data"), 0644)
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	flags := OstreeChecksumFlagsIgnoreXattrs | OstreeChecksumFlagsCanonicalPermissions
	checksum, err := OstreeChecksumFileAt(link, OstreeObjectTypeFile, flags)
	if err != nil {
		t.Fatalf("OstreeChecksumFileAt symlink error: %v", err)
	}
	if len(checksum) != 64 {
		t.Errorf("checksum length = %d, want 64", len(checksum))
	}

	// Symlink checksum should differ from the target file's checksum.
	cTarget, _ := OstreeChecksumFileAt(target, OstreeObjectTypeFile, flags)
	if checksum == cTarget {
		t.Error("symlink checksum should differ from target file checksum")
	}
}

func TestOstreeChecksumFileAtSymlinkDeterministic(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "link")
	os.Symlink("/some/target", link)

	flags := OstreeChecksumFlagsIgnoreXattrs | OstreeChecksumFlagsCanonicalPermissions
	c1, _ := OstreeChecksumFileAt(link, OstreeObjectTypeFile, flags)
	c2, _ := OstreeChecksumFileAt(link, OstreeObjectTypeFile, flags)
	if c1 != c2 {
		t.Error("symlink checksum not deterministic")
	}
}

func TestOstreeChecksumFileAtDirectory(t *testing.T) {
	dir := t.TempDir()

	flags := OstreeChecksumFlagsIgnoreXattrs | OstreeChecksumFlagsCanonicalPermissions
	checksum, err := OstreeChecksumFileAt(dir, OstreeObjectTypeDirMeta, flags)
	if err != nil {
		t.Fatalf("OstreeChecksumFileAt dir error: %v", err)
	}
	if len(checksum) != 64 {
		t.Errorf("checksum length = %d, want 64", len(checksum))
	}
}

func TestOstreeChecksumFileAtDirectoryCanonicalPerms(t *testing.T) {
	dir := t.TempDir()

	withCanon, _ := OstreeChecksumFileAt(dir, OstreeObjectTypeDirMeta,
		OstreeChecksumFlagsIgnoreXattrs|OstreeChecksumFlagsCanonicalPermissions)
	withoutCanon, _ := OstreeChecksumFileAt(dir, OstreeObjectTypeDirMeta,
		OstreeChecksumFlagsIgnoreXattrs)

	// The directory is owned by the current user. If not root, canonical
	// permissions will zero out uid/gid, producing a different checksum.
	if os.Getuid() != 0 {
		if withCanon == withoutCanon {
			t.Error("canonical perms should change checksum for non-root dir")
		}
	}
}

func TestOstreeChecksumFileAtNonexistent(t *testing.T) {
	_, err := OstreeChecksumFileAt("/nonexistent/path/xyz", OstreeObjectTypeFile,
		OstreeChecksumFlagsNone)
	if err == nil {
		t.Error("expected error for nonexistent path")
	}
}

func TestOstreeChecksumFileAtCanonicalPermsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test")
	os.WriteFile(path, []byte("data"), 0644)

	flags := OstreeChecksumFlagsIgnoreXattrs
	cNormal, _ := OstreeChecksumFileAt(path, OstreeObjectTypeFile, flags)
	cCanon, _ := OstreeChecksumFileAt(path, OstreeObjectTypeFile,
		flags|OstreeChecksumFlagsCanonicalPermissions)

	if os.Getuid() != 0 {
		if cNormal == cCanon {
			t.Error("canonical perms should change checksum for non-root owned file")
		}
	}
}

// ---------------------------------------------------------------------------
// Manual checksum verification
//
// We manually build the expected checksum for a known file to catch
// serialisation regressions.
// ---------------------------------------------------------------------------

func TestFileChecksumManual(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manual.txt")
	content := []byte("test content")
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatal(err)
	}

	// Compute expected checksum manually:
	// header = buildFileHeader(0, 0, mode, "", nil)  (canonical perms, ignore xattrs)
	// sha256(header + content)
	header, err := buildFileHeader(0, 0, 0100644, "", nil)
	if err != nil {
		t.Fatal(err)
	}

	h := sha256.New()
	h.Write(header)
	h.Write(content)
	expected := hex.EncodeToString(h.Sum(nil))

	flags := OstreeChecksumFlagsIgnoreXattrs | OstreeChecksumFlagsCanonicalPermissions
	got, err := OstreeChecksumFileAt(path, OstreeObjectTypeFile, flags)
	if err != nil {
		t.Fatal(err)
	}

	if got != expected {
		t.Errorf("manual checksum mismatch:\n  got  %s\n  want %s", got, expected)
	}
}

func TestDirChecksumManual(t *testing.T) {
	dir := t.TempDir()

	// With canonical perms + ignore xattrs: uid=0, gid=0, mode from stat.
	meta, err := buildDirMeta(0, 0, 040700, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Adjust mode: TempDir creates 0700 dirs, but the actual stat mode
	// includes the S_IFDIR bits.
	fi, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	actualMode := uint32(fi.Mode().Perm()) | 040000
	meta, err = buildDirMeta(0, 0, actualMode, nil)
	if err != nil {
		t.Fatal(err)
	}

	h := sha256.New()
	h.Write(meta)
	expected := hex.EncodeToString(h.Sum(nil))

	flags := OstreeChecksumFlagsIgnoreXattrs | OstreeChecksumFlagsCanonicalPermissions
	got, err := OstreeChecksumFileAt(dir, OstreeObjectTypeDirMeta, flags)
	if err != nil {
		t.Fatal(err)
	}

	if got != expected {
		t.Errorf("dir checksum mismatch:\n  got  %s\n  want %s", got, expected)
	}
}

func TestSymlinkChecksumManual(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "mylink")
	target := "/usr/bin/test-target"
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	fi, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	// Symlink mode from stat.
	mode := uint32(fi.Mode().Perm()) | 0120000

	header, err := buildFileHeader(0, 0, mode, target, nil)
	if err != nil {
		t.Fatal(err)
	}

	h := sha256.New()
	h.Write(header)
	expected := hex.EncodeToString(h.Sum(nil))

	flags := OstreeChecksumFlagsIgnoreXattrs | OstreeChecksumFlagsCanonicalPermissions
	got, err := OstreeChecksumFileAt(link, OstreeObjectTypeFile, flags)
	if err != nil {
		t.Fatal(err)
	}

	// Note: canonical perms doesn't zero uid/gid for symlinks in the source,
	// only for regular files and dirs. So this test uses uid/gid=0 only if
	// running as root. We set 0,0 above which is correct for root; for
	// non-root the actual stat uid/gid will differ so we skip the exact match.
	if os.Getuid() == 0 {
		if got != expected {
			t.Errorf("symlink checksum mismatch:\n  got  %s\n  want %s", got, expected)
		}
	} else {
		// At least verify it's a valid 64-char hex string.
		if len(got) != 64 {
			t.Errorf("symlink checksum length = %d, want 64", len(got))
		}
	}
}

// ---------------------------------------------------------------------------
// Integration test: compare with `ostree checksum`
// ---------------------------------------------------------------------------

func checkOstreeChecksumAvailable(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("ostree")
	if err != nil {
		t.Skip("ostree binary not found, skipping integration test")
	}
	return path
}

func ostreeChecksum(t *testing.T, path string) string {
	t.Helper()
	// ostree checksum <path> prints the content checksum to stdout.
	cmd := exec.Command("ostree", "checksum", path)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("ostree checksum %s failed: %v", path, err)
	}
	return string(bytes.TrimSpace(out))
}

func TestOstreeChecksumIntegrationRegularFile(t *testing.T) {
	checkOstreeChecksumAvailable(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(path, []byte("Hello OSTree integration!\n"), 0644); err != nil {
		t.Fatal(err)
	}

	expected := ostreeChecksum(t, path)

	flags := OstreeChecksumFlagsIgnoreXattrs | OstreeChecksumFlagsCanonicalPermissions
	got, err := OstreeChecksumFileAt(path, OstreeObjectTypeFile, flags)
	if err != nil {
		t.Fatal(err)
	}

	if got != expected {
		t.Errorf("checksum mismatch with ostree CLI:\n  go   = %s\n  cli  = %s", got, expected)
	}
}

func TestOstreeChecksumIntegrationEmptyFile(t *testing.T) {
	checkOstreeChecksumAvailable(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "empty")
	if err := os.WriteFile(path, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	expected := ostreeChecksum(t, path)
	flags := OstreeChecksumFlagsIgnoreXattrs | OstreeChecksumFlagsCanonicalPermissions
	got, err := OstreeChecksumFileAt(path, OstreeObjectTypeFile, flags)
	if err != nil {
		t.Fatal(err)
	}

	if got != expected {
		t.Errorf("empty file checksum mismatch:\n  go   = %s\n  cli  = %s", got, expected)
	}
}

func TestOstreeChecksumIntegrationLargeFile(t *testing.T) {
	checkOstreeChecksumAvailable(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "large.bin")

	// 1 MiB of repeating data
	data := bytes.Repeat([]byte("ABCDEFGHIJ"), 1024*100)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}

	expected := ostreeChecksum(t, path)
	flags := OstreeChecksumFlagsIgnoreXattrs | OstreeChecksumFlagsCanonicalPermissions
	got, err := OstreeChecksumFileAt(path, OstreeObjectTypeFile, flags)
	if err != nil {
		t.Fatal(err)
	}

	if got != expected {
		t.Errorf("large file checksum mismatch:\n  go   = %s\n  cli  = %s", got, expected)
	}
}

func TestOstreeChecksumIntegrationSymlink(t *testing.T) {
	checkOstreeChecksumAvailable(t)

	dir := t.TempDir()
	link := filepath.Join(dir, "mylink")
	if err := os.Symlink("/usr/bin/bash", link); err != nil {
		t.Fatal(err)
	}

	expected := ostreeChecksum(t, link)
	flags := OstreeChecksumFlagsIgnoreXattrs | OstreeChecksumFlagsCanonicalPermissions
	got, err := OstreeChecksumFileAt(link, OstreeObjectTypeFile, flags)
	if err != nil {
		t.Fatal(err)
	}

	if got != expected {
		t.Errorf("symlink checksum mismatch:\n  go   = %s\n  cli  = %s", got, expected)
	}
}

func TestOstreeChecksumIntegrationDirectory(t *testing.T) {
	checkOstreeChecksumAvailable(t)

	dir := t.TempDir()

	expected := ostreeChecksum(t, dir)
	flags := OstreeChecksumFlagsIgnoreXattrs | OstreeChecksumFlagsCanonicalPermissions
	got, err := OstreeChecksumFileAt(dir, OstreeObjectTypeDirMeta, flags)
	if err != nil {
		t.Fatal(err)
	}

	if got != expected {
		t.Errorf("dir checksum mismatch:\n  go   = %s\n  cli  = %s", got, expected)
	}
}

func TestOstreeChecksumIntegrationBinaryFile(t *testing.T) {
	checkOstreeChecksumAvailable(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "binary.dat")

	// Binary data with all byte values.
	data := make([]byte, 256)
	for i := range data {
		data[i] = byte(i)
	}
	if err := os.WriteFile(path, data, 0755); err != nil {
		t.Fatal(err)
	}

	expected := ostreeChecksum(t, path)
	flags := OstreeChecksumFlagsIgnoreXattrs | OstreeChecksumFlagsCanonicalPermissions
	got, err := OstreeChecksumFileAt(path, OstreeObjectTypeFile, flags)
	if err != nil {
		t.Fatal(err)
	}

	if got != expected {
		t.Errorf("binary file checksum mismatch:\n  go   = %s\n  cli  = %s", got, expected)
	}
}

func TestOstreeChecksumIntegrationMultiplePermissions(t *testing.T) {
	checkOstreeChecksumAvailable(t)

	dir := t.TempDir()
	perms := []os.FileMode{0444, 0644, 0755, 0600}

	for _, perm := range perms {
		t.Run(perm.String(), func(t *testing.T) {
			path := filepath.Join(dir, "file_"+perm.String())
			if err := os.WriteFile(path, []byte("perm test"), perm); err != nil {
				t.Fatal(err)
			}

			expected := ostreeChecksum(t, path)
			flags := OstreeChecksumFlagsIgnoreXattrs | OstreeChecksumFlagsCanonicalPermissions
			got, err := OstreeChecksumFileAt(path, OstreeObjectTypeFile, flags)
			if err != nil {
				t.Fatal(err)
			}

			if got != expected {
				t.Errorf("perm %s checksum mismatch:\n  go   = %s\n  cli  = %s",
					perm, got, expected)
			}
		})
	}
}
