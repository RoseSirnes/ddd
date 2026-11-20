#!/bin/sh
set -eu
# The Railway-mounted volume may initially be root-owned; use the ddd user thereafter.
chown -R ddd:ddd /data
exec su-exec ddd /usr/local/bin/ddd -public-server -data-dir /data
