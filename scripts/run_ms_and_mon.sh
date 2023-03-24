#!/bin/sh

# start metric mon 
/usr/bin/python /app/mongoshake_mon.py &

# start mongoshake
/app/collector --conf=/app/conf/collector.conf --verbose=2

