#!/bin/bash
# matrixOS lifecycle filesystem library.
set -eu


fs_lib.get_luks_rootfs_device_path() {
    local luks_name="${1}"
    if [ -z "${luks_name}" ]; then
        echo "fs_lib.get_luks_rootfs_device_path: missing luks_name parameter" >&2
        return 1
    fi
    local rootfs_devmapper="/dev/mapper/${luks_name}"
    echo "${rootfs_devmapper}"
}

fs_lib.device_uuid() {
    local devpath="${1}"
    if [[ -z "${devpath}" ]]; then
        echo "${0}: missing argument devpath." >&2
        return 1
    fi
    blkid -s UUID -o value "${devpath}"
}

fs_lib.device_partuuid() {
    local devpath="${1}"
    if [[ -z "${devpath}" ]]; then
        echo "${0}: missing argument devpath." >&2
        return 1
    fi
    blkid -s PARTUUID -o value "${devpath}"
}

fs_lib.mountpoint_to_device() {
    local mnt="${1}"
    if [ -z "${mnt}" ]; then
        echo "fs_lib.mount_device: missing mnt parameter" >&2
        return 1
    fi
    findmnt -no SOURCES "${mnt}" | head -n 1
}

fs_lib.cleanup_mounts() {
    local mounts=( "${@}" )
    # umount in reverse.
    local len=${#mounts[@]}
    local mnt=
    local i=
    udevadm settle
    for (( i=$len-1; i>=0; i-- )); do
        mnt="${mounts[$i]}"
        local mounted=
        mounted=$(findmnt -n "${mnt}" || true)
        if [ -z "${mounted}" ]; then
            continue
        fi

        echo "Umounting ${mnt} ..."
        if ! umount "${mnt}"; then
            # Try to flush in this case, to reduce corruption %.
            blockdev --flushbufs "${mnt}" || true
            echo "Unable to umount ${mnt}" >&2
            findmnt "${mnt}" 1>&2 || true
            continue
        fi
    done
    udevadm settle
}

fs_lib.cleanup_cryptsetup_devices() {
    local cryptsetup_devices=( "${@}" )
    local cd=
    udevadm settle
    for cd in "${cryptsetup_devices[@]}"; do
        local cdpath=
        cdpath=$(fs_lib.get_luks_rootfs_device_path "${cd}")
        if [ ! -e "${cdpath}" ]; then
            continue
        fi

        echo "Closing LUKS device: ${cd} ..."
        blockdev --flushbufs "${cdpath}" || true
        if ! cryptsetup close "${cd}"; then
            echo "Unable to cryptsetup close ${cdpath}" >&2
            findmnt "${cdpath}" 1>&2 || true
            continue
        fi
    done
    udevadm settle
}

fs_lib.cleanup_loop_devices() {
    local loop_devices=( "${@}" )
    local ld=
    local losetup_find=
    udevadm settle
    for ld in "${loop_devices[@]}"; do
        if [ ! -e "${ld}" ]; then
            continue
        fi
        losetup_find=$(losetup --raw -l -O BACK-FILE "${ld}" | tail -n 1)
        if [ -z "${losetup_find}" ]; then
            continue
        fi
        echo "Cleaning loop device ${ld} ..."
        if ! losetup -d "${ld}"; then
            blockdev --flushbufs "${ld}" || true
            echo "Unable to close loop device ${ld}" >&2
            findmnt "${ld}" 1>&2 || true
            continue
        fi
    done
    udevadm settle
}

fs_lib.list_submounts() {
    local mnt="${1}"
    if [ -z "${mnt}" ]; then
        echo "${0}: missing argument." >&2
        return 1
    fi
    findmnt -rn -o TARGET --submounts \
        --target "${mnt}" | grep "^${mnt}"
}

fs_lib.check_dir_not_fs_root() {
    local mnt="${1}"
    if [ -z "${mnt}" ]; then
        echo "fs_lib.check_dir_not_fs_root: missing mnt parameter" >&2
        return 1
    fi
    # Safety check: Is the inode of the chroot the same as the host root?
    if [[ $(stat -c %i-%Hd-%Ld "${mnt}") -eq $(stat -c %i-%Hd-%Ld /) ]]; then
        echo "CRITICAL ERROR: ${mnt} IS MAPPED TO HOST ROOT. ABORTING." >&2
        return 1
    fi
}

_fs_lib_slave_mounts=(
        /dev
        /dev/pts
        /sys
)

fs_lib.setup_common_rootfs_mounts() {
    if [ -z "${1}" ]; then
        echo "fs_lib.setup_common_rootfs_mounts: missing array parameter" >&2
        return 1
    fi
    local -n __mounts_list="${1}"

    local mnt="${2}"
    if [ -z "${mnt}" ]; then
        echo "fs_lib.setup_common_rootfs_mounts: missing mnt parameter" >&2
        return 1
    fi
    if [ ! -d "${mnt}" ]; then
        echo "${mnt} does not exist ..." >&2
        return 1
    fi

    fs_lib.check_dir_not_fs_root "${mnt}"

    for d in "${_fs_lib_slave_mounts[@]}"; do
        local dst="${mnt%/}${d}"
        mkdir -p "${dst}"
        mount -v --bind "${d}" "${dst}"
        __mounts_list+=( "${dst}" )
        mount -v --make-slave "${dst}"
    done

    local chroot_devshm="${mnt%/}/dev/shm"
    if [ ! -d "${chroot_devshm}" ]; then
        mkdir -p "${chroot_devshm}"
    fi
    mount -v -t tmpfs "devshm" "${chroot_devshm}" \
        -o rw,mode=1777,nosuid,nodev
    __mounts_list+=( "${chroot_devshm}" )

    local chroot_proc="${mnt%/}/proc"
    mkdir -p "${chroot_proc}"
    mount -t proc proc "${chroot_proc}"
    __mounts_list+=( "${chroot_proc}" )

	mkdir -p "${mnt%/}/run/lock"
	mount -v -t tmpfs none "${mnt%/}/run/lock" \
		-o rw,nosuid,nodev,noexec,relatime,size=5120k
    __mounts_list+=( "${mnt%/}/run/lock" )
}

fs_lib.unsetup_common_rootfs_mounts() {
    local mnt="${1}"
    if [ -z "${mnt}" ]; then
        echo "fs_lib.unsetup_common_rootfs_mounts: missing mnt parameter" >&2
        return 1
    fi
    if [ ! -d "${mnt}" ]; then
        echo "${mnt} does not exist ..." >&2
        return 1
    fi

    fs_lib.check_dir_not_fs_root "${mnt}"

    local mounts=()
    local d=
    for d in "${_fs_lib_slave_mounts[@]}"; do
        mounts+=( "${mnt%/}${d}" )
    done

    mounts+=(
        "${mnt%/}/dev/shm"
        "${mnt%/}/proc"
        "${mnt%/}/run/lock"
    )
    fs_lib.cleanup_mounts "${mounts[@]}"
}

fs_lib.bind_mount_distdir() {
    if [ -z "${1}" ]; then
        echo "fs_lib.bind_mount_distdir: missing array parameter" >&2
        return 1
    fi
    local -n _bmd_mounts="${1}"

    local distfiles_dir="${2}"
    if [ -z "${distfiles_dir}" ]; then
        echo "fs_lib.bind_mount_distdir: missing parameter distfiles_dir" >&2
        return 1
    fi
    if [ ! -d "${distfiles_dir}" ]; then
        echo "${distfiles_dir} does not exist ..." >&2
        return 1
    fi

    local rootfs="${3}"
    if [ -z "${rootfs}" ]; then
        echo "fs_lib.bind_mount_distdir: missing rootfs parameter" >&2
        return 1
    fi
    if [ ! -d "${rootfs}" ]; then
        echo "${rootfs} does not exist ..." >&2
        return 1
    fi

    local dst_dir="${rootfs%/}/var/cache/distfiles"
    if [ ! -d "${dst_dir}" ]; then
        mkdir -v -p "${dst_dir}"
    fi
    fs_lib.bind_mount "${!_bmd_mounts}" "${distfiles_dir}" "${dst_dir}"
}

fs_lib.bind_umount_distdir() {
    local rootfs="${1}"
    if [ -z "${rootfs}" ]; then
        echo "fs_lib.bind_umount_distdir: missing rootfs parameter" >&2
        return 1
    fi
    if [ ! -d "${rootfs}" ]; then
        echo "${rootfs} does not exist ..." >&2
        return 1
    fi

    local dst_dir="${rootfs%/}/var/cache/distfiles"
    fs_lib.bind_umount "${dst_dir}"
}

fs_lib.bind_mount_binpkgs() {
    if [ -z "${1}" ]; then
        echo "fs_lib.bind_mount_binpkgs: missing array parameter" >&2
        return 1
    fi
    local -n _bmp_mounts="${1}"

    local binpkgs_dir="${2}"
    if [ -z "${binpkgs_dir}" ]; then
        echo "fs_lib.bind_mount_binpkgs: missing parameter binpkgs_dir" >&2
        return 1
    fi
    if [ ! -d "${binpkgs_dir}" ]; then
        echo "${binpkgs_dir} does not exist ..." >&2
        return 1
    fi

    local rootfs="${3}"
    if [ -z "${rootfs}" ]; then
        echo "fs_lib.bind_mount_binpkgs: missing rootfs parameter" >&2
        return 1
    fi
    if [ ! -d "${rootfs}" ]; then
        echo "${rootfs} does not exist ..." >&2
        return 1
    fi

    local dst_dir="${rootfs%/}/var/cache/binpkgs"
    if [ ! -d "${dst_dir}" ]; then
        mkdir -v -p "${dst_dir}"
    fi
    fs_lib.bind_mount "${!_bmp_mounts}" "${binpkgs_dir}" "${dst_dir}"
}

fs_lib.bind_umount_binpkgs() {
    local rootfs="${1}"
    if [ -z "${rootfs}" ]; then
        echo "fs_lib.bind_umount_binpkgs: missing rootfs parameter" >&2
        return 1
    fi
    if [ ! -d "${rootfs}" ]; then
        echo "${rootfs} does not exist ..." >&2
        return 1
    fi

    local dst_dir="${rootfs%/}/var/cache/binpkgs"
    fs_lib.bind_umount "${dst_dir}"
}

fs_lib.bind_mount() {
    if [ -z "${1}" ]; then
        echo "fs_lib.bind_mount: missing array parameter" >&2
        return 1
    fi
    local -n _bm_mounts="${1}"

    local src="${2}"
    if [ -z "${src}" ]; then
        echo "fs_lib.bind_mount: missing src parameter" >&2
        return 1
    fi
    if [ ! -d "${src}" ]; then
        echo "${src} does not exist ..." >&2
        return 1
    fi
    fs_lib.check_dir_not_fs_root "${src}"

    local dst="${3}"
    if [ -z "${dst}" ]; then
        echo "fs_lib.bind_mount: missing dst parameter" >&2
        return 1
    fi
    if [ ! -d "${dst}" ]; then
        echo "${dst} does not exist ..." >&2
        return 1
    fi
    fs_lib.check_dir_not_fs_root "${dst}"

    mount -v --bind "${src}" "${dst}"
    _bm_mounts+=( "${dst}" )
    mount -v --make-slave "${dst}"
}

fs_lib.bind_umount() {
    local mnt="${1}"
    if [ -z "${mnt}" ]; then
        echo "fs_lib.bind_umount: missing mnt parameter" >&2
        return 1
    fi
    if [ ! -d "${mnt}" ]; then
        echo "${mnt} does not exist ..." >&2
        return 1
    fi
    fs_lib.check_dir_not_fs_root "${mnt}"

    fs_lib.cleanup_mounts "${mnt}"
}

fs_lib.check_fs_capability_support() {
    local test_dir="${1}"
    if [ -z "${test_dir}" ]; then
        echo "preppers_lib._check_capability_support missing parameter." >&2
        return 1
    fi

    local tmp_bin="${test_dir}/.cap_test.$$.bin"
    local tmp_copy="${test_dir}/.cap_test.$$.copy"
    local ret=0

    # Ensure we start clean.
    touch "${tmp_bin}"

    # Try to set the capability.
    if ! setcap 'cap_net_raw+ep' "${tmp_bin}" 2>/dev/null; then
        echo "WARNING: System/FS does not allow setting capabilities." >&2
        rm -f "${tmp_bin}"
        return 1
    fi

    # Copy with archive flags.
    cp -a "${tmp_bin}" "${tmp_copy}" 2>/dev/null

    # Flexible check for the capability string.
    if ! getcap "${tmp_copy}" | grep -q "cap_net_raw[=+]ep"; then
        ret=1
    fi

    rm -f "${tmp_bin}" "${tmp_copy}"
    return "${ret}"
}

# Verify that hardlinks are preserved between source and destination.
# Returns 0 if hardlinks are intact, 1 if they were duplicated/broken.
fs_lib.check_hardlink_preservation() {
    local src="${1}"
    local dst="${2}"
    if [ -z "${src}" ] || [ -z "${dst}" ]; then
        echo "fs_lib.check_hardlink_preservation missing parameter." >&2
        return 1
    fi

    echo "Checking hardlink preservation from ${src} to ${dst}..."

    # with pipefail, we need to ignore SIGPIPE sent from head to sort.
    local test_pair
    test_pair=$(find "${src}" -type f -links +1 -printf '%i %p\n' | sort | head -n 2 || true)

    if [[ -z "${test_pair}" ]]; then
        echo "WARNING: no hardlinked files found in source. Cannot verify." >&2
        return 0
    fi

    # Extract the paths from the pair
    local file1_src=
    local file2_src=
    file1_src=$(echo "${test_pair}" | sed -n '1p' | cut -d' ' -f2-)
    file2_src=$(echo "${test_pair}" | sed -n '2p' | cut -d' ' -f2-)

    # Map those paths to the destination
    local rel_path1="${file1_src#$src}"
    local rel_path2="${file2_src#$src}"

    local file1_dst="${dst%/}/${rel_path1}"
    local file2_dst="${dst%/}/${rel_path2}"

    # Compare Inode numbers in the destination
    local inode1_dst=
    local inode2_dst=
    inode1_dst=$(stat -c '%i' "${file1_dst}" 2>/dev/null)
    inode2_dst=$(stat -c '%i' "${file2_dst}" 2>/dev/null)

    if [[ -z "${inode1_dst}" ]] || [[ -z "${inode2_dst}" ]]; then
        echo "ERROR: unable to determine inode information." >&2
        return 1
    fi

    if [[ "${inode1_dst}" == "${inode2_dst}" ]]; then
        echo "SUCCESS: hardlinks preserved (Inode: ${inode1_dst})."
        return 0
    else
        echo "CRITICAL: hardlinks BROKEN! Files were duplicated." >&2
        echo "  File 1: ${inode1_dst}" >&2
        echo "  File 2: ${inode2_dst}" >&2
        return 1
    fi
}

fs_lib.check_dir_is_root() {
    local chroot_dir="${1}"
    if [ -z "${chroot_dir}" ]; then
        echo "fs_lib.check_dir_is_root missing parameter." >&2
        return 1
    fi

    # Safety check: Is the inode of the chroot the same as the host root?
    if [[ $(stat -c %i "${chroot_dir}") -eq $(stat -c %i /) ]]; then
        echo "CRITICAL ERROR: CHROOT IS MAPPED TO HOST ROOT. ABORTING." >&2
        exit 1
    fi
}

fs_lib.check_dirs_same_filesystem() {
    local src="${1}"
    local dst="${2}"
    if [ -z "${src}" ] || [ -z "${dst}" ]; then
        echo "fs_lib.check_dirs_same_filesystem missing parameter." >&2
        return 1
    fi

    local dev1=
    local dev2=
    dev1=$(stat -c '%d' "${src}")
    dev2=$(stat -c '%d' "${dst}")
    [[ "${dev1}" == "${dev2}" ]]
}

fs_lib.check_active_mounts() {
    local chroot_dir="${1}"
    if [ -z "${chroot_dir}" ]; then
        echo "fs_lib.check_active_mounts missing parameter." >&2
        return 1
    fi

    local active_mounts=
    active_mounts=$(findmnt -rn -o TARGET --submounts --target "${chroot_dir}" \
        | grep "^${chroot_dir}" || true)
    if [ -n "${active_mounts}" ]; then
        echo "[${_seeder_name}] Cannot operate sync to ${chroot_dir}. Active mounts detected:" >&2
        echo "${active_mounts}" >&2
        echo "Please umount them manually." >&2
        return 1
    fi
}

fs_lib.cp_reflink_copy_allowed() {
    local src="${1}"
    local dst="${2}"
    local use_cp_flag="${3}"
    if [ -z "${src}" ] || [ -z "${dst}" ] || [ -z "${use_cp_flag}" ]; then
        echo "fs_lib.cp_reflink_copy_allowed missing parameters." >&2
        return 1
    fi

    if [ -z "${use_cp_flag}" ] || [ "${src}" = "/" ]; then
        return 1
    fi

    fs_lib.check_dirs_same_filesystem "${src}" "${dst}"
    fs_lib.check_fs_capability_support "${src}"
    fs_lib.check_fs_capability_support "${dst}"
}

fs_lib.create_temp_dir() {
    local parent_dir="${1}"
    local prefix="${2:-tmp}"

    mkdir -p "${parent_dir}"
    local new_dir=
    new_dir=$(mktemp -d -p "${parent_dir}" "${prefix}.XXXXXXXXXX")

    if [[ $? -ne 0 || -z "${new_dir}" ]]; then
        echo "${0}: failed to create temporary directory" >&2
        return 1
    fi
    echo "${new_dir}"
}

fs_lib.create_temp_file() {
    local parent_dir="${1}"
    local prefix="${2:-tmp}"

    mkdir -p "${parent_dir}"
    local new_path=
    new_path=$(mktemp -p "${parent_dir}" "${prefix}.XXXXXXXXXX")

    if [[ $? -ne 0 || -z "${new_path}" ]]; then
        echo "${0}: failed to create temporary file" >&2
        return 1
    fi
    echo "${new_path}"
}

fs_lib.removefile_withglob() {
    local target="${1}"
    rm -f ${target}
}

fs_lib.removedir() {
    local target="${1}"
    if [ ! -d "${target}" ]; then
        echo "Cleaning: ${target} does not exist" >&2
        return 1
    fi
    echo "Removing ${target}"
    rm -rf "${target}"
}

fs_lib.emptydir() {
    local target="${1}"
    if [ ! -d "${target}" ]; then
        echo "Cleaning: ${target} does not exist" >&2
        return 1
    fi
    echo "Emptying directory ${target}"
    find "${target}" -mindepth 1 -delete
}

fs_lib.dir_empty() {
    local dir="${1}"
    [[ -d "${dir}" ]] || return 1

    # Save current shell options
    local old_shopt=$(shopt -p nullglob dotglob)

    # nullglob: globs expand to nothing if no match
    # dotglob: include hidden files (.file)
    shopt -s nullglob dotglob

    local files=("$dir"/*)

    # Restore shell options
    eval "${old_shopt}"

    # Check array length
    (( ${#files[@]} == 0 ))
}
