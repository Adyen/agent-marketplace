#!/bin/sh
AM_TUI_QA_EDITOR_SPAWN_CHILD=1 "$EDITOR"
printf 'editor parent exited\n'
exit 0
