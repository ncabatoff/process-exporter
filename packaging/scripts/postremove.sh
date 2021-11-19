#!/bin/sh
# postremove script for prometheus-process-exporter
# Script executed after the package is removed.

if [ -d /run/systemd/system ]; then
    systemctl daemon-reload
fi