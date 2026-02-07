# matrixOS Imaging Subsystem

This directory contains the tooling responsible for transforming OSTree commits (produced by the `release/` stage) into bootable disk images (artifacts).

While the `release` stage concerns itself with the *content* of the filesystem, the `image` stage concerns itself with the *layout* and *bootability* of the final artifact.

## Core Component: `image.releases`

The primary entry point is `image.releases`. This script orchestrates the creation of disk images for specified OSTree branches.

### Workflow

The imaging process follows a strict pipeline to ensure reproducibility and boot reliability:

1. **Image Allocation**: A sparse file is created to serve as the disk image.
2. **Partitioning**: The image is partitioned using a standard layout (GPT):
    * **ESP (EFI System Partition)**: Formatted VFAT. Contains the bootloader (GRUB/Shim) and kernel images (if using UKI/systemd-boot).
    * **Boot**: Formatted Btrfs. Contains boot loader entries and kernels.
    * **Root**: Formatted Btrfs (LUKS encryption optional). This is where the OSTree deployment lives.
3. **OSTree Deployment**: The script initializes an OSTree repository within the image and performs an `ostree admin deploy`. This checks out the specific commit from the build repository into the physical disk image.
4. **Bootloader Installation**: GRUB is installed to the ESP. SecureBoot shims are copied if `--productionize` is active.
5. **Customization**: Any image-specific tweaks (like generating unique machine IDs or setting default kernel arguments) happen here.
6. **Artifact Generation**: The raw image is optionally converted to QCOW2 (for virtualization) or compressed (XZ) for distribution.

### Usage

This script is typically invoked by `dev/weekly_builder.sh`, but can be run manually for debugging or custom image generation.

```bash
# Generate images for specific releases from the local repo
./image.releases --local-ostree --only-releases=matrixos/amd64/gnome

# Generate production-ready images (compressed, signed artifacts)
./image.releases --local-ostree --productionize --create-qcow2
```

### Key Arguments

* `--local-ostree`: Uses the local OSTree repository (usually in `ostree/repo`) instead of pulling from a remote.
* `--productionize`: Enables steps required for public release, such as compressing the final image and ensuring SecureBoot artifacts are in place.
* `--create-qcow2`: Converts the resulting raw image into a QCOW2 file, optimized for QEMU/KVM usage.
* `--only-releases`: A comma-separated list of branches to build images for.

## Partition Layout

The imaging scripts enforce a specific partition GUID scheme to ensure the OS can identify its own partitions regardless of device node names (`/dev/sda`, `/dev/nvme0n1`, etc.).

* **ESP**: `C12A7328-F81F-11D2-BA4B-00A0C93EC93B`
* **Boot**: `BC13C2FF-59E6-4262-A352-B275FD6F7172`
* **Root**: `4F68BCE3-E8CD-4DB1-96E7-FBCAF984B709`

## Filesystem Features

* **Btrfs**: The root filesystem is formatted as Btrfs.
* **Compression**: Zstd compression is enabled by default on the filesystem level.
* **Growth**: The filesystem is configured to automatically expand to fill the available disk space on first boot.

## Dependencies

The imaging process requires the following tools to be present on the host system:

* `ostree`: Core tool for managing the filesystem trees.
* `qemu-img`: Used for converting raw images to QCOW2 format (`qemu-utils`).
* `xz`: Used for compressing the final raw images.
* `gpg`: Used for signing the OSTree commits and artifacts.
* `btrfs-progs`: Required for formatting Btrfs partitions.
* `dosfstools`: Required for formatting the EFI System Partition (VFAT).
