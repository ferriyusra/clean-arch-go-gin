#!/usr/bin/env bash
# Starts the Jenkins image build for one commit and waits for its result, so the
# GitHub check goes red when Jenkins does. Called by .github/workflows/jenkins.yml;
# it also runs from a laptop, which is the quickest way to test the credentials:
#
#   JENKINS_URL=https://jenkins.example.com JENKINS_USER=gha-trigger \
#   JENKINS_API_TOKEN=... JENKINS_JOB=clean-arch-go-gin/image \
#   GIT_SHA=$(git rev-parse origin/main) GIT_REF=refs/heads/main \
#   bash .github/scripts/trigger-jenkins.sh
#
# Needs bash, curl and jq. See CI_CD.md for the Jenkins side.
set -euo pipefail

: "${JENKINS_URL:?JENKINS_URL is not set}"
: "${JENKINS_USER:?JENKINS_USER is not set}"
: "${JENKINS_API_TOKEN:?JENKINS_API_TOKEN is not set}"
: "${JENKINS_JOB:?JENKINS_JOB is not set}"
: "${GIT_SHA:?GIT_SHA is not set}"
: "${GIT_REF:?GIT_REF is not set}"
SOURCE_RUN_URL="${SOURCE_RUN_URL:-}"
TIMEOUT_SECONDS="${TIMEOUT_SECONDS:-2400}"
POLL_SECONDS="${POLL_SECONDS:-10}"

command -v jq >/dev/null || { echo "::error::jq is required"; exit 1; }

# Jenkins validates these again. Checking here turns a typo into a clear error
# before anything is queued.
[[ "$GIT_SHA" =~ ^[0-9a-f]{40}$ ]] || { echo "::error::GIT_SHA must be a full 40-character commit SHA: $GIT_SHA"; exit 1; }
[[ "$JENKINS_JOB" =~ ^[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*$ ]] || { echo "::error::JENKINS_JOB must be a job name or folder/job path: $JENKINS_JOB"; exit 1; }

base="${JENKINS_URL%/}"
# "folder/job" becomes "job/folder/job/job", the shape of Jenkins job URLs.
job_url="$base/job/${JENKINS_JOB//\//\/job\/}"

# Credentials go to curl on stdin as a config file, never as arguments: a
# command line is readable by every process on the machine.
curl_config() {
  local user=${JENKINS_USER//\\/\\\\} token=${JENKINS_API_TOKEN//\\/\\\\}
  printf 'user = "%s:%s"\n' "${user//\"/\\\"}" "${token//\"/\\\"}"
}
jenkins() {
  curl --config - --silent --show-error --connect-timeout 10 --max-time 30 "$@" < <(curl_config)
}

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# ---- 1. queue the build ----------------------------------------------------
# An API token is exempt from Jenkins' CSRF crumb, so this is one request.
# delay=0sec skips the quiet period; nothing here benefits from waiting.
status=$(jenkins --request POST \
  --output "$work/body" --dump-header "$work/headers" --write-out '%{http_code}' \
  --data-urlencode "GIT_SHA=$GIT_SHA" \
  --data-urlencode "GIT_REF=$GIT_REF" \
  --data-urlencode "SOURCE_RUN_URL=$SOURCE_RUN_URL" \
  --data-urlencode "delay=0sec" \
  "$job_url/buildWithParameters") || { echo "::error::Could not reach Jenkins at $base"; exit 1; }

case "$status" in
  201|303) ;; # 303: an identical request is already queued; follow that one.
  400)
    echo "::error::Jenkins answered 400. If the body says the job 'is not parameterized', run the job once with 'Build Now' so the Jenkinsfile registers its parameters (CI_CD.md, 'First run')."
    cat "$work/body"; echo; exit 1 ;;
  401|403)
    echo "::error::Jenkins refused the credentials ($status). Check JENKINS_USER / JENKINS_API_TOKEN and that the user has Overall/Read, Job/Read and Job/Build."
    exit 1 ;;
  404)
    echo "::error::Jenkins has no job at $job_url (404). Check JENKINS_JOB."
    exit 1 ;;
  *)
    echo "::error::Unexpected answer $status from $job_url/buildWithParameters"
    cat "$work/body"; echo; exit 1 ;;
