#!/bin/sh
# postinstall script for prometheus-process-exporter
# Script executed after the package is removed.

if [ -d /run/systemd/system ]; then
    systemctl daemon-reload
    systemctl enable process-exporter.service
    systemctl restart process-exporter.service
fi