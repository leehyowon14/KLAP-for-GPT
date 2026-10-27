#!/bin/bash
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
[[ "$(uname -s)" == Darwin ]] || { echo 'macOS is required.' >&2; exit 1; }
kind="${1:-mac}"
[[ "$kind" == mac || "$kind" == helper ]] || { echo 'usage: build-mac.sh [mac|helper]' >&2; exit 1; }
for command in go swift codesign python3; do command -v "$command" >/dev/null || { echo "Install $command first." >&2; exit 1; }; done
[[ -f "$root/core/go.mod" ]] || { echo 'Run git submodule update --init --recursive.' >&2; exit 1; }
app="$root/.build/KLAP for GPT.app"
if [[ "$kind" == helper ]]; then app="$root/.build/KLAP Helper.app"; fi
mkdir -p "$app/Contents/MacOS/bridges/macos" "$app/Contents/Resources"
if [[ "$kind" == mac ]]; then
  command -v npm >/dev/null || { echo 'Install Node.js 24+ first.' >&2; exit 1; }
  (cd "$root/web" && npm ci --no-audit --no-fund && npm run build && npm test)
  cp "$root/web/dist/index.html" "$app/Contents/Resources/index.html"
  (cd "$root" && go build -trimpath -o "$app/Contents/MacOS/klap-gpt" ./cmd/klap-gpt)
  executable=klap-gpt
else
  (cd "$root" && go build -trimpath -o "$app/Contents/MacOS/klap-helper" ./cmd/klap-helper)
  executable=klap-helper
fi
for product in ReminderBridge CalendarBridge CategoryBridge; do
  swift build --package-path "$root/core/bridges/macos" -c release --product "$product"
done
swift_bin="$(swift build --package-path "$root/core/bridges/macos" -c release --show-bin-path)"
for product in ReminderBridge CalendarBridge CategoryBridge; do
  cp "$swift_bin/$product" "$app/Contents/MacOS/bridges/macos/$product"
  codesign --force --sign - --identifier "dev.leehyowon14.klap.gpt.$product" "$app/Contents/MacOS/bridges/macos/$product"
done
python3 - "$app" "$executable" "$kind" <<'PY'
import plistlib,sys,pathlib
app,exe,kind=sys.argv[1:]
info={'CFBundleIdentifier':'dev.leehyowon14.klap.gpt.'+kind,'CFBundleName':'KLAP for GPT' if kind=='mac' else 'KLAP Helper','CFBundleExecutable':exe,'CFBundlePackageType':'APPL','CFBundleVersion':'1','CFBundleShortVersionString':'0.1.0','LSMinimumSystemVersion':'12.0','LSUIElement':True,'NSRemindersFullAccessUsageDescription':'과제와 온라인 강의를 미리알림에 동기화합니다.','NSCalendarsFullAccessUsageDescription':'시간표와 학사일정을 캘린더에 동기화합니다.','NSRemindersUsageDescription':'학습 일정 동기화','NSCalendarsUsageDescription':'학습 일정 동기화'}
with open(pathlib.Path(app)/'Contents/Info.plist','wb') as f: plistlib.dump(info,f)
PY
codesign --force --deep --sign - "$app"
codesign --verify --deep --strict "$app"
printf '%s\n' "$app"
