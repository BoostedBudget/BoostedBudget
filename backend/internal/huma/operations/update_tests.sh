#!/bin/bash

# Read the file
FILE="paddle_checkout_test.go"

# Add input.UserID = "user_123" after each input := line (except in Unauthenticated tests)
# This is a simple approach that works for most cases
sed -i '' '/Unauthenticated/,/^}/!s/\(input := &.*Input{}\)/\1\
	input.UserID = "user_123"/' "$FILE"

echo "Updated $FILE"
