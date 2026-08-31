#!/bin/sh
printf '%b' '\033[?1049h\033[?25l\033[?1002h\033[?1006h\033[?2004h'
printf 'mode leak fixture\n'
exit 0
