#!/bin/sh
stty -echo -icanon
printf '%b' '\033[?1049h\033[?25l\033[?2004h'
printf 'sigkill fixture ready\n'
while :; do sleep 30; done
