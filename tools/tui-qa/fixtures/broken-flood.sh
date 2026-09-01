#!/bin/sh
dd if=/dev/zero bs=1048576 count=11 2>/dev/null | tr '\000' x
sleep 30