esac

# The Location header is built from Jenkins' own configured root URL, which is
# often an internal hostname. Only the item id is taken from it.
queue_id=$(tr -d '\r' < "$work/headers" | sed -n 's#^[Ll]ocation:.*/queue/item/\([0-9][0-9]*\)/\{0,1\}$#\1#p' | tail -n 1)
[[ -n "$queue_id" ]] || { echo "::error::Jenkins did not return a queue item"; tr -d '\r' < "$work/headers"; exit 1; }
echo "Queued as Jenkins queue item $queue_id"

deadline=$(( $(date +%s) + TIMEOUT_SECONDS ))
past_deadline() { (( $(date +%s) >= deadline )); }

# ---- 2. wait for the queue item to become a build ---------------------------
build_number=""
while [[ -z "$build_number" ]]; do
  past_deadline && { echo "::error::Timed out waiting for Jenkins to start the build (queue item $queue_id)"; exit 1; }
  # Only answers a retry cannot change end the wait. A restart or a proxy
  # hiccup gets the same tolerance as the build poll below. `|| code=000` keeps
  # a connection failure from tripping set -e.
  code=$(jenkins --output "$work/item" --write-out '%{http_code}' \
    "$base/queue/item/$queue_id/api/json?tree=cancelled,why,executable%5Bnumber%5D") || code=000
  case "$code" in
    200) item=$(<"$work/item") ;;
    404)
      echo "::error::Jenkins no longer knows queue item $queue_id. It forgets an item 5 minutes after the item leaves the queue, and on restart; check the job's recent builds."
      exit 1 ;;
    401|403)
      echo "::error::Jenkins refused the credentials ($code) while polling queue item $queue_id. The user needs Job/Read as well as Job/Build."
      exit 1 ;;
    *)
      echo "Queue poll failed ($code); retrying"
      sleep "$POLL_SECONDS"
      continue ;;
  esac
  if [[ "$(jq -r '.cancelled // false' <<<"$item")" == "true" ]]; then
    echo "::error::The queued build was cancelled in Jenkins"; exit 1
  fi
  build_number=$(jq -r '.executable.number // empty' <<<"$item")
  if [[ -z "$build_number" ]]; then
    echo "Waiting in the Jenkins queue: $(jq -r '.why // "no reason given"' <<<"$item")"
    sleep "$POLL_SECONDS"
  fi
done

build_url="$job_url/$build_number/"
echo "Jenkins build started: $build_url"
if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
  echo "build_url=$build_url" >> "$GITHUB_OUTPUT"
fi

# ---- 3. wait for the build to finish ----------------------------------------
result=""
while [[ -z "$result" ]]; do
  if past_deadline; then
    echo "::error::Timed out after ${TIMEOUT_SECONDS}s; the build is still running in Jenkins: $build_url"
    exit 1
  fi
  sleep "$POLL_SECONDS"
  # One failed poll is not a failed build: a Jenkins restart or a proxy
  # hiccup should not turn a green build red. The deadline still bounds it.
  state=$(jenkins --fail "${build_url}api/json?tree=building,result") || { echo "Poll failed; retrying"; continue; }
  if [[ "$(jq -r '.building' <<<"$state")" == "false" ]]; then
    result=$(jq -r '.result // empty' <<<"$state")
  fi
done

if [[ -n "${GITHUB_STEP_SUMMARY:-}" ]]; then
  {
    echo "### Jenkins image build"
    echo ""
    echo "| | |"
    echo "|---|---|"
    echo "| Result | \`$result\` |"
    echo "| Commit | \`$GIT_SHA\` |"
    echo "| Ref | \`$GIT_REF\` |"
    echo "| Build | $build_url |"
    echo "| Console | ${build_url}console |"
  } >> "$GITHUB_STEP_SUMMARY"
fi

if [[ "$result" != "SUCCESS" ]]; then
  echo "::error::Jenkins build finished with $result: ${build_url}console"
  exit 1
fi
echo "Jenkins build succeeded: $build_url"
