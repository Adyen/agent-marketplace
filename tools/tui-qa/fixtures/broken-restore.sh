#!/bin/sh
printf '%b' '\033[?1049h\033[?25l'
printf 'in alternate screen, never restoring\n'
exit 0
