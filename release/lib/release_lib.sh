#!/bin/bash
set -eu

source "${MATRIXOS_DEV_DIR:-/matrixos}/headers/env.include.sh"
source "${MATRIXOS_DEV_DIR}/build/seeders/headers/seedersenv.include.sh"
source "${MATRIXOS_DEV_DIR}/build/seeders/headers/preppersenv.include.sh"
source "${MATRIXOS_DEV_DIR}/release/headers/releaserenv.include.sh"

source "${MATRIXOS_DEV_DIR}/lib/fs_lib.sh"
source "${MATRIXOS_DEV_DIR}/lib/ostree_lib.sh"
source "${MATRIXOS_DEV_DIR}/lib/qa_lib.sh"


_check_imagedir() {
    local imagedir="${1}"
    if [ -z "${imagedir}" ]; then
        echo "imagedir is empty" >&2
        return 1
    fi
    if [ ! -d "${imagedir}" ]; then
        echo "imagedir is not found" >&2
        return 1
    fi
    return 0
}

_get_ostree_verbosity() {
    local verbose="${1}"
    local verbose_args=
    if [ "${verbose}" = "1" ]; then
        verbose_args="--verbose"
    fi
    echo ${verbose_args}
}

_get_rsync_verbosity() {
    local verbose="${1}"
    local verbose_args=
    if [ "${verbose}" = "1" ]; then
        verbose_args="--verbose --partial --progress"
    fi
    echo ${verbose_args}
}

release_lib.check_matrixos() {
    if [ ! -d "${MATRIXOS_DEV_DIR}" ]; then
        echo "matrixOS dev dir: ${MATRIXOS_DEV_DIR} does not exit" >&2
        return 1
    fi
    qa_lib.check_matrixos_private "${MATRIXOS_PRIVATE_GIT_REPO_PATH}"
}

