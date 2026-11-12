#!/usr/bin/env bash
# SSHT installer for Linux and macOS. Re-run to update. Does not touch ~/.ssh/config.
#   curl -fsSL https://raw.githubusercontent.com/chuikoff/ssht/main/install/unix.sh | bash
#   SSHT_VERSION=v1.3.9 bash install/unix.sh
#   FORCE_INSTALL=true INSTALL_DIR=$HOME/.local/bin bash install/unix.sh

set -euo pipefail

REPO="chuikoff/ssht"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
SSHT_VERSION="${SSHT_VERSION:-latest}"
FORCE_INSTALL="${FORCE_INSTALL:-false}"

info() { printf '\033[0;32m%s\n\033[0m' "$*"; }
warn() { printf '\033[1;33m%s\n\033[0m' "$*"; }
die() { printf '\033[0;31m%s\n\033[0m' "$*" >&2; exit 1; }

case "$(uname -s)" in
    Linux) OS="linux" ;;
    Darwin) OS="darwin" ;;
    *) die "Этот скрипт для Linux и macOS. На Windows: irm https://raw.githubusercontent.com/chuikoff/ssht/main/install/windows.ps1 | iex" ;;
esac

case "$(uname -m)" in
    x86_64|amd64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *) die "Архитектура $(uname -m) не поддерживается. Есть только amd64 и arm64." ;;
esac

info "Установка SSHT"
info "Платформа: $OS ($ARCH)"

if command -v ssht >/dev/null 2>&1 && [ "$FORCE_INSTALL" != "true" ]; then
    current="$(ssht --version 2>/dev/null || true)"
    warn "SSHT уже есть в PATH: $current"
    if [ -t 0 ]; then
        printf 'Продолжить установку? (y/N): '
        read -r answer
        case "$answer" in
            y|Y|yes|д|Д|да) ;;
            *) info "Установка отменена."; exit 0 ;;
        esac
    else
        warn "Запуск через pipe, установка продолжится. Отказ: не запускайте скрипт, если обновление не нужно."
    fi
fi

if [ "$SSHT_VERSION" = "latest" ]; then
    info "Запрос последнего релиза..."
    tag="$(curl -fsSL -H "User-Agent: ssht-installer" -H "Accept: application/vnd.github+json" \
        "https://api.github.com/repos/$REPO/releases/latest" \
        | sed -n 's/.*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)"
    [ -n "$tag" ] || die "Не удалось получить номер релиза с GitHub."
else
    tag="$SSHT_VERSION"
    case "$tag" in v*) ;; *) tag="v$tag" ;; esac
fi
info "Версия: $tag"

file="ssht_${tag}_${OS}_${ARCH}.tar.gz"
base="https://github.com/$REPO/releases/download/$tag"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

info "Скачивание $file..."
curl -fL --retry 3 -H "User-Agent: ssht-installer" -o "$work/$file" "$base/$file" \
    || die "Не удалось скачать $file. Проверьте, что релиз $tag опубликован."
curl -fsSL -H "User-Agent: ssht-installer" -o "$work/checksums.txt" "$base/checksums.txt"
expected="$(awk -v name="$file" '$2 == name || $2 == ("*" name) { print $1; exit }' "$work/checksums.txt")"
[ -n "$expected" ] || die "В checksums.txt нет записи для $file"
if command -v sha256sum >/dev/null 2>&1; then
    actual="$(sha256sum "$work/$file" | awk '{print $1}')"
elif command -v shasum >/dev/null 2>&1; then
    actual="$(shasum -a 256 "$work/$file" | awk '{print $1}')"
else
    die "Нужен sha256sum или shasum, чтобы проверить архив."
fi
[ "$actual" = "$expected" ] || die "SHA256 не совпала для $file"

tar -xzf "$work/$file" -C "$work"
bin=""
while IFS= read -r candidate; do
    bin="$candidate"
    break
done < <(find "$work" -type f -name ssht)
[ -n "$bin" ] || die "В архиве нет файла ssht"
chmod +x "$bin"

target="$INSTALL_DIR/ssht"
run() {
    if [ -w "$INSTALL_DIR" ] || [ "$(id -u)" -eq 0 ]; then
        "$@"
    elif command -v sudo >/dev/null 2>&1; then
        sudo "$@"
    else
        return 1
    fi
}

if [ ! -d "$INSTALL_DIR" ]; then
    run mkdir -p "$INSTALL_DIR" || die "Не удалось создать $INSTALL_DIR"
fi

if [ -f "$target" ]; then
    run rm -f "$target" || die "Не удалось заменить $target. Закройте запущенный ssht."
fi
run cp "$bin" "$target" || die "Не удалось скопировать ssht в $INSTALL_DIR"
run chmod 755 "$target"

info ""
info "SSHT установлен: $target"
case ":$PATH:" in
    *":$INSTALL_DIR:"*) ;;
    *) warn "$INSTALL_DIR нет в PATH. Добавьте: export PATH=\"$INSTALL_DIR:\$PATH\"" ;;
esac
"$target" --version
