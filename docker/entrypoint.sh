#!/bin/sh
set -eu
umask 077
bash /app/provision.sh
exec java --enable-native-access=ALL-UNNAMED -jar /app/backend.jar --spring.profiles.active=combined,local "$@"
