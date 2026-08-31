#!/bin/sh
stty raw -echo
printf '%b' '\033[?1049h\033[?25l\033[?1002h\033[?1006h\033[?2004h'
printf 'application returned error: synthetic failure\n' >&2
stty sane
printf '%b' '\033[?1049l\033[?25h\033[?1002l\033[?1003l\033[?1006l\033[?2004l'
exit 1
