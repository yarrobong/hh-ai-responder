#!/usr/bin/env bash

set -euo pipefail

APP="hh-ai-responder"

# Переходим в директорию, где находится скрипт
cd "$(dirname "$0")" || {
  echo "ERROR: cannot change directory" >&2
  exit 1
}

# The application loads .env itself. Do not shell-source it here: dotenv
# values such as comma-separated keyword lists are not necessarily shell code.

# Проверяем наличие бинарника
if [ ! -f "$APP" ]; then
  echo "ERROR: $APP not found" >&2
  exit 2
fi

# Проверяем исполняемость
if [ ! -x "$APP" ]; then
  echo "ERROR: $APP is not executable" >&2
  exit 3
fi

# Запускаем приложение с передачей аргументов
exec "./$APP" "$@"
