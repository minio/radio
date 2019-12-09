#!/bin/sh

# If command starts with an option, prepend radio
if [ "${1}" != "radio" ]; then
    if [ -n "${1}" ]; then
        set -- radio "$@"
    fi
fi

"$@"
