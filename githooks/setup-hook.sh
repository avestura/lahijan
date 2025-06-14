#!/usr/bin/sh

cp ./commit-msg .git/hooks/ && chmod +x .git/hooks/commit-msg

echo "✅ Git hook installed successfully!"
