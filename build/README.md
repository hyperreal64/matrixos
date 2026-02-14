# matrixOS Build System

This directory contains the core build system for matrixOS, specifically the "seeders". Seeders are the architectural layers of the operating system, built sequentially to form the final distribution images.

## The Orchestrator: `seeder`

The heart of this directory is the `seeder` script (located at `build/seeder`). This script is responsible for orchestrating the entire build process. It is not just a simple runner; it handles:

* **Dependency Management:** It detects available seeders in `build/seeders/` and executes them in the correct order (lexicographically, e.g., `00-bedrock` -> `10-server`).
* **Environment Setup:** It prepares the build environment, ensuring necessary directories exist and permissions are correct.
* **Locking:** It uses file-based locking to ensure that multiple build processes do not interfere with each other when working on the same seeder.
* **Resumability:** It supports resuming builds from the last successful "phase" inside the chroot, saving massive amounts of time during development.
* **Artifact Management:** It handles the mounting and unmounting of filesystems, including binding the host's package caches (`distfiles`, `binpkgs`) into the build environments.

### Usage

While typically invoked by the high-level `dev/build.sh` or `dev/weekly_builder.sh` wrappers, `seeder` can be run manually for debugging or specific build tasks:

```bash
# Build all seeders
./build/seeder --verbose

# Build only the bedrock layer, resuming if it failed previously
./build/seeder --only-seeders=00-bedrock --resume --verbose

# Skip the bedrock layer
./build/seeder --skip-seeders=00-bedrock
```

## Seeder Architecture

The build process is defined by "seeders" located in `build/seeders/`. Each seeder represents a layer of the OS and contains the following standard components:

1. **`params.sh`**: Defines environment variables specific to the seeder, such as the target chroot directory name.
2. **`prepper.sh`**: A script executed on the **host** system. It prepares the chroot directory. For the base layer (`00-bedrock`), this involves downloading and unpacking the Gentoo Stage3 tarball. For subsequent layers, it usually involves cloning the previous layer's chroot using `cp --reflink=auto`.
3. **`chroot.sh`**: The main build script executed **inside** the chroot. It installs packages, configures system services, and performs customizations.
4. **`packages.conf`** (Optional): A configuration file listing the packages to be installed for that specific layer.

## The Build Library

To avoid code duplication and ensure consistency, the build logic is heavily abstracted into libraries located in `build/seeders/lib/`:

* **`seeders_lib.sh`**: Contains logic for the orchestrator, such as seeder detection, locking mechanisms, and execution wrappers.
* **`preppers_lib.sh`**: Provides functions for the `prepper.sh` scripts, including GPG verification of Stage3 tarballs and safe directory handling.
* **`chroots_lib.sh`**: The most extensive library, used by `chroot.sh`. It manages:
  * **Portage Configuration:** Setting up `make.conf`, repos, and overlays.
  * **Build Phases:** Tracking which steps (bootstrap, kernel, system) have completed.
  * **Package Installation:** Wrappers around `emerge` to handle binary packages and retries.

## Efficiency and Speed

The build system is designed for rapid iteration:

* **Reflinks (Copy-on-Write):** We use `cp --reflink=auto` extensively. When building `10-server` on top of `00-bedrock`, we don't copy the data; we create a lightweight reference. This makes spinning up new build layers instantaneous and saves disk space.
* **Binary Packages:** The system is configured to build and use Gentoo binary packages (`binpkgs`). If a package hasn't changed, it's installed from the cache rather than recompiled.
* **Phase Tracking:** The `chroots_lib.sh` maintains state files inside the chroot. If a build fails at the "build_kernel" phase, running with `--resume` skips the already completed "bootstrap" and "sync" phases.

## Directory Structure

* `seeder`: The main executable script.
* `seeders/`: Contains the layer definitions (e.g., `00-bedrock`, `10-server`, `20-gnome`).
* `seeders/lib/`: Shell libraries for seeders, preppers, and chroot operations.
* `seeders/headers/`: Environment variable definitions and constants.

## Seeder Layers

* **00-bedrock**: The foundation. Starts from a Gentoo Stage3. Builds the kernel, bootloader (GRUB), and essential filesystem tools.
* **10-server**: Adds virtualization support (libvirt), hardware drivers (NVIDIA), and system services.
* **20-gnome**: The desktop layer. Installs GNOME Shell, GUI applications, and desktop-specific configurations.
