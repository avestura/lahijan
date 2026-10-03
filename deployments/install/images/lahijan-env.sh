#!/bin/sh
# Entrypoint shim shared by the Lahijan install images.
#
# On first start the `secrets` service writes generated passwords and keys to
# /secrets/secrets.env on a shared volume. Each line there only fills a
# variable that is still empty, so a value you put in .env always wins.
# This shim loads that file and then runs the image's normal entrypoint.
if [ -r /secrets/secrets.env ]; then
	set -a
	# shellcheck disable=SC1091
	. /secrets/secrets.env
	set +a
fi
exec "$@"
