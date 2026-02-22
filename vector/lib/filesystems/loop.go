package filesystems

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

// Mockable variables for loop-device operations.
var (
	loopControlPath = "/dev/loop-control"
	sysBlockPrefix  = "/sys/block"
	devPrefix       = "/dev"

	// Low-level wrappers; replaced by fakes in tests.
	openFile      = os.OpenFile
	ioctlRetInt   = unix.IoctlRetInt
	ioctlSetInt   = unix.IoctlSetInt
	ioctlLoopInfo = unix.IoctlLoopSetStatus64
	closeFile     = func(f *os.File) error { return f.Close() }
	readFileBytes = os.ReadFile
)

// Loop manages the lifecycle of a Linux loop device backed by an image file.
// All methods are safe for concurrent use.
type Loop struct {
	mu       sync.Mutex
	Path     string // image file path, set at construction
	Device   string // loop device path (e.g. /dev/loop3), set by Attach
	attached bool   // true after a successful Attach, false after Detach
}

// NewLoop returns a Loop for the given image path.
// Use Attach() to associate it with a loop device.
func NewLoop(imagePath string) *Loop {
	return &Loop{Path: imagePath}
}

// NewLoopFromDevice returns a Loop for an already-attached loop device.
// Use BackingFile() / Detach() to query or release it.
func NewLoopFromDevice(device string) *Loop {
	return &Loop{Device: device, attached: true}
}

// BackingFile returns the kernel-reported backing file for the loop device
// by reading /sys/block/loopN/loop/backing_file.
// Returns an empty string when the device has no backing file.
func (l *Loop) BackingFile() string {
	l.mu.Lock()
	dev := l.Device
	l.mu.Unlock()

	if dev == "" {
		return ""
	}
	base := filepath.Base(dev) // "loop0"
	p := filepath.Join(sysBlockPrefix, base, "loop", "backing_file")
	data, err := readFileBytes(p)
	if err != nil {
		return ""
	}
	path := strings.TrimSpace(string(data))
	if path == "" {
		return ""
	}

	return path
}

// Detach clears the file-descriptor association of the loop device,
// equivalent to `losetup -d /dev/loopN`.
// Returns an error if the device was never attached or already detached.
func (l *Loop) Detach() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.attached {
		return fmt.Errorf("loop detach: device not attached")
	}

	f, err := openFile(l.Device, os.O_RDONLY, 0)
	if err != nil {
		return fmt.Errorf("loop detach: open %s: %w", l.Device, err)
	}
	defer closeFile(f)

	if err := ioctlSetInt(int(f.Fd()), unix.LOOP_CLR_FD, 0); err != nil {
		return fmt.Errorf("loop detach: LOOP_CLR_FD on %s: %w", l.Device, err)
	}

	l.attached = false
	return nil
}

// Attach associates the image at l.Path with the next free loop device
// (with partition scanning enabled), equivalent to
// `losetup --show -fP <imagePath>`.
// On success l.Device is set to the allocated device path.
// Returns an error if already attached.
func (l *Loop) Attach() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.attached {
		return fmt.Errorf("loop attach: already attached to %s", l.Device)
	}

	// 1. Get a free loop device number from /dev/loop-control.
	ctl, err := openFile(loopControlPath, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("loop attach: open %s: %w", loopControlPath, err)
	}
	devNr, err := ioctlRetInt(int(ctl.Fd()), unix.LOOP_CTL_GET_FREE)
	closeFile(ctl)
	if err != nil {
		return fmt.Errorf("loop attach: LOOP_CTL_GET_FREE: %w", err)
	}

	loopPath := fmt.Sprintf("%s/loop%d", devPrefix, devNr)

	// 2. Open the backing image file.
	imgFile, err := openFile(l.Path, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("loop attach: open image %s: %w", l.Path, err)
	}
	defer closeFile(imgFile)

	// 3. Open the loop device and associate the image fd with LOOP_SET_FD.
	loopFile, err := openFile(loopPath, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("loop attach: open %s: %w", loopPath, err)
	}
	defer closeFile(loopFile)

	if err := ioctlSetInt(int(loopFile.Fd()), unix.LOOP_SET_FD, int(imgFile.Fd())); err != nil {
		return fmt.Errorf("loop attach: LOOP_SET_FD on %s: %w", loopPath, err)
	}

	// 4. Enable partition scanning via LOOP_SET_STATUS64.
	info := unix.LoopInfo64{
		Flags: unix.LO_FLAGS_PARTSCAN,
	}
	if err := ioctlLoopInfo(int(loopFile.Fd()), &info); err != nil {
		// Best-effort detach on failure.
		_ = ioctlSetInt(int(loopFile.Fd()), unix.LOOP_CLR_FD, 0)
		return fmt.Errorf("loop attach: LOOP_SET_STATUS64 on %s: %w", loopPath, err)
	}

	l.Device = loopPath
	l.attached = true
	return nil
}
