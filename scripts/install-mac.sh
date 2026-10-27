#!/bin/bash
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
kind="${1:-}"; origin="${2:-}"
[[ "$kind" == mac || "$kind" == helper ]] && [[ -n "$origin" ]] || { echo 'usage: install-mac.sh mac|helper https://your-klap.example' >&2; exit 1; }
python3 - "$origin" <<'PY'
import sys,urllib.parse
u=urllib.parse.urlsplit(sys.argv[1])
assert u.scheme=='https' and u.hostname and not u.path.rstrip('/') and not u.query and not u.fragment and not u.username and not u.password, 'A public HTTPS origin is required.'
PY
origin="${origin%/}"
bash "$root/scripts/build-mac.sh" "$kind"
name='KLAP for GPT'; executable=klap-gpt
if [[ "$kind" == helper ]]; then name='KLAP Helper'; executable=klap-helper; fi
install_dir="${KLAP_INSTALL_DIR:-$HOME/Applications}"
destination="$install_dir/$name.app"
mkdir -p "$install_dir" "$HOME/Library/LaunchAgents"
label="dev.leehyowon14.klap.gpt.$kind"
plist="$HOME/Library/LaunchAgents/$label.plist"
launchctl bootout "gui/$(id -u)/$label" 2>/dev/null || true
if [[ -e "$destination" ]]; then mv "$destination" "$destination.previous.$(date +%Y%m%d%H%M%S)"; fi
ditto "$root/.build/$name.app" "$destination"
data_dir="$HOME/Library/Application Support/KLAP-for-GPT"
mkdir -p "$data_dir"; chmod 700 "$data_dir"
if [[ "$kind" == helper ]]; then
  "$destination/Contents/MacOS/$executable" --server "$origin" --pair-only
fi
python3 - "$destination" "$kind" "$origin" "$data_dir" "$plist" "$label" <<'PY'
import sys,plistlib,pathlib
app,kind,origin,data,target,label=sys.argv[1:]
binary=app+'/Contents/MacOS/'+('klap-gpt' if kind=='mac' else 'klap-helper')
args=[binary,'--public-url',origin,'--mode','mac','--data-dir',data,'--ui',app+'/Contents/Resources/index.html'] if kind=='mac' else [binary,'--server',origin]
value={'Label':label,'ProgramArguments':args,'RunAtLoad':True,'KeepAlive':True,'ProcessType':'Interactive','ThrottleInterval':15,'StandardOutPath':data+'/'+kind+'.log','StandardErrorPath':data+'/'+kind+'.log'}
with open(target,'wb') as f: plistlib.dump(value,f)
pathlib.Path(target).chmod(0o600)
PY
launchctl bootstrap "gui/$(id -u)" "$plist"
printf 'Installed %s\nSettings: %s\n' "$destination" "$origin"
if [[ "$kind" == mac ]]; then printf 'First setup token: %s/bootstrap.token\n' "$data_dir"; fi
