#!/bin/sh
set -e

# Convert USER and PASS to authInfo if provided
if [ -n "$USER" ] && [ -n "$PASS" ]; then
    export authInfo="$USER:$PASS"
fi

# If PORT is set, pass it as -p flag (optional, binary reads PORT env var)
# The binary reads the environment variable "port", so we don't need to pass as flag.
# However, we can still pass it as flag for clarity.
# if [ -n "$PORT" ]; then
#     set -- "-p" "$PORT" "$@"
# fi

# Run the binary with any passed arguments
exec ./webssh "$@"