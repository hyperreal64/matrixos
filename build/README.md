# matrixOS Seeders

This directory contains the "seeders" for matrixOS. Seeders are scripts that build the different layers of the operating system. They are designed to be modular and build upon each other, starting from a base layer and adding more functionality in subsequent layers.

## Concept

The core idea behind seeders is to create a series of system images, each with a specific purpose. This is achieved by running scripts within a chroot environment, which allows for a clean and isolated build process.

The build process is divided into layers:

1.  **Bedrock:** This is the base layer of the OS. It contains a minimal set of packages required for a bootable system, including the kernel, bootloader, and essential system utilities.
2.  **Layers:** These are built on top of the bedrock and add more functionality. For example, there are layers for a server environment and a GNOME desktop environment.
3.  **Leaves:** These are even more specialized layers that can be built on top of other layers. For example, a "GNOME gaming" leaf could be built on top of the GNOME layer.

Each seeder is a directory that contains a set of scripts and configuration files. The main scripts are:

*   `prepper.sh`: This script runs outside the chroot and prepares the environment. For the bedrock layer, this involves downloading and unpacking a Gentoo stage3 tarball.
*   `chroot.sh`: This script runs inside the chroot and performs the actual build process. This includes installing packages, configuring the system, and running any other necessary commands.

## Efficiency and Speed

The build process is optimized for both speed and disk space consumption through several mechanisms:

*   **Reflinks (Copy-on-Write):** When creating copies of filesystems (for example, when preparing a chroot for a new build layer), the system uses `cp --reflink=auto`. A reflink is a lightweight link between two files on a copy-on-write (CoW) filesystem like Btrfs. Instead of creating a full copy of the data, a reflink points to the original data blocks. This is nearly instantaneous and consumes no extra disk space. A new copy of the data is only made when one of the files is modified. This dramatically speeds up the process of creating and tearing down build environments.

*   **Portage Binary Packages:** Gentoo's package manager, Portage, is configured to build and cache binary packages (`--buildpkg`). When a package is built for the first time, a binary version of it is saved. On subsequent builds, if the same version of the package is needed, Portage can simply install the pre-built binary package instead of compiling it from source again. This significantly reduces build times, especially for large packages like the kernel or desktop environments.

*   **OSTree Consume:** While not directly part of the seeder process, it's worth noting that the release process uses `ostree commit --consume`. This means that once a build artifact is committed to the `ostree` repository, the original files are removed from the build cache. This keeps the disk usage of the build system in check by not keeping redundant copies of the built filesystems.

## Seeder Layers

### 00-bedrock

This is the foundational layer of matrixOS. It provides a minimal, bootable system with the following key components:

*   **Kernel:** The Linux kernel for matrixOS.
*   **Bootloader:** GRUB, with matrixOS branding.
*   **Filesystem Tools:** `btrfs-progs`, `xfsprogs`, `dosfstools`, etc.
*   **Containerization:** Docker and its components.
*   **Virtualization:** QEMU and the QEMU guest agent.
*   **OSTree:** A tool for managing bootable, versioned filesystem trees.

### 10-server

This layer builds on the bedrock and adds packages for a server environment. This includes:

*   **Virtualization:** `libvirt` for managing virtual machines.
*   **Printing:** CUPS for print services.
*   **NVIDIA Drivers:** For GPU support.
*   **Power Management:** `power-profiles-daemon` for managing power consumption.

### 20-gnome

This layer builds on the bedrock and provides a full-featured GNOME desktop environment. It includes a wide range of packages, such as:

*   **GNOME Shell:** The main desktop interface.
*   **GNOME Applications:** A suite of applications like Nautilus, Gedit, and GNOME Software.
*   **Hardware Support:** Drivers for various hardware components, including NVIDIA GPUs and wireless adapters.
*   **Development Tools:** `git`, `rust`, `go`, etc.
*   **Games:** A selection of GNOME games.

## Build Process

The build process is managed by the `seeder.sh` script (located in the `build/` directory). This script iterates through the seeder directories in numerical order, executing the `prepper.sh` and `chroot.sh` scripts for each one.

The `chroots_lib.sh` and `preppers_lib.sh` files in the `lib/` directory contain a library of shell functions that are used by the seeder scripts. These functions provide common functionality for tasks such as:

*   **Phase Tracking:** The build process is divided into phases, and the scripts keep track of which phases have been completed. This allows the build to be resumed if it is interrupted.
*   **Package Management:** The scripts use a `packages.conf` file in each seeder directory to determine which packages to install.
*   **Portage Configuration:** The scripts configure Portage, the Gentoo package manager, to use the correct overlays and settings for matrixOS.

The result of the build process is a set of system images that can be deployed to a virtual machine or physical hardware.