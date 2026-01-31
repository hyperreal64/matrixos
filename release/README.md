# matrixOS Release Engineering

Look, if you're here, you're expected to know what you're doing. This isn't a playground. This directory contains the guts of the matrixOS release process. It's not complicated, but it's powerful, so don't mess it up unless you're prepared to clean up your own mess.

The entire system is built around `ostree`. If you don't know what that is, go read the manual. We're not waiting for you. We're creating immutable, bootable, versioned filesystem trees. This is how we achieve reproducible and atomic upgrades.

## The Big Picture

The release process can be boiled down to a simple concept: we take a "seed" (a chroot environment built by the scripts in `build/seeders/`), we clean it, we test it, and then we commit it to an `ostree` repository.

There are two main entry points for this process:

1.  `release.seeds`: This is the main orchestrator. It finds all the builds produced by the seeders (e.g., `bedrock`, `server`, `gnome`) and releases them one by one. This is what you'll use for the automated weekly builds.
2.  `release.current.rootfs`: This is a more specialized tool for releasing the *currently running* root filesystem. It's a clever hack, but a useful one for bootstrapping or creating a "golden image" from a live system. Don't use it unless you know *exactly* what you're doing.

## Efficiency and Speed

This isn't your grandma's build system. We've optimized for both speed and disk space. Here's how:

*   **Reflinks (Copy-on-Write):** When we copy a chroot to prepare it for release, we use `cp --reflink=auto`. On a modern filesystem like Btrfs, this doesn't actually copy any data. It creates a "reflink," which is a lightweight, metadata-only reference to the original data blocks. The copy is instantaneous and consumes zero extra disk space. Data is only copied when a file is modified (Copy-on-Write). This makes the "Sync" step of the release process incredibly fast.

*   **Portage Binary Packages:** The build process (in the `seeders` stage) configures Portage to create binary packages (`--buildpkg`) and use them (`--usepkg`). This means we compile things once and then just reuse the binary. This is especially important during the `post_clean_shrink` phase of the release, where we might need to reinstall packages. Using binpkgs avoids costly recompilations.

*   **`ostree --consume`:** By default, we commit to `ostree` with the `--consume` flag. This tells `ostree` to *move* the files from the image directory into the repository, rather than copying them. Once the commit is done, the temporary image directory is empty, and no disk space is wasted on redundant copies. It's a simple, effective way to keep the build server clean.

## Core Components

### `release_main.sh`

This is the workhorse. It takes a chroot, a temporary image directory, and a branch name, and it does the dirty work of turning that chroot into an `ostree` commit. It's a linear process:

1.  **Sync:** It copies the chroot to a temporary image directory. It's smart enough to use `cp --reflink=auto` for CoW-friendly filesystems, which is a nice touch. Otherwise, it falls back to `rsync`.
2.  **Clean:** It rips out all the junk we don't want in a release: temp files, logs, caches, and other detritus.
3.  **QA:** It runs a series of checks to make sure we're not releasing garbage. This is non-negotiable.
4.  **Configure:** It sets up systemd services and the hostname based on configuration files.
5.  **Commit:** It performs the `ostree commit`.

This script actually does a two-phase commit:

1.  **Full Commit:** It first commits the entire, unadulterated rootfs (minus the junk) to a `-full` branch (e.g., `gnome-full`). This is for developers who need all the symbols and headers.
2.  **Shrink:** It then rips out even *more* stuff (dev headers, static libs, etc.) to create a lean, mean runtime image.
3.  **Shrunken Commit:** It commits the shrunken rootfs to the main branch (e.g., `gnome`). This is what gets deployed to users.

### `release.seeds`

This script is the conductor of the orchestra. It's a wrapper around `release_main.sh` that knows how to deal with our "seeder" architecture.

It iterates through each of the seeders defined in `build/seeders/`, finds the latest build for each one, and then calls `release_main.sh` to do the actual release. It's the glue that connects the build and release processes.

### `release.current.rootfs`

As I said before, this is a special tool. It creates a "jailed" environment by bind-mounting the root filesystem as read-only, then selectively mounts the few directories we need to be writable. It then `chroots` into this jail and runs `release_main.sh`.

It's a testament to the power of Unix that we can do this kind of thing. But with great power comes great responsibility. One wrong move and you could bork your running system. You have been warned.

### `lib/release_lib.sh`

This is where the magic happens. It's a library of shell functions that contains the core logic for the entire release process. If you want to understand the nitty-gritty details, this is the place to look. It's well-structured and does what it's supposed to do. No more, no less.

### `services/` and `hooks/`

These directories provide a clean way to customize the release for different seeds.

*   **`services/`**: Contains `.conf` files that define which systemd services to enable, disable, or mask for a given release. Simple, effective.
*   **`hooks/`**: Contains shell scripts that are executed at specific points in the release process. This allows for arbitrary customizations. For example, the `gnome.sh` hook sets up a default user account for the GNOME live image.

## Usage

For the most part, you shouldn't need to run these scripts manually. The `weekly_builder.sh` script in the `dev/` directory is the intended entry point for automated builds.

However, if you need to do a manual release, you'll use `release.seeds`. For example, to release only the `gnome` seed in `dev` mode, you would run something like:

```bash
./release.seeds -rel=dev -o=20-gnome
```

The script takes a number of arguments to control its behavior. Read the source. It's all there.

## Final Words

This is a solid, no-nonsense release system. It's not fancy, but it's robust and it gets the job done. It's built on sound principles and leverages the power of standard Unix tools.

If you need to make changes, do it carefully. And for God's sake, test your changes before you commit them. We're not here to hold your hand.

(Created by Gemini using yours truly, but using Linus Torvalds style)
