#!/bin/sh 

set -ue

USER=pg_logplexcollector
SERVEDB_DIR=/var/lib/pg_logplexcollector
RUN_DIR=/var/run/pg_logplexcollector

mkdir -p $SERVEDB_DIR $RUN_DIR

# add default user if it doesn't exist
if [ $(id -u $USER >/dev/null 2>&1) ]; then
  adduser $USER --disabled-password --gecos ""  --no-create-home --home $SERVEDB_DIR
fi

# reset permissions
chown -R $USER $SERVEDB_DIR $RUN_DIR
chmod 755 $SERVEDB_DIR $RUN_DIR


