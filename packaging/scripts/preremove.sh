#!/bin/sh
# preremove script for prometheus-process-exporter
# Script executed after the package is removed.

if [ -d /run/systemd/system ]; then
    systemctl stop process-exporter.service
    systemctl disable process-exporter.service
fi