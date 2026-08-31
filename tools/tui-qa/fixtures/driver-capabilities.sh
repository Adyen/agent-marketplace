#!/bin/sh
trap 'printf "\nTARGET_SIGNAL=TERM\n"; exit 0' TERM
stty raw -echo
printf '\033[?1000h\033[?1006h'
printf 'capability-ready size='
stty size
while IFS= read -r line; do
  printf 'input-hex='
  printf '%s' "$line" | od -An -tx1 | tr -d '\n'
  printf '\nobserved-size='
  stty size
done
