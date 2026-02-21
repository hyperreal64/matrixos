package filesystems

import (
	"testing"

	"golang.org/x/sys/unix"
)

func TestSetAndGetFileCap(t *testing.T) {
	setupMockXattr(t)

	path := "/tmp/fake_binary"

	t.Run("RoundTrip", func(t *testing.T) {
		if err := setFileCap(path, unix.CAP_NET_RAW); err != nil {
			t.Fatalf("setFileCap failed: %v", err)
		}
		has, err := getFileCap(path, unix.CAP_NET_RAW)
		if err != nil {
			t.Fatalf("getFileCap failed: %v", err)
		}
		if !has {
			t.Error("expected CAP_NET_RAW to be set")
		}
	})

	t.Run("DifferentCapNotSet", func(t *testing.T) {
		if err := setFileCap(path, unix.CAP_NET_RAW); err != nil {
			t.Fatal(err)
		}
		has, err := getFileCap(path, unix.CAP_CHOWN)
		if err != nil {
			t.Fatal(err)
		}
		if has {
			t.Error("CAP_CHOWN should not be set")
		}
	})

	t.Run("HighCap", func(t *testing.T) {
		// CAP_CHECKPOINT_RESTORE = 40, lives in the second uint32
		capBit := 40
		if err := setFileCap(path, capBit); err != nil {
			t.Fatal(err)
		}
		has, err := getFileCap(path, capBit)
		if err != nil {
			t.Fatal(err)
		}
		if !has {
			t.Errorf("expected cap %d to be set", capBit)
		}
	})

	t.Run("NoXattr", func(t *testing.T) {
		_, err := getFileCap("/no/such/file", unix.CAP_NET_RAW)
		if err == nil {
			t.Error("expected error for missing xattr")
		}
	})

	t.Run("TooShort", func(t *testing.T) {
		// Write a truncated xattr
		sysLsetxattr("/short", xattrNameCaps, []byte{0, 0, 0}, 0)
		_, err := getFileCap("/short", unix.CAP_NET_RAW)
		if err == nil {
			t.Error("expected error for short xattr")
		}
	})
}
