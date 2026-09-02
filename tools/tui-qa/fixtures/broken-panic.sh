#!/bin/sh
stty -echo -icanon
printf '%b' '\033[?1049h\033[?25l\033[?2004h'
printf 'panic: synthetic fixture failure\n' >&2
exit 2
