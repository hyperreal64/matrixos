# matrixOS Development Scripts

This directory contains scripts used for the development, testing, and automation of matrixOS. These scripts are primarily intended for developers and are not part of the final OS image.

## Scripts Overview

### Testing Scripts

A series of test scripts are provided to facilitate the testing of various matrixOS components. These scripts set up the necessary environment variables and execute the main scripts for the components under test.

*   `_test_client_imager.sh`: A wrapper script for testing the main imager script (`image/image_main.sh`).
*   `_test_imager.sh`: Similar to `_test_client_imager.sh`, but with more environment variables set for testing with specific paths.
*   `_test_release.sh`: A test runner for the release process, executing `release/release.seeds`.
*   `_test_seeder.sh`: A test script for the seeder, executing `build/seeder`.

### Live USB Build Scripts

These scripts are used to build live USB images of matrixOS.

*   `build_liveusb.sh`: Builds a standard live USB image without encryption.
*   `build_encrypted_liveusb.sh`: Builds a live USB image with encryption enabled.

### Chroot Environment

*   `chroot.sh`: This script is used to set up a chroot environment for manual building, development, and testing of matrixOS components. It performs a series of bind mounts to create an isolated environment.

### Automated Build Script

*   `weekly_builder.sh`: This script is designed to be run on a weekly basis to automate the build and release process of matrixOS. It utilizes a chroot environment to build the seeds and then releases them. This script is crucial for the continuous integration and delivery of the OS.
