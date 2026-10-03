#!/bin/sh
set -e

mkdir -p /data/files

# If running as root, give /data to PUID:PGID and re-exec as them (setpriv is util-linux's gosu,
# already in the image). The chown walks every file, which takes a while on a big library, so it
# only runs when the owner isn't right yet.
if [ "$(id -u)" = "0" ]; then
    TARGET_UID="${PUID:-1000}"
    TARGET_GID="${PGID:-1000}"
    for dir in /data /data/files; do
        if [ "$(stat -c %u:%g "$dir")" != "${TARGET_UID}:${TARGET_GID}" ]; then
            chown -R "${TARGET_UID}:${TARGET_GID}" /data
            break
        fi
    done
    exec setpriv --reuid="${TARGET_UID}" --regid="${TARGET_GID}" --clear-groups -- "$@"
fi

exec "$@"