release_lib._cp_reflink_copy() {
    local src="${1}"
    local dst="${2}"
    local verbose_mode="${3}"

    local excludes=()
    release_lib.get_sync_excluded_paths "excludes" "${src}" "${dst}"
    if [[ ${#excludes[@]} -eq 0 ]]; then
        echo "Unable to get sync excluded paths" >&2
        return 1
    fi

    echo "Removing ${dst} ..."
    # It is safe to remove dst because parent func already checked if we have
    # active mounts.
    rm -rf "${dst}"

    echo "Spawning cp --preserve=links --reflink=auto from ${src} to ${dst} ..."
    cp -a --preserve=links --reflink=auto "${src}" "${dst}"

    echo "Copy with --reflink=auto complete."
    echo "Removing the following paths:"
    local d=
    for d in "${excludes[@]}"; do
        echo "  ${d}"
    done

    # These directories are already prefixed with dst, but check to be sure.
    for d in "${excludes[@]}"; do
        if [[ "${d}" != "${dst}"* ]]; then
            echo "ERROR: Path ${d} is outside of ${dst}!" >&2
            return 1
        fi
    done

    # We could use ostree commit --skip-list=PATH but I think this is safer.
    for d in "${excludes[@]}"; do
        echo "Removing ${d}"
        # do not quote because we have globs.
        rm -rf ${d}
    done
}

release_lib._rsync_copy() {
    local src="${1}"
    local dst="${2}"
    local verbose_mode="${3}"

    local verbose_args
    verbose_args=$(_get_rsync_verbosity "${verbose_mode}")

    if [ "${src}" = "/" ]; then
        # Backward compat.
        # Make sure they end with /.
        dst="${dst%/}/"
    else
        # Make sure they end with /.
        src="${src%/}/"
        dst="${dst%/}/"
    fi

    local excludes=()
    release_lib.get_sync_excluded_paths "excludes" "${src}" "${dst}"
    if [[ ${#excludes[@]} -eq 0 ]]; then
        echo "Unable to get sync excluded paths" >&2
        return 1
    fi

    local exclude_flags=()
    local exc=
    for exc in "${excludes[@]}"; do
        exclude_flags+=( --exclude="${exc}" )
    done

    local rsync_args=(
        --archive
        ${verbose_args}
        --no-D
        --numeric-ids
        --delete-during
        --one-file-system
        "${exclude_flags[@]}"
        "${src}" "${dst}"
    )
    echo "Running: rsync ${rsync_args[@]}"
    rsync "${rsync_args[@]}"
}

release_lib.get_sync_excluded_paths() {
    if [ -z "${1}" ]; then
        echo "get_sync_excluded_paths <target array?> <src> <dst>" >&2
        return 1
    fi
    local -n __target_ref="${1}"

    local src="${2}"
    if [ -z "${src}" ]; then
        echo "get_sync_excluded_paths <target array> <src?> <dst>" >&2
        return 1
    fi
    local dst="${3}"
    if [ -z "${dst}" ]; then
        echo "get_sync_excluded_paths <target array> <src> <dst?>" >&2
        return 1
    fi

    local __internal_list=()
    local ostreedir="${MATRIXOS_OSTREE_REPO_DIR}"
    __internal_list+=(
        "${dst%/}${ostreedir}"
        # "${dst%/}/var/db/repos/*" -- this is needed to emerge --depclean later.
        "${dst%/}/tmp/*"
        "${dst%/}${MATRIXOS_SEEDERS_BUILD_ARTIFACTS_DIR}"
        "${dst%/}${MATRIXOS_PREPPERS_BUILD_ARTIFACTS_DIR}"
        "${dst%/}/var/spool/nullmailer/trigger"
        "${dst%/}/var/tmp/portage/"
        "${dst%/}/var/cache/binpkgs/*"
        "${dst%/}/var/cache/distfiles/*"
    )
    __target_ref=( "${__internal_list[@]}" )
}

release_lib.sync_filesystem() {
    local chroot_dir="${1}"
    local imagedir="${2}"
    local verbose_mode="${3}"
    if [ -z "${chroot_dir}" ] || [ -z "${verbose_mode}" ] || [ -z "${imagedir}" ]; then
        echo "${0}: <chroot_dir> <image dir> <verbose mode 0/1>" >&2
        return 1
    fi
    if [ "${chroot_dir}" = "${imagedir}" ]; then
        echo "chroot dir = imagedir = ${imagedir}" >&2
        return 1
    fi

    mkdir -p "${imagedir}"
    _check_imagedir "${imagedir}"

    fs_lib.check_dir_is_root "${imagedir}"
    fs_lib.check_active_mounts "${imagedir}"

    # Active mounts check already done by sanity_check_chroot_dir.

    local use_cp=${MATRIXOS_RELEASER_USE_CP_REFLINK_MODE_INSTEAD_OF_RSYNC}

    if fs_lib.cp_reflink_copy_allowed "${chroot_dir}" "${imagedir}" "${use_cp}"; then
        echo "Using experimental cp --reflink=auto copy mode ..."
        echo "Note that this is faster for paths (rm will take no time)"
        release_lib._cp_reflink_copy "${chroot_dir}" "${imagedir}" "${verbose_mode}"
    else
        echo "Using rsync copy mode ..."
        release_lib._rsync_copy "${chroot_dir}" "${imagedir}" "${verbose_mode}"
    fi

    fs_lib.check_hardlink_preservation "${chroot_dir}" "${imagedir}"
}

release_lib.pre_clean_qa_checks() {
    local imagedir="${1}"
    _check_imagedir "${imagedir}"

    echo "Pre clean QA Checks ..."

    local sbcert_path="${MATRIXOS_SECUREBOOT_CERT_PATH}"
    qa_lib.verify_distro_rootfs_environment_setup "${imagedir}"
    qa_lib.check_secureboot "${imagedir}" "${sbcert_path}"
    qa_lib.check_number_of_kernels "${imagedir}" "1"
    qa_lib.check_nvidia_module "${imagedir}"
    qa_lib.check_ryzen_smu_module "${imagedir}"

    echo "Pre clean QA Checks complete"
}

release_lib.clean_rootfs() {
    local imagedir="${1}"
    _check_imagedir "${imagedir}"

    # Just copy the certs, from MOS private dir, so that they can be
    # shipped with the image and used to boot on SecureBoot-enabled machines.
    cp "${MATRIXOS_PRIVATE_GIT_REPO_PATH}/secureboot/keys/db/db.pem" \
        "${imagedir}/etc/portage/secureboot.pem"
    cp "${MATRIXOS_PRIVATE_GIT_REPO_PATH}/secureboot/keys/KEK/KEK.pem" \
        "${imagedir}/etc/portage/secureboot-kek.pem"

    local removedirs=(
        /root/.bash_history
        /root/.ssh
        /root/.gnupg
        /root/.cache
        /root/.local
        /var/lib/gdm/.cache
        /var/lib/gdm/.local
        /var/lib/gdm/.config
        "${MATRIXOS_DEFAULT_PRIVATE_GIT_REPO_PATH}"
        /var/lib/sbctl/keys
        /var/tmp/ostree-gpg-private
    )
    local emptydirs=(
        /tmp
        /dev
        /boot
        /root
        /var/lib/systemd/coredump
        /var/tmp/portage
    )
    local removefiles=(
        /etc/resolv.conf
        /etc/portage/secureboot.x509
        /root/.bash_history
        /root/.lesshst
        /root/.bashrc
        /root/.bash_history
        /root/.xauth*
        /var/lib/sbctl/keys
    )

    for d in "${removedirs[@]}"; do
        fs_lib.removedir "${imagedir}${d}" || true
    done
    for d in "${emptydirs[@]}"; do
        fs_lib.emptydir "${imagedir}${d}" || true
    done
    for p in "${removefiles[@]}"; do
        fs_lib.removefile_withglob "${imagedir}${p}" || true
    done

    # Prepare Portage
    mkdir -p "${imagedir}"/var/db/repos/gentoo || true

    return 0
}

release_lib.setup_hostname() {
    local imagedir="${1}"
    _check_imagedir "${imagedir}"

    local hostname="${MATRIXOS_RELEASE_HOSTNAME}"
    if [ -z "${hostname}" ]; then
        echo "${0}: hostname not set. Skipping..." >&2
        return 0
    fi
    echo "Setting hostname to: ${hostname}"
    echo "${hostname}" > "${imagedir}/etc/hostname"
}

release_lib.setup_services() {
    local imagedir="${1}"
    _check_imagedir "${imagedir}"

    if [ -z "${2}" ]; then
        echo "release_lib.setup_services: missing mounts parameter" >&2
        return 1
    fi
    local -n _ss_mounts="${2}"

    local branch="${3}"
    if [ -z "${branch}" ]; then
        echo "release_lib.setup_services: missing branch parameter" >&2
        return 1
    fi

    local services_setup_file="${MATRIXOS_DEV_DIR}/release/services/${branch}.conf"
    if [ ! -e "${services_setup_file}" ]; then
        echo "Services setup file ${services_setup_file} does not exist. Skipping ..." >&2
        return 0
    fi

    local services_to_enable=()
    local services_to_disable=()
    local services_to_mask=()
    local global_service_presets_to_enable=()
    local global_service_presets_to_disable=()
    local global_service_presets_to_mask=()
    local default_targets=()  # there should be only one.

    local lines=()
    # Read the file, skip comments and spaces.
    readarray -t lines < <(grep -vE '^[[:space:]]*($|#)' "${services_setup_file}")

    for line in "${lines[@]}"; do
        local action=
        local remainder=

        # 'action' gets the first word (e.g., enable)
        read -r action remainder <<< "${line}"

        # Now read the reset.
        local services=()
        read -ra services <<< "${remainder}"

        case "${action}" in
            "enable")
                services_to_enable+=( "${services[@]}" )
                ;;
            "disable")
                services_to_disable+=( "${services[@]}" )
                ;;
            "mask")
                services_to_mask+=( "${services[@]}" )
                ;;
            "preset-enable")
                global_service_presets_to_enable+=( "${services[@]}" )
                ;;
            "preset-disable")
                global_service_presets_to_disable+=( "${services[@]}" )
                ;;
            "preset-mask")
                global_service_presets_to_disable+=( "${services[@]}" )
                ;;
            "set-default")
                default_targets+=( "${services[@]}" )
                ;;
            *)
                echo "Unrecognized line in ${line}: ${services_setup_file}" >&2
                ;;
        esac
    done

    local skip_proc="1"
    fs_lib.setup_common_rootfs_mounts "${!_ss_mounts}" "${imagedir}" "${skip_proc}"

    # Use the weird syntax to run systemctl in chroot and capture the exit code properly
    # This way we force the disabling of the exec() optimization of /bin/sh -c and
    # we get systemctl behave and write to std* instead of thinking it's PID 1 and using
    # /dev/kmsg.
    for svc in "${services_to_enable[@]}"; do
        echo "Enabling service: ${svc}"
        fs_lib.chroot "${imagedir}" /bin/sh -c "systemctl enable ${svc}; exit \$?" || true
    done
    for svc in "${services_to_disable[@]}"; do
        echo "Disabling service: ${svc}"
        fs_lib.chroot "${imagedir}" /bin/sh -c "systemctl disable ${svc}; exit \$?" || true
    done
    for svc in "${services_to_mask[@]}"; do
        echo "Masking service: ${svc}"
        fs_lib.chroot "${imagedir}" /bin/sh -c "systemctl mask ${svc}; exit \$?" || true
    done

    for svc in "${global_service_presets_to_enable[@]}"; do
        echo "Preset enabling for service: ${svc}"
        fs_lib.chroot "${imagedir}" /bin/sh -c "systemctl --global enable ${svc}; exit \$?" || true
    done
    for svc in "${global_service_presets_to_disable[@]}"; do
        echo "Preset disabling for service: ${svc}"
        fs_lib.chroot "${imagedir}" /bin/sh -c "systemctl --global disable ${svc}; exit \$?" || true
    done
    for svc in "${global_service_presets_to_mask[@]}"; do
        echo "Preset masking for service: ${svc}"
        fs_lib.chroot "${imagedir}" /bin/sh -c "systemctl --global mask ${svc}; exit \$?" || true
    done

    # check if we have multiple default targets
    if [ ${#default_targets[@]} -gt 1 ]; then
        echo "WARNING: multiple default targets set, picking the last one" >&2
    fi
    if [ ${#default_targets[@]} -gt 0 ]; then
        # pick the last default_targets element
        local last_default_target="${default_targets[-1]}"
        echo "Setting default target to: ${last_default_target}"
        fs_lib.chroot "${imagedir}" /bin/sh -c "systemctl set-default ${last_default_target}; exit \$?" || true
    fi

    fs_lib.unsetup_common_rootfs_mounts "${imagedir}"
}

release_lib.release_hook() {
    local imagedir="${1}"
    _check_imagedir "${imagedir}"

    local branch="${2}"
    if [ -z "${branch}" ]; then
        echo "release_lib.release_hook <branch>" >&2
        return 1
    fi

    local hook_exec="${MATRIXOS_RELEASE_HOOKS_DIR}/${branch}.sh"
    if [ ! -f "${hook_exec}" ]; then
        echo "Release hook ${hook_exec} does not exist. Skipping ..." >&2
        return 0
    fi
    echo "Running release hook ${hook_exec} ..."
    (
        export CHROOT_DIR="${imagedir}"
        "${hook_exec}"
    )
}

release_lib.post_clean_qa_checks() {
    local imagedir="${1}"
    _check_imagedir "${imagedir}"

    echo "Post-clean QA Checks ..."

    echo "Listing the top 20 largest packages:"
    fs_lib.chroot "${imagedir}" \
        equery size '*' | sed 's/\(.*\):.*(\(.*\))$/\2 \1/' \
            | sort -n | numfmt --to=iec-i | tail -n 20

}

release_lib.post_clean_shrink() {
    local imagedir="${1}"
    _check_imagedir "${imagedir}"

    if [ -z "${2}" ]; then
        echo "release_lib.post_clean_shrink: missing mounts parameter" >&2
        return 1
    fi
    local -n _pcs_mounts="${2}"

    local branch_name="${3}"
    if [ -z "${branch_name}" ]; then
        echo "release_lib.post_clean_shrink: missing branch parameter" >&2
        return 1
    fi

    echo "Shrinking the rootfs to save space ..."

    local skip_proc="1"
    fs_lib.setup_common_rootfs_mounts "${!_pcs_mounts}" "${imagedir}" "${skip_proc}"
    # remove everything --with-bdeps=n not in new world file, for the not-full branch.
    fs_lib.chroot "${imagedir}" emerge --depclean --with-bdeps=n --complete-graph
    fs_lib.unsetup_common_rootfs_mounts "${imagedir}"

    # Note: /usr/src presence is required by some snap binaries. So, keep the dir around.
    # Note: we keep /usr/var-db-pkg (MATRIXOS_RO_VDB) so that the canonical gentoo tooling
    #       and our upgrader CLI can understand what's installed.
    local removedirs=(
        /usr/include
    )
    local emptydirs=(
        /usr/lib/pkgconfig
        /var/db/repos
        /usr/src
    )

    for d in "${removedirs[@]}"; do
        fs_lib.removedir "${imagedir}${d}" || true
    done
    for d in "${emptydirs[@]}"; do
        fs_lib.emptydir "${imagedir}${d}" || true
    done

    echo "Removing all {.a,.la} files"
    find "${imagedir}/usr" -type f \( -name "*.a" -o -name "*.la" \) -delete

    echo "Shrink completed."
}

release_lib.initialize_gpg() {
    local gpg_enabled="${1}"
    if [ -z "${gpg_enabled}" ]; then
        echo "GPG signing not enabled."
        return 0
    fi

    echo "GPG signing enabled."
    ostree_lib.patch_ostree_gpg_homedir
    gpg --homedir="$(ostree_lib.get_ostree_gpg_homedir)" \
        --batch --yes \
        --import "${MATRIXOS_OSTREE_GPG_KEY_PATH}"
}

release_lib.add_extra_dotdot_to_usr_etc_portage() {
    local imagedir="${1}"
    _check_imagedir "${imagedir}"

    # Fix /usr/etc/portage if symlink is broken after the move.
    echo "Fixing /usr/etc/portage symlink after move of /etc to /usr/etc ..."
    local etcportagedir="${imagedir}/usr/etc/portage"
    local cur_portage_symlink
    cur_portage_symlink=$(readlink "${etcportagedir}")
    rm "${etcportagedir}"
    ln -sf "../${cur_portage_symlink}" "${etcportagedir}"
    echo "New /usr/etc/portage symlink status:"
    ls -la "${etcportagedir}"

    if [[ -L "${etcportagedir}" && ! -e "${etcportagedir}" ]]; then
        echo "Symlink is broken: ${etcportagedir}" >&2
        return 1
    fi
}

release_lib.remove_extra_dotdot_from_usr_etc_portage() {
    local imagedir="${1}"
    _check_imagedir "${imagedir}"

    echo "Removing extra ../ from /usr/etc/portage so that it works after client deployment."
    local etcportagedir="${imagedir}/usr/etc/portage"
    local cur_portage_symlink
    cur_portage_symlink=$(readlink "${etcportagedir}")
    rm "${etcportagedir}"
    ln -sf "${cur_portage_symlink/#..\/}" "${etcportagedir}"

    echo "New /usr/etc/portage symlink status (might be broken):"
    ls -la "${etcportagedir}"
}

release_lib.symlink_etc() {
    local imagedir="${1}"
    _check_imagedir "${imagedir}"

    # Try to prevent post_clean to recreate /etc.
    echo "Symlinking /etc to prevent emerge packages recreating it ..."
    ln -sf "usr/etc" "${imagedir}/etc"
}

release_lib.unlink_etc() {
    local imagedir="${1}"
    _check_imagedir "${imagedir}"

    echo "Removing /etc symlink before ostree commit ..."
    rm -f "${imagedir}/etc"
}

release_lib.ostree_prepare() {
    local imagedir="${1}"
    _check_imagedir "${imagedir}"
    ostree_lib.prepare_filesystem_hierarchy "${imagedir}"
}

release_lib.maybe_ostree_init() {
    local repodir="${1}"
    if [ -z "${repodir}" ]; then
        echo "release_lib.maybe_ostree_init <repodir>" >&2
        return 1
    fi
    if [ -d "${repodir}/objects" ]; then
        echo "ostree repository ${repodir} already present."
        return 0
    fi
    echo "Creating ostree repository ${repodir} ..."
    mkdir -p "${repodir}"

    ostree_lib.run --repo="${repodir}" init --mode=archive \
        $(ostree_lib.collection_id_args)

    local gpg_val="false"
    if [ -n "${MATRIXOS_OSTREE_GPG_ENABLED}" ]; then
        gpg_val="true"
    fi
    ostree_lib.run --repo="${repodir}" config set "core.gpg-verify" "${gpg_val}"
}

release_lib.release() {
    local repodir="${1}"
    if [ -z "${repodir}" ]; then
        echo "release_lib.release  missing repodir." >&2
        return 1
    fi

    local imagedir="${2}"
    _check_imagedir "${imagedir}"

    local verbose_mode="${3}"  # can be empty.
    local gpg_enabled="${4}"  # can be empty.

    local branch="${5}"
    if [ -z "${branch}" ]; then
        echo "release_lib.release missing branch." >&2
        return 1
    fi
    local parent_branch="${6}"  # if there is no parent, that is the "root" branch.
    local consume_allowed="${7}"

    if [ -e "${imagedir}/etc" ]; then
        echo "${imagedir}/etc exists. This is illegal and breaks clients. Please fix." >&2
        return 1
    fi

    local parent_args=
    if [ -n "${parent_branch}" ]; then
        local parent_rev
        parent_rev=$(ostree_lib.last_commit "${repodir}" "${parent_branch}")
        if [ -z "${parent_rev}" ]; then
            echo "Unable to run ostree rev-parse." >&2
            return 1
        fi
        echo "Setting ostree branch parent of ${branch} to be ${parent_branch} ..."
        parent_args="--parent=${parent_rev}"
    fi

    local verbose_args=
    verbose_args=$(_get_ostree_verbosity "${verbose_mode}")
    local consume_flag=
    if [ -n "${consume_allowed}" ]; then
        consume_flag="--consume"
    fi

    local metadata=
    local metadata_file="${imagedir}${MATRIXOS_SEEDER_BUILD_METADATA_FILE}"
    if [ -f "${metadata_file}" ]; then
        echo "Reading metadata file ${metadata_file} for release commit subject ..."
        metadata=$(cat "${metadata_file}")
    fi

    local subject=
    subject="Automated release of ${MATRIXOS_OSNAME} for ${branch} at $(date +%Y-%M-%d)"
    local commit_body_file=
    commit_body_file=$(fs_lib.create_temp_file "/tmp" "matrixos.release_lib.release")
    cat <<EOFZ > "${commit_body_file}"
matrixOS ${branch} (parent: ${parent_branch:-none}) at $(date +%Y-%m-%d)

Build metadata:
${metadata:-not available}
EOFZ

    echo "Normalizing files at ${imagedir} before ostree commit to have same timestamp ..."
    find "${imagedir}" -depth -exec touch -h -d @1 '{}' +

    # There is no remote here with branch on purpose. Because we do not want to commit
    # by default with a prefix that instead of being part of the "remote" part, it becomes
    # part of the branch name.
    local ostree_commit_args=(
        ${verbose_args} ${consume_flag}
        --repo="${repodir}"
        ${parent_args}
        --branch="${branch}"
        $(ostree_lib.ostree_gpg_args "${gpg_enabled}")
        --subject="${subject}"
        --body-file="${commit_body_file}"
        "${imagedir}"
    )

    echo "Committing ostree rootfs from ${imagedir} to branch: ${branch}"
    echo "Running: ostree commit ${ostree_commit_args[@]}"
    ostree_lib.run commit "${ostree_commit_args[@]}"
    ostree_lib.prune "${repodir}" "${branch}"
    if [ -n "${MATRIXOS_RELEASE_GENERATE_STATIC_DELTAS}" ]; then
        ostree_lib.generate_static_delta "${repodir}" "${branch}"
    else
        echo "Skipping static delta generation as requested by flags."
    fi
    ostree_lib.update_summary "${repodir}" "${gpg_enabled}"

    rm -f "${commit_body_file}"  # leave it there if commit fails.
}

release_lib.detect_local_releases() {
    local repodir="${1}"
    if [ -z "${repodir}" ]; then
        echo "release_lib.detect_releases: missing repodir parameter" >&2
        return 1
    fi

    local skip_releases_check_func="${2}"
    if [ -z "${skip_releases_check_func}" ]; then
        echo "release_lib.detect_releases: missing skip_releases_check_func parameter" >&2
        return 1
    fi

    local only_releases_check_func="${3}"
    if [ -z "${only_releases_check_func}" ]; then
        echo "release_lib.detect_releases: missing only_releases_check_func parameter" >&2
        return 1
    fi

    echo "Detecting local releases ..." >&2
    local refs=
    refs=( $(ostree_lib.local_refs "${repodir}") )

    for ref in "${refs[@]}"; do
        if ${skip_releases_check_func} "${ref}"; then
            echo "Skipping release: ${ref} as requested by flags." >&2
            continue
        fi
        if ! ${only_releases_check_func} "${ref}"; then
            echo "Skipping release: ${ref} not in list of releases to create." >&2
            continue
        fi

        echo "${ref}"
    done
}

release_lib.detect_remote_releases() {
    local remote="${1}"
    if [ -z "${remote}" ]; then
        echo "release_lib.detect_releases: missing remote parameter" >&2
        return 1
    fi

    local repodir="${2}"
    if [ -z "${repodir}" ]; then
        echo "release_lib.detect_releases: missing repodir parameter" >&2
        return 1
    fi

    local skip_releases_check_func="${3}"
    if [ -z "${skip_releases_check_func}" ]; then
        echo "release_lib.detect_releases: missing skip_releases_check_func parameter" >&2
        return 1
    fi

    local only_releases_check_func="${4}"
    if [ -z "${only_releases_check_func}" ]; then
        echo "release_lib.detect_releases: missing only_releases_check_func parameter" >&2
        return 1
    fi

    echo "Detecting remote releases ..." >&2
    local refs
    refs=( $(ostree_lib.remote_refs "${remote}" "${repodir}") )

    for ref in "${refs[@]}"; do
        if ${skip_releases_check_func} "${ref}"; then
            echo "Skipping release: ${ref} as requested by flags." >&2
            continue
        fi
        if ! ${only_releases_check_func} "${ref}"; then
            echo "Skipping release: ${ref} not in list of releases to create." >&2
            continue
        fi

        echo "${ref}"
    done
}

release_lib.validate_release_stage() {
    local val="${1}"
    case "${val}" in
        dev|prod)
            return 0
        ;;

        *)
            echo "Unknown release stage: ${val}" >&2
            return 1
        ;;
    esac
}

release_lib.release_lock_dir() {
    local lock_dir="${MATRIXOS_RELEASE_LOCK_DIR}"
    mkdir -p "${lock_dir}"
    echo "${lock_dir}"
}

release_lib.release_lock_path() {
    local seeder_name="${1}"
    if [ -z "${seeder_name}" ]; then
        echo "release_lib.release_lock_path <seeder_name>" >&2
        return 1
    fi

    local lock_dir=
    lock_dir="$(release_lib.release_lock_dir)"
    local lock_file="${lock_dir}/${seeder_name}.lock"
    echo "${lock_file}"
}

release_lib.execute_with_release_lock() {
    local func="${1}"
    local release_name="${2}"
    shift 2

    local lock_path
    lock_path=$(release_lib.release_lock_path "${release_name}")
    echo "Acquiring release ${release_name} lock via ${lock_path} ..."

    local lock_fd=
    # Do not use a subshell otherwise the global cleanup variables used in trap will not
    # be filled properly. Like: ${MOUNTS} in seeder.
    exec {lock_fd}>"${lock_path}"

    if ! flock -x --timeout "${MATRIXOS_RELEASE_LOCK_WAIT_SECS}" "${lock_fd}"; then
        echo "Timed out waiting for release lock ${lock_path}" >&2
        exec {lock_fd}>&-
        return 1
    fi

    echo "Lock for releaser ${release_name}, ${lock_path} acquired!"

    # We do NOT use a trap. We rely on standard flow control.
    # If "${func}" crashes (set -e), the script dies and OS closes the FD.
    # If "${func}" returns (success or fail), we capture it.
    "${func}" "${@}"
    local ret=${?}

    # Release the lock.
    exec {lock_fd}>&-
    return ${ret}
}
