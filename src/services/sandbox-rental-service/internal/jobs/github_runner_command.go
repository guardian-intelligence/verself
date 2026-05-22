package jobs

func githubRunnerCommand() string {
	return `set -eu
phase() { printf '::verself-phase name=%s ts=%s\n' "$1" "$(date -u +%Y-%m-%dT%H:%M:%S.%NZ)"; }
phase runner.bootstrap.start
jit_file="$(mktemp)"
header_file="$(mktemp)"
cleanup() {
  rm -f "$jit_file" "$header_file"
  if [ -n "${runtime_dir:-}" ]; then
    rm -rf "$runtime_dir"
  fi
}
trap cleanup EXIT
printf 'header = "X-Verself-Runner-Bootstrap: %s"\n' "${VERSELF_RUNNER_BOOTSTRAP_TOKEN:?}" > "$header_file"
if [ -n "${VERSELF_TRACEPARENT:-}" ]; then
  printf 'header = "traceparent: %s"\n' "$VERSELF_TRACEPARENT" >> "$header_file"
fi
curl -fsS --retry 3 --retry-delay 1 --config "$header_file" "${VERSELF_HOST_SERVICE_HTTP_ORIGIN:?}${VERSELF_RUNNER_BOOTSTRAP_PATH:?}" -o "$jit_file"
phase runner.bootstrap.jit_fetched
unset VERSELF_TRACEPARENT
unset VERSELF_RUNNER_BOOTSTRAP_TOKEN
export PATH="/opt/actions-runner/externals/node20/bin:$PATH"
runtime_dir="` + githubRunnerRuntimeDir + `"
durable_work_dir="` + githubRunnerDurableWorkDir + `"
# GitHub only accepts work_folder relative to the runner install dir.
tool_cache="` + runnerToolCacheDir + `"
[ -d "$durable_work_dir" ] || { echo "durable GitHub work dir is not mounted: $durable_work_dir" >&2; exit 1; }
rm -rf "$runtime_dir"
mkdir -p "$runtime_dir" "$tool_cache"
cp -a /opt/actions-runner/bin "$runtime_dir/bin"
cp -a /opt/actions-runner/run.sh /opt/actions-runner/run-helper.sh.template "$runtime_dir/"
for entry in /opt/actions-runner/* /opt/actions-runner/.[!.]* /opt/actions-runner/..?*; do
  [ -e "$entry" ] || continue
  base="$(basename "$entry")"
  case "$base" in
    _diag|_temp|_work|bin|lost+found|run.sh|run-helper.sh|run-helper.sh.template)
      continue
      ;;
  esac
  ln -s "$entry" "$runtime_dir/$base"
done
ln -s "$durable_work_dir" "$runtime_dir/_work"
export RUNNER_TOOL_CACHE="$tool_cache"
export AGENT_TOOLSDIRECTORY="$tool_cache"
phase runner.bootstrap.runtime_ready
cd "$runtime_dir"
mkdir -p _diag _temp
phase github.runner.process_start
./run.sh --jitconfig "$(cat "$jit_file")"`
}
