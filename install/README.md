# Installation & Management Tools

Welcome to the `install/` directory. These scripts are shipped inside matrixOS to help you manage the lifecycle of your operating system. They handle everything from the initial installation to upgrades and system recovery.

You will typically find these scripts at `/matrixos/install/` on a running system.

## 💿 `install.device`

**The Installer.**

This script installs matrixOS from your current live environment (USB stick) onto a permanent storage device (SSD/HDD).

* **What it does:** It partitions your drive, formats it, and copies the running system to the new drive.
* **How to use:** Run `sudo /matrixos/install/install.device` and follow the prompts.
* **Warning:** This will wipe the target drive!

## 🛠️ `setupOS`

**The First-Boot Wizard.**

You should run this script immediately after installing matrixOS and booting into it for the first time.

* **What it does:**
  * Sets the `root` password.
  * Sets the user password.
  * Sets the LUKS encryption password (if you used encryption).
  * Regenerates SSH host keys for security.
* **How to use:** Run `sudo /matrixos/install/setupOS`.

## ⬆️ `upgrade`

**The Updater.**

This is the recommended way to update your system. It wraps the complex `ostree` commands into a simple utility.

* **What it does:** It pulls the latest updates from the matrixOS servers, stages them, and prepares the system to boot into the new version on the next restart.
* **How to use:** Run `sudo /matrixos/install/upgrade`.

## 🔓 `jailbreak`

**The Escape Hatch.**

matrixOS is immutable by default (you can't easily break the core system). However, if you want full control to modify system files, compile custom kernels, or use Portage directly, you can "jailbreak" the system.

* **What it does:** It converts your immutable OSTree installation into a standard, mutable Gentoo Linux installation.
* **Warning:** This is a **one-way process**. Once you jailbreak, you cannot go back to the automatic OSTree updates. You are responsible for maintaining the system (updates, kernel, etc.) yourself.
* **Prerequisite:** You must switch to a `-full` branch before running this (e.g., `ostree admin switch matrixos/amd64/gnome-full`).
* **How to use:** Run `sudo /matrixos/install/jailbreak`.
