#!/bin/bash

# Read the file
FILE="paddle_checkout_test.go"

# Add input.UserID = "user_123" after each input := line (except in Unauthenticated tests)
# This is a simple approach that works for most cases
# Portable in-place edit (#1598): the BSD form -- the -i flag with an empty '' argument --
# errors under GNU sed, and this repo now rejects both single-platform forms tree-wide
# (scripts/check-portable-sed.sh). The temp-file form behaves identically on macOS and Linux.
sed '/Unauthenticated/,/^}/!s/\(input := &.*Input{}\)/\1\
	input.UserID = "user_123"/' "$FILE" > "$FILE.portable.tmp" \
    && cat "$FILE.portable.tmp" > "$FILE" \
    && rm -f "$FILE.portable.tmp"

echo "Updated $FILE"
