#!/bin/bash
set -eu

ini_lib.get() {
    local file="${1}"
    local section="${2}"
    local key="$3"

    awk -v target_section="${section}" -v target_key="${key}" '
    BEGIN { FS="="; }
    # 1. Trim whitespace from a string
    function trim(s) {
        gsub(/^[ \t]+|[ \t]+$/, "", s);
        return s;
    }
    # 2. Track the current section
    /^\[.*\]/ {
        current_section = trim($0);
        # Remove brackets [ ]
        gsub(/^\[|\]$/, "", current_section); 
        next;
    }
    # 3. Process key=value pairs
    {
        if (current_section == target_section) {
            # Split line by first "="
            if ($0 ~ /=/) {
                # Clean up the key part
                k = trim($1);
                
                # Check if it matches our target
                if (k == target_key) {
                    # Extract the value (everything after the first =)
                    # We use sub to replace the key= part with nothing
                    sub(/^[^=]+=/, "");
                    val = trim($0);
                    print val;
                    exit;
                }
            }
        }
    }
    ' "${file}"
}

ini_lib.get_sections() {
    local file="${1}"

    awk '
    # 1. Match lines that contain a section start
    /^[ \t]*\[.*\]/ {
        s = $0;
        
        # 2. Trim inline comments (anything after the last closing bracket)
        # Finds the last ] and removes everything after it
        sub(/\][^\]]*$/, "]", s);
        
        # 3. Trim whitespace and the square brackets themselves
        # Remove everything up to and including the first [
        sub(/^[ \t]*\[/, "", s);
        # Remove the last ]
        sub(/\]$/, "", s);
        
        # 4. Print the clean section name
        if (length(s) > 0) print s;
    }
    ' "${file}"
}