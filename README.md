# 👋 Welcome to matrixOS

matrixOS is a Gentoo-based Linux distribution that blends the power and customizability of Gentoo with the reliability of OSTree atomic upgrades. It leverages OSTree to provide **Atomicity** and **Immutability** guarantees, ensuring that updates are applied entirely or not at all, and the base system remains read-only to prevent accidental damage.

It comes with Flatpak, Snap, and Docker ready to go out of the box.

Our two main goals are:

- **Reliability**: Providing a stable, immutable base system through OSTree, which allows for atomic upgrades and rollbacks.
- **Gaming-Friendly**: Shipping with the Steam loader, Lutris, and optimizations to get you gaming on both NVIDIA and AMD GPUs with minimal fuss.

..and our motto is: `emerge once, deploy everywhere`.

TL;DR: Download from: [Cloudflare](https://images.matrixos.org)

<table align="center">
  <tr>
    <td align="center">
      <a href="./screenshots/1.png">
        <img src="./screenshots/1.png" width="250" alt="Desktop 1" />
      </a>
      <br />
      <sub>GNOME Desktop w/Steam and GNOME Software</sub>
    </td>
    <td align="center">
      <a href="./screenshots/2.png">
        <img src="./screenshots/2.png" width="250" alt="Desktop 2" />
      </a>
      <br />
      <sub>System/OS and Flatpak integration</sub>
    </td>
  </tr>
  <tr>
    <td align="center">
      <a href="./screenshots/3.png">
        <img src="./screenshots/3.png" width="250" alt="Terminal" />
      </a>
      <br />
      <sub>OSTree integration</sub>
    </td>
    <td align="center">
      <a href="./screenshots/5.png">
        <img src="./screenshots/5.png" width="250" alt="Dontknow"/>
      </a>
      <br />
      <sub>Coding and AI</sub>
    </td>
  </tr>
</table>

## 🛠️ Disambiguation

- [The OG matrixOS](https://matrixos.ca): A Debian-based distribution shipping with Trinity Desktop.
- [MatrixOS](https://github.com/203-Systems/MatrixOS): An Operating System for Software Defined Controllers.

We need more entropy in this world!

## ⚠️ Disclaimer

matrixOS is a hobby project created for homelab setups. It is **not** intended for mission-critical production environments. Everything in this repository is provided "AS IS" and comes with **NO WARRANTY**.

## ✨ Features

- **Graphics**: Latest Mesa and NVIDIA drivers out of the box.
- **Cooling**: Includes `coolercontrold` and `liquidctl`.
- **Filesystem**: `btrfs` on `/boot` and `/` with zstd compression, auto-resizing on first boot. Includes `ntfsplus` driver.
- **Security**: UEFI SecureBoot support with easy-to-install certificates.
- **Apps**: Steam, Flatpak, Snap, AppImage, and Docker available immediately.
- **Dev**: Ships with `Google Antigravity` for AI-assisted coding.

## 💻 Prerequisites

**Hardware Requirements:**
- **Architecture**: x86_64/amd64 with `x86-64-v3` support (AVX, AVX2, BMP1/2, FMA, etc.).
- **Storage**: At least 32GB (64GB recommended) on USB/SSD/NVMe.

## 💿 Available Images & Keys

Images are available in `raw` (for flashing) and `qcow2` (for VM) formats, compressed with `xz`.
**Trusted Source**: [Cloudflare](https://images.matrixos.org)

### Public Keys
Use these keys to verify the authenticity of images and commits:
- **GPG (OSTree, Images)**: `DC474F4CBD1D3260D9CC6D9275DD33E282BE47CE`
- **SecureBoot Fingerprint**: `sha256 Fingerprint=38:02:D7:FC:A7:6F:08:04:9C:7F:D5:D7:AF:9A:24:6C:9B:C2:28:F3:45:99:7B:DF:79:EE:F3:35:0A:81:87:1B`

## 💿 Installation

### Option 1: Flash to Drive
Download the image (compressed with `xz`) and flash it to your target drive using `dd` or similar tools.

```shell
xz -dc matrixos_amd64_gnome-DATE.img.xz | sudo dd of=/dev/sdX bs=4M status=progress conv=sparse,sync
```

There are two default users:
- **root**: password `matrix`
- **matrix** (UID=1000): password `matrix`
- **LUKS password** (if encrypted): `MatrixOS2026Enc`

### Option 2: Install from matrixOS
Once booted into matrixOS (e.g., from a USB stick), you can install it onto another drive using the built-in installer.

```shell
/matrixos/install/install.device
```

If you are partitioning manually, **strict adherence** to the following layout is required:

1.  **ESP Partition**: Type `ef00` | GUID: `C12A7328-F81F-11D2-BA4B-00A0C93EC93B`
2.  **/boot Partition**: Type `ea00` | GUID: `BC13C2FF-59E6-4262-A352-B275FD6F7172`
3.  **/ Partition**: Type `8304` | GUID: `4F68BCE3-E8CD-4DB1-96E7-FBCAF984B709`

### Post-Installation Setup
After your first boot, run the setup script to configure credentials and LUKS passwords. Run this from a VT or Desktop terminal.

```shell
/matrixos/install/setupOS
reboot
```
To enable Docker: `systemctl enable --now docker`.

### 🔒 SecureBoot
matrixOS supports SecureBoot. You can set it up in two ways:
1.  **Certificate Enrollment**: Enroll the shipped public certificate at first boot via the Shim MOK Manager.
2.  **Manual MOK Enrollment**: Use the provided MOK file (DER binary) to enroll directly into `shim`. The `shim` is signed with Microsoft 2011 and 2023 certificates.

## ⚙️ System Management

matrixOS uses OSTree for atomic updates.

### Upgrades
Update to the latest image:

```shell
ostree admin upgrade
reboot
```
*Wrappers available at `/matrixos/install/upgrade` or `./vector/vector upgrade` (WIP).*

### Rollbacks
If an update fails, simply boot into the previous entry (`ostree:1`). To make it permanent:

```shell
ostree admin pin 1
```

### Branch Switching
List available branches and switch between them (e.g., from `gnome` to `kde` if available):

```shell
ostree remote refs origin
ostree admin switch <another_branch>
reboot
```

### Mutability & Jailbreaking
- **Temporary Mutability**: `ostree admin unlock --hotfix` (resets on upgrade). So that you can run `emerge` as much as you like (important: switch to a `*-full` OSTree branch before doing this).
- **Permanent Jailbreak**: Convert to a standard Gentoo system.
  - List available branches: `ostree remote refs origin`
  - Switch to the `-full` branch: `ostree admin switch <branch>-full && reboot`
  - Run the jailbreak script: `/matrixos/install/jailbreak && reboot`

## 🛠️ Build Your Own Distro

You can build custom versions of matrixOS using the provided `dev/build.sh` script. The build process is: **Seeder -> Releaser -> Imager**. Respectively, the directories are: `build` for Seeder, `release` for Releaser, and `image` for Imager.

### Customization Directories

- **`build/seeders/`**: Contains the build layers (e.g., `00-bedrock`, `10-server`). Each subdirectory has scripts/configs defining packages and settings for that layer.
- **`release/`**: Configuration for the release process.
  - **`hooks/`**: Scripts running at different release stages.
  - **`services/`**: Systemd services to enable/disable/mask.
  - *Note*: `hooks/` and `services/` follow the `OSNAME/ARCH/SEEDER_NAME` pattern (e.g., `matrixos/amd64/gnome`) for branch-specific configs.
- **`image/`**: Configuration for the image creation process.
  - **`hooks/`**: Scripts for partition setup, bootloader install, etc.
  - **`image.releases`**: Defines which releases are built into images.

### Configuration Rules
All configuration is centralized in `conf/matrixos.conf`.
- **Project Info**: OS name, architecture, git repositories.
- **Paths**: Directories for logs, downloads, and output artifacts.
- **Keys**: Paths to GPG and SecureBoot keys lead here.
- **Component Settings**: Specific configs for Seeder, Releaser, and Imager.

**Important**: If you fork this repository to customize builds, update `GitRepo` in `conf/matrixos.conf` to point to your fork.

### Build Workflow
Run the build script as root. It handles the entire pipeline.

```shell
./dev/build.sh
```

- **Resume**: `./dev/build.sh --resume`
- **Force specific steps**: `--force-release`, `--force-images`, `--only-images`
- **Enter a chroot**: `./dev/enter.seed <name>-<date>`
- **Clean artifacts**: `./vector/vector janitor && ./dev/clean_old_builds.sh`

**Resource Requirements**: x86-64-v3 CPU, 32GB+ RAM, ~70GB Disk.

## Known Issues

### GNOME aspect ratio is either 100% or 200%
GNOME 48 currently lacks fine-grained scaling (not getting into details here, but it's fixed in 49). Workaround:
```shell
gsettings set org.gnome.mutter experimental-features "['scale-monitor-framebuffer']"
```

### NVIDIA drivers and nouveau fight
If nouveau loads despite NVIDIA drivers being present:
```shell
ostree admin kargs edit-in-place --append-if-missing=modprobe.blacklist=nouveau
ostree admin kargs edit-in-place --append-if-missing=rd.driver.blacklist=nouveau
```

## 🚀 Roadmap Milestones

The current focus is on **User Friendliness (Milestone 3)** and **New Technologies (Milestone 4)**.

### Milestone 4 (Future)
- [ ] Rewrite core tooling in Go (`vector`) to replace bash scripts.
- [ ] Implement proper CI/CD pipelines and testing.
- [ ] Migrate to `bootc` or wrapper on top of `ostree` + UKI support, moving away from direct `ostree` usage.

## 🙏 Contributing

Contributions are welcome!
- **Code**: helping with the migration to `bootc` or improving CLI tools.
- **Resources**: Mirrors for images/OSTree repo and compute power for builds.
- **Donations**: Please donate to [Gentoo Linux](https://gentoo.org/donate).

## 📄 License

First-party code is released under the **BSD 2-Clause "Simplified" License**.
Third-party applications retain their respective licenses.
