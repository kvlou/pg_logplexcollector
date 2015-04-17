#!/bin/sh

PROG=pg_logplexcollector
USER=pg_logplexcollector
RUN_DIR=/var/run/$PROG

stop $PROG; sleep 1
rm -f /etc/init/$PROG.conf
rm -rf $RUN_DIR

# force shutdown, in case upstart took a bit.
pkill -u $USER -KILL $PROG

if [ ! $(id -u $USER >/dev/null 2>&1) ]; then
  userdel pg_logplexcollector
fi

