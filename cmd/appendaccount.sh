#!/bin/sh
# This script appends a new random account to the config.toml next to it.
# If a parameter is passed in, a comment will be added with that parameter

set -e

# Feel free to make this more secure or whatever, though 8 bytes of
# randomness is pretty ok for casual use
account=$(openssl rand -hex 16)

line="[Accounts.$account]"

if [ "$#" -ge "1" ]; then
	line="$line  # $1"
fi

echo "Adding line '$line' to config.toml"
printf "\n$line\n" >>config.toml
echo "Done"
