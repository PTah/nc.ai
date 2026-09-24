#!/usr/bin/env bash
# Sidecar <zip>.sha256 для macOS-релизов NotCursor.
#
# Апдейтер сравнивает хеш ЗАПУСКАЕМОГО файла (LocalExeHash) со значением из
# sidecar, а не хеш архива. Если положить в sidecar sha256 самого zip, приложение
# вечно показывает «версия та же, но на GitHub другая сборка» — этот инцидент
# уже случался в 0.7.4 и в 0.7.5.
#
#   scripts/macos-zip-sidecar.sh <zip>...              # записать <zip>.sha256
#   scripts/macos-zip-sidecar.sh --check <zip> <exe>   # сверить sidecar с файлом
#
# Формат sidecar: <64-hex>  <имя zip> (LF, без BOM) — так его читает ParseSha256.
set -euo pipefail

exe_in_zip() {
  unzip -p "$1" NotCursor.app/Contents/MacOS/NotCursor | shasum -a 256 | awk '{print $1}'
}

hash_file() {
  shasum -a 256 "$1" | awk '{print $1}'
}

if [[ "${1:-}" == "--check" ]]; then
  zip="${2:?нужен путь к zip}"
  local_exe="${3:?нужен путь к локальному исполняемому файлу}"
  want="$(exe_in_zip "${zip}")"
  got="$(hash_file "${local_exe}")"
  echo "zip exe:   ${want}"
  echo "local exe: ${got}"
  if [[ "${want}" != "${got}" ]]; then
    echo "MISMATCH: sidecar не совпадёт с локальным бинарником" >&2
    exit 1
  fi
  echo "OK: хеши совпадают"
  exit 0
fi

if [[ "$#" -eq 0 ]]; then
  echo "usage: $0 <zip>... | --check <zip> <exe>" >&2
  exit 2
fi

for zip in "$@"; do
  [[ -f "${zip}" ]] || { echo "нет файла: ${zip}" >&2; exit 1; }
  hex="$(exe_in_zip "${zip}")"
  printf '%s  %s\n' "${hex}" "$(basename "${zip}")" > "${zip}.sha256"
  echo "${zip}.sha256 -> ${hex}"
done
