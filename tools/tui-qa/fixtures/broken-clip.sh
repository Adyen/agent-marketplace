#!/bin/sh
printf '%b' '\033[?7l'
printf 'LEFT_CHROME_ANCHOR'
printf '%080s' ''
printf 'RIGHT_EDGE_ANCHOR'
printf '%b' '\033[?7h'
sleep 30
