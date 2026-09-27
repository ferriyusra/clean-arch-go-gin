# CI/CD

GitHub Actions tests every change. Jenkins turns commits that passed into a
container image on GitHub Container Registry. There is no deployment step:
the pipeline ends at `ghcr.io/ferriyusra/clean-arch-go-gin`, and whatever runs
the service pulls from there.

```
 pull request, or push to main / v* tag
        │
        ▼
 GitHub Actions ─ ci.yml ─────────────────────────────────────────────┐
   build-and-test   gofmt · go mod tidy · vet · build · mocks · -race │
   lint             golangci-lint                                     │
        │ both green, and the event is a push to main or a v* tag     │
        ▼                                                             │
   image ─ jenkins.yml (environment: jenkins)                         │
        │ .github/scripts/trigger-jenkins.sh                          │
        │ POST buildWithParameters (GIT_SHA, GIT_REF, SOURCE_RUN_URL) │
        │ then polls until the Jenkins build finishes                 │
        ▼                                                             │
 Jenkins ─ Jenkinsfile                                                │
   Validate → Checkout GIT_SHA → Build → Smoke test → Push            │
        │                                                             │
        ▼                                                             │
 ghcr.io/ferriyusra/clean-arch-go-gin:sha-1a2b3c4, :main, :1.2.3 …    │
                                                                      │
 Security ─ security.yml: govulncheck on push, PR and weekly ─────────┘
```

The push's CI run stays in progress while Jenkins works and fails if Jenkins
fails. So the CI run shows whether a commit made it all the way to the
registry, and its job summary links to the Jenkins build.

## Contents

- [What runs where](#what-runs-where)
- [When an image is built](#when-an-image-is-built)
- [Image tags](#image-tags)
- [One-time setup](#one-time-setup)
- [Releasing](#releasing)
- [Re-running and dry runs](#re-running-and-dry-runs)
- [What the Jenkins build checks](#what-the-jenkins-build-checks)
- [Security model](#security-model)
- [Troubleshooting](#troubleshooting)
- [Changing the pipeline](#changing-the-pipeline)
- [Possible next steps](#possible-next-steps)

## What runs where

| File | Runs on | Job |
|---|---|---|
| `.github/workflows/ci.yml` | GitHub: pushes to `main`, `v*` tags, every PR | Tests and lint. On a push, also calls `jenkins.yml` once both pass |
| `.github/workflows/jenkins.yml` | GitHub: called by `ci.yml`, or started by hand | Sends the tested commit to Jenkins and waits for the result |
| `.github/scripts/trigger-jenkins.sh` | Inside `jenkins.yml`, or on your laptop | The Jenkins API calls: queue the build, follow it, report the result |
| `Jenkinsfile` | Jenkins | Builds, smoke-tests and pushes the image |
| `.github/workflows/security.yml` | GitHub: push, PR and weekly | `govulncheck`. It does not gate the image; see below |
| `Dockerfile` | Jenkins (and `make docker-build`) | The image itself: a static binary on `distroless/static-debian13:nonroot` |

**Why not do everything in Actions?** GitHub Actions could push to GHCR by
itself. The split puts registry credentials and the Docker daemon on
infrastructure you control. Actions holds only a Jenkins token that can start
one job, and that token cannot publish anything.

**Why is govulncheck not a gate?** It runs in its own workflow because it
needs a weekly schedule; its header comment explains why that is kept out of
`ci.yml`. A vulnerability published tomorrow should not stop the release that
fixes it. A red Security run is a signal to act on, not a lock on the registry.

## When an image is built

| Event | CI runs | Image built |
|---|---|---|
| Pull request, from a fork or a branch | yes | **no**: the `image` job runs only for pushes |
| Push to `main` | yes | yes, if `build-and-test` and `lint` pass |
| Push of a tag `vMAJOR.MINOR.PATCH` or `vMAJOR.MINOR.PATCH-prerelease` | yes | yes, if both pass and the tagged commit is on `main` or a `release/*` branch |
| Push of any other `v*` tag (`v1`, `v1.2`) | yes | no: Jenkins rejects the ref and the `image` job fails |
| Push to any other branch | no | no |
| Manual run of the **Jenkins image** workflow | no | yes, for `main` or a `v*` tag only; see [Re-running](#re-running-and-dry-runs) |

What keeps pull requests out is `if: github.event_name == 'push'` on the
`image` job in `ci.yml`. `needs:` alone would run it after a PR's checks too.
`jenkins.yml` backs that up by accepting only `refs/heads/main` and
`refs/tags/v*`, and the `jenkins` environment backs up both with its
deployment rules.

The trigger is a reusable-workflow call from `ci.yml`, not a `workflow_run`
trigger:

- The called workflow's `github.sha` and `github.ref` are the push's own commit
  and full ref. In a `workflow_run` workflow they are the default branch's. The
  tested commit is only in `github.event.workflow_run.head_sha`, and the ref
  only as a bare `head_branch`, which cannot tell the tag `v1.2.3` from a branch
  named `v1.2.3`.
- `workflow_run` also fires for fork PRs, and a fork can name its branch
  `main`, so every guard would have to be written by hand.

**Several pushes in quick succession.** Image jobs for one ref run one at a
time, in the order they reach the queue (`concurrency` with `queue: max` in
`jenkins.yml`). Every commit gets its own build and its own `sha-` tag. The
order can differ from the push order when CI runs finish out of order. That is
safe, because each build moves `:main` only if its commit is still the tip.

## Image tags

| Ref | Tags pushed |
|---|---|
| `refs/heads/main` | `sha-1a2b3c4`, plus `main` **if the commit is still main's tip** |
| `refs/tags/v1.2.3` | `sha-…`, `1.2.3`, plus `1.2` if it is the newest `v1.2.x`, plus `latest` if it is the newest release overall |
| `refs/tags/v1.3.0-rc.1` | `sha-…`, `1.3.0-rc.1`, and nothing else |

The floating tags (`main`, `X.Y`, `latest`) only move forward. The order in
which builds finish is not the order in which commits were made:

- CI for an older commit can finish after CI for a newer one. Without the
  tip check, the late build would move `:main` backwards. The check compares
  against `origin/main` as fetched when that build starts.
- A `v1.1.4` hotfix released from `release/1.1` after `v1.2.0` should update
  `:1.1` but must not take `:latest` away from `1.2.0`.

**`sha-<7 chars>` always names an image built from that commit, but it is not
immutable.** It is pushed again whenever the commit is built again: on a
re-run or a manual build, and routinely when a release tag is pushed for a
commit that main already built. That build carries the release version. Each
rebuild has a new digest, because `--pull` picks up patched base images and
the `created` label changes. To pin one exact image, use the digest the Push
stage prints (`ghcr.io/…@sha256:…`).

Each image also carries OCI labels:

| Label | Value | Set by |
|---|---|---|
| `org.opencontainers.image.source` | `https://github.com/ferriyusra/clean-arch-go-gin` | `Dockerfile` |
| `org.opencontainers.image.version` | the tag (`v1.2.3`), or `git describe` for main | `Dockerfile`, from `--build-arg VERSION` |
| `org.opencontainers.image.revision` | the full commit SHA | Jenkins |
| `org.opencontainers.image.created` | build time, UTC | Jenkins |
| `org.opencontainers.image.licenses` / `.description` | `MIT` / a one-liner | `Dockerfile` |

`source` is the label GHCR uses to link the package to this repository. Without
it, an image pushed from outside Actions appears under the account with no
repository attached.

The same version string is stamped into the binary through `-ldflags`. It is
what `"version"` in the `server starting` log line reports, and the smoke test
checks that it arrived.

## One-time setup

Do these in order. Each step needs the ones before it.

### 1. Protect the refs that publish

Pushing to `main` publishes `:main`, and pushing a `v*` tag publishes a
release. Whoever can do either can publish. The Jenkinsfile refuses a tag on a
commit that is on neither `main` nor a `release/*` branch, but that only means
something if those branches are protected. Under *Settings → Rules →
Rulesets*:

- **Branches `main` and `release/*`:** require a pull request, and require the
  `build-and-test` and `lint` status checks. Block force pushes and deletion.
- **Tags `refs/tags/v*`:** restrict creation, update and deletion to
  maintainers (the bypass list). Without this, anyone with write access can
  publish `:latest`, or move a released tag and overwrite `:X.Y.Z`.

On a repository with a single owner these rules change nothing today, and they
are what keep it that way when a collaborator joins.

### 2. A GHCR token for Jenkins

GHCR accepts only a **classic** personal access token from outside Actions;
fine-grained tokens are not supported for the container registry.

`ghcr.io/ferriyusra/…` is a **personal namespace**, so only `ferriyusra` can
create the package. Use a token of that account. To use a separate machine
account instead, publish once with the owner's token. The package then exists
and is linked to the repository. Then give the machine account *Write*, either
under *Package settings → Manage access*, or by making it a Write collaborator
on the repository, which the linked package inherits. Finally, swap the
Jenkins credential.

1. Open **<https://github.com/settings/tokens/new?scopes=write:packages>**.
   It pre-selects only `write:packages` and `read:packages`. Do not start from
   the plain *Generate new token (classic)* form. Ticking `write:packages`
   there also selects `repo`, which gives the token full control of every
   repository the account can reach, including pushing to this one's `main`.
2. Set an expiry and put a reminder in the calendar. An expired token fails the
   Push stage with `denied`.
3. Check what the token can do. This keeps it off the command line:

   ```bash
   printf 'header = "Authorization: Bearer %s"\n' "$PAT" \
     | curl -sI -K - https://api.github.com/user | grep -i '^x-oauth-scopes'
   ```

   It must print `read:packages, write:packages` and nothing else. If it
   lists `repo`, delete the token and start again from the link.

### 3. Jenkins

**Plugins.** The standard "suggested plugins" set covers everything:
*Pipeline*, *Git* (4.3 or later, for tag pruning), *Credentials Binding*,
*Timestamper*, *Folders* and *Matrix Authorization Strategy*. The Docker
Pipeline plugin is **not** needed; the Jenkinsfile runs the `docker` CLI
directly.

**Agent.** A Linux agent with the label `docker` and:

- `git`, and the `docker` CLI **with the buildx plugin**
  (`docker-buildx-plugin` from Docker's repositories, or `docker-buildx` on
  Debian/Ubuntu's `docker.io`, which Ubuntu does not install by default).
  The Jenkinsfile sets `DOCKER_BUILDKIT=1`. On CLI 23 and later, that makes a
  missing buildx a hard error rather than a quiet fallback to the deprecated
  legacy builder.
- Access to a Docker daemon. The Push stage switches to a per-build Docker
  config, so it first pins the daemon through `DOCKER_HOST`, taken from the
  current `docker context`. Unix sockets (including rootless) and `ssh://`
  contexts work. A `tcp://` context with TLS client certificates does not.
- Outbound access to `github.com` (checkout), Docker Hub (the `dockerfile:1`
  frontend, `golang`, `curlimages/curl`), `gcr.io` (distroless),
  `proxy.golang.org` (`go mod download` inside the build) and `ghcr.io`.
  Image pulls and the build's own traffic leave from the **daemon's** host,
  which may not be the agent's.
- A POSIX `sh` with `od`, `tr` and `date`. Any normal distribution has them.

To use another label, change `agent { label 'docker' }` in the Jenkinsfile.

**Authorization.** *Manage Jenkins → Security → Authorization* → **Project-based
Matrix Authorization Strategy**. Keep your admin user or group at
*Overall/Administer*. Give *Authenticated Users* nothing beyond
*Overall/Read*, because project-based entries only ever add to the global ones.
Under the default *Logged-in users can do anything*, every user is an
administrator, the trigger user included.

**A folder with its own credentials.** *New Item → Folder*, named
`clean-arch-go-gin`. Then *(folder) → Credentials → Stores scoped to
clean-arch-go-gin → Global credentials → Add Credentials*:

| Field | Value |
|---|---|
| Kind | Username with password |
| Username | the GitHub account that owns the token (see step 2) |
| Password | the classic PAT from step 2 |
| ID | **`ghcr-credentials`** (the Jenkinsfile's `GHCR_CREDENTIALS_ID`) |

A credential in the controller-wide store can be used by every job on the
controller. In the folder, only jobs inside it can use it. Keep this job the
folder's only item, and do not give anyone but admins *Job/Configure* or
*Item/Create* there, since whoever can configure a job in the folder can use
the token.

**The job.** Inside the folder, *New Item → Pipeline*, named `image`. Its path,
`clean-arch-go-gin/image`, is the default `JENKINS_JOB`.

- *Pipeline → Definition*: **Pipeline script from SCM**
- *SCM*: Git. *Repository URL*: `https://github.com/ferriyusra/clean-arch-go-gin.git`.
  Credentials only if the repository becomes private.
- *Branch Specifier*: `*/main`. This is where the **Jenkinsfile** is read from.
  The commit that gets **built** is always the `GIT_SHA` parameter, so a tag
  build uses main's Jenkinsfile to build the tagged commit.
- Leave the remote *Name* and *Refspec* blank. The build relies on the defaults
  (`origin`, all branches, tags fetched) to check whether a commit is on
  `origin/main` or `origin/release/*`, and where a tag points.
- *Script Path*: `Jenkinsfile`. *Lightweight checkout*: on.
- Leave *Build Triggers* empty. GitHub Actions starts the job; a Jenkins
  webhook or poll would build commits that CI has not passed.

**First run.** Jenkins learns a Jenkinsfile's parameters only by running it.
Until then, the trigger gets `400 … is not parameterized`. Click **Build Now**
once. With no `GIT_SHA`, the run stops at *Register parameters*, builds
nothing, and shows *Build with Parameters* from then on. If it instead fails
at once with `Invalid option type "timestamps"`, install Timestamper and click
**Build Now** again.

**The trigger user.** Create a Jenkins user for GitHub (e.g. `gha-trigger`).
In the global matrix from *Authorization* above, give it **Overall/Read** only.
Then, on the job, *Configure → Enable project-based security*: add
`gha-trigger` with **Job/Read** and **Job/Build**. That checkbox appears only
once the global strategy is project-based. Finally, logged in as that user,
*(user) → Security → API Token → Add new token*. An API token needs no CSRF
crumb, and it can be revoked without touching a password.

### 4. GitHub

**Reachability.** GitHub-hosted runners call Jenkins from the public
internet, so `JENKINS_URL` must be reachable from there over HTTPS. If Jenkins
is private, run the trigger job on a self-hosted runner inside the network by
changing `runs-on` in `jenkins.yml`.

**The `jenkins` environment.** *Settings → Environments → New environment*,
named **`jenkins`** (the name `jenkins.yml` uses):

- *Deployment branches and tags* → **Selected branches and tags**: add the
  branch `main` and the tag `v*`. A job running for any other ref is refused
  before it can read the secrets. A repository secret, by contrast, is
  readable by a workflow on any branch a collaborator pushes.
- Add these under the environment, **not** as repository secrets or variables.
  The called workflow receives nothing from `ci.yml`, so a repository-level
  secret never reaches it:

| Kind | Name | Value |
|---|---|---|
| Variable | `JENKINS_URL` | Jenkins root URL, e.g. `https://jenkins.example.com`. Use the exact scheme and host; a redirect is treated as a failure |
| Variable (optional) | `JENKINS_JOB` | job path if it is not `clean-arch-go-gin/image` |
| Secret | `JENKINS_USER` | the trigger user, e.g. `gha-trigger` |
| Secret | `JENKINS_API_TOKEN` | that user's API token |

`JENKINS_URL` is a variable because the runner masks secrets everywhere. As a
secret, every Jenkins link in the log and job summary would read `***`. The
host is not a credential: it has to be reachable from the internet anyway.
Each image job appears as a deployment to `jenkins` on the repository page.
That is expected.

**Check it before pushing.** The trigger script runs anywhere with bash,
`curl` and `jq`. Run it once from your machine against a commit that is
already on main:

```bash
JENKINS_URL=https://jenkins.example.com JENKINS_USER=gha-trigger \
JENKINS_API_TOKEN=... JENKINS_JOB=clean-arch-go-gin/image \
GIT_SHA=$(git rev-parse origin/main) GIT_REF=refs/heads/main \
bash .github/scripts/trigger-jenkins.sh
```

### 5. After the first push

A new GHCR package is **private**. Linked through the `source` label, it
inherits the repository's access *permissions* but not its visibility. To let
anyone pull it: *Package → Package settings → Change visibility → Public*.
Making a package public cannot be undone.

## Releasing

```bash
git tag -a v1.2.3 -m "v1.2.3"          # on a commit that is on main
git push origin v1.2.3
```

The tag push runs CI on the tagged commit, and then the image job, which
publishes `1.2.3`, plus `1.2` and `latest` where they apply (see
[Image tags](#image-tags)). Tags must be `vMAJOR.MINOR.PATCH` with an optional
`-prerelease`. Build metadata (`+build.5`) is rejected, because `+` is not
valid in an image tag.

A hotfix for an older line is tagged on a `release/X.Y` branch (for example
`release/1.1`), cut from the last `vX.Y.*` tag and protected like `main`. A tag
on any other commit is refused by Jenkins.

A released tag is not moved. To withdraw a release, delete the tag and its
GHCR package version, then release a new patch version. Jenkins prunes deleted
tags on its next build, so a deleted tag stops counting as "newest" for
`:latest` and `:X.Y`.

## Re-running and dry runs

| Situation | Do this |
|---|---|
| Jenkins was down or flaky, and the CI run is recent | On the CI run, **Re-run failed jobs**. Only the `image` job runs again |
| The CI run is too old to re-run | *Actions → Jenkins image → Run workflow* and pick `main` or the tag. **This skips CI**, so only use it for a ref whose CI passed. On `main` it builds main's current tip |
| Rebuild and smoke-test an already-merged commit without pushing | In Jenkins, *Build with Parameters* with a SHA on main (or a tag's SHA), its ref, and **`DRY_RUN`** ticked. Useful after a failed build, or to check that today's base images still build. It cannot test unmerged changes, because Jenkins only builds commits on main or a release branch |
| Try a Dockerfile change before merging | Locally: `make docker-build`, then run the image the way the smoke test does (below) |
| Rebuild the same commit | Allowed. `sha-…` is re-pushed with a new digest; see [Image tags](#image-tags) |

Running an image the way the smoke test does:

```bash
docker run --rm --read-only --tmpfs /tmp -p 8080:8080 \
  -e JWT_ACCESS_SECRET="$(openssl rand -hex 24)" \
  -e JWT_REFRESH_SECRET="$(openssl rand -hex 24)" \
  -e CSRF_SECRET="$(openssl rand -hex 24)" \
  -e DATABASE_DSN=/tmp/smoke.db \
  clean-arch-go-gin:<version>
curl -s localhost:8080/api/health/ready
curl -s localhost:8080/api/v1/message
```

## What the Jenkins build checks

1. **Validate.** `GIT_SHA` must be 40 lowercase hex characters, and `GIT_REF`
   must be `refs/heads/main` or a release tag. Anything else stops the build
   before an agent is used.
2. **Checkout.** Checks out exactly `GIT_SHA`, pruning branches and tags that
   no longer exist on GitHub, because the workspace is reused between builds.
   It then refuses to go on unless:
   - for main: the commit is on `origin/main`;
   - for a tag: the tag points at the commit, **and** the commit is on
     `origin/main` or an `origin/release/*` branch.

   Anyone who can start the job can type any SHA. Only commits on those
   branches are ever published.
3. **Build.** `docker build --pull --load` with `VERSION` and the OCI labels.
   `--pull` makes every build start from the current, patched base images.
4. **Smoke test.** Runs the image the way production would: `DEV_MODE` off,
   three random 48-character secrets (kept out of the log), and a **read-only
   root filesystem** with the sqlite file on a tmpfs `/tmp`. Then, from a
   `curlimages/curl` container on the app's network namespace (distroless has
   no shell), it checks:
   - `GET /api/health/live` and `GET /api/health/ready` answer `success: true`;
   - `GET /api/v1/message` returns a message with `content`. That row is
     seeded by migration 2, so migrations ran. A missing row would still
     answer `success: true`, with `data: null`;
   - the startup log reports the expected `version`.
5. **Push.** Logs in with `ghcr-credentials` into a per-build
   `DOCKER_CONFIG`, pushes the tags from [Image tags](#image-tags), and prints
   the digest.

The stage's `post` removes the smoke container, every local tag of the image
and the per-build Docker config, whatever the outcome.

## Security model

- **Pull requests never reach Jenkins.** The `image` job runs only for `push`
  events, and fork PRs get no secrets in any case.
- **Only protected refs publish.** Jenkins publishes commits on `main` or a
  `release/*` branch, and only for `main` or a `v*` tag. Who may push those
  refs is decided by the rulesets in [step 1](#1-protect-the-refs-that-publish).
  Without them, anyone with write access can release.
- **The Jenkins token reaches only main and release tags.** It lives in the
  `jenkins` environment, whose deployment rules admit only those refs. A
  workflow on another branch cannot read it.
- **The GitHub side cannot publish.** Actions holds a Jenkins token that can
  start one job, and the `GITHUB_TOKEN` is `contents: read`. The registry
  credential lives only in Jenkins, in the job's folder.
- **The registry token can only publish packages.** It is a classic PAT with
  `write:packages` and `read:packages`. A leak lets someone push images to the
  account's packages, but not touch source code. That holds only if it was
  created from the scoped link in [step 2](#2-a-ghcr-token-for-jenkins).
- **Jenkins does not trust its parameters.** The SHA and ref are
  regex-validated, then checked against the repository's own history before
  anything is built.
- **No interpolation into shells.** The workflow passes `github.*` values
  through `env:`, and the Jenkinsfile's `sh` steps are single-quoted and read
  parameters from the environment. A crafted ref cannot become a command.
- **Secrets stay off command lines and out of logs.** The trigger script feeds
  credentials to curl on stdin, and Jenkins passes the GHCR token to
  `docker login` on stdin. The smoke-test secrets reach `docker run` through
  the environment, and are generated with shell tracing off.
- **The login does not outlive the build.** It goes to a per-build
  `DOCKER_CONFIG` that `post` deletes, never to the agent's `~/.docker`.
- **The build runs repository code on the agent.** `docker build` executes the
  Dockerfile's `RUN` steps, and the smoke test runs the app. Both come from
  the commit being built. That is why only protected refs are built, and why
  the agent should not have access it does not need.

## Troubleshooting

| Symptom | Cause and fix |
|---|---|
| Every Actions job fails in seconds with no steps | Actions is not running for the account at all, often a billing or spending-limit lock. The job annotation says why. Fix it under *Settings → Billing* |
| `image` job: `JENKINS_URL is not set`, or `JENKINS_USER is not set` | The value is missing from the `jenkins` **environment**. Repository-level secrets and variables do not reach the called workflow. See [GitHub setup](#4-github) |
| `image` job fails with `… is not allowed to deploy to jenkins due to environment protection rules` | The ref is not in the environment's deployment rules. Add `main` and the tag `v*` |
| `Jenkins answered 400 … is not parameterized` | The job has never run. Click **Build Now** once ([First run](#3-jenkins)) |
| `Jenkins refused the credentials (401/403)` | Wrong user or token, or the user lacks *Overall/Read*, *Job/Read* or *Job/Build* |
| `Jenkins has no job at … (404)` | `JENKINS_JOB` does not match. A job in a folder is `folder/job`, and the default is `clean-arch-go-gin/image` |
| `Unexpected answer 301/302` | `JENKINS_URL` redirects, e.g. `http` to `https`. Use the final URL |
| `Jenkins no longer knows queue item …` | The item vanished before the script saw it start. Jenkins forgets an item 5 minutes after it leaves the queue, and on restart. Check the job's recent builds. Transient poll failures are retried and do not cause this |
| `Timed out waiting for Jenkins to start the build` | The queue item stayed blocked for the whole deadline (`TIMEOUT_SECONDS`, 40 minutes). Usually earlier builds of this job are still running: `disableConcurrentBuilds()` runs one at a time, and a main build and a tag build of the same commit queue behind each other. It can also mean Jenkins is quieting down. The `Waiting in the Jenkins queue:` lines above it quote Jenkins' reason |
| `Jenkins build finished with ABORTED` about 30 minutes after it started | The pipeline's 30-minute `timeout` fired. Most often no online agent has the `docker` label, or its executors are busy. The console shows `Still waiting to schedule task` or `Waiting for next available executor on docker`. Bring an agent online, or fix its label |
| `Timed out after 2400s; the build is still running` | Queue time and build time share one 40-minute deadline, so a build that queued behind another can hit it. Raise `TIMEOUT_SECONDS` and `timeout-minutes` in `jenkins.yml` together |
| `GIT_REF must be refs/heads/main or refs/tags/v…` | A tag like `v1` or `v1.2`. Release tags need three numbers |
| `… is not on origin/main; refusing to publish it` | A manual run with a SHA from another branch. Merge first |
| `… is on neither origin/main nor an origin/release/* branch; refusing to publish v…` | The tag is on an unmerged commit. Merge the change, and tag the merged commit |
| `Tag … points at '…', not …` | The tag was moved after CI ran. Released tags are not moved; see [Releasing](#releasing) |
| `ERROR: BuildKit is enabled but the buildx component is missing or broken` | Install the buildx plugin on the agent (see [Agent](#3-jenkins)) |
| `Smoke test failed: GET /api/health/live` | The app exited at startup: invalid configuration, or a database it could not open. The container log printed below ends with one JSON `"msg":"fatal"` line. Each configuration problem is inside its `error` field, separated by `\n  - ` |
| `Smoke test failed: GET /api/health/ready …` | The app started, but its database health check failed. The container log is printed below |
| `the startup log does not report version …` | `VERSION` did not reach `-ldflags`. Check the `ARG VERSION` / `-X main.version` pair in the `Dockerfile` |
| Push: `denied` or `unauthorized` | The PAT expired, is fine-grained, or lacks `write:packages`. Or the token's user cannot write this package: only the namespace owner can create it, and others need *Write* (see [step 2](#2-a-ghcr-token-for-jenkins)) |
| `WorkflowScript: …: Invalid option type "timestamps"` | The Timestamper plugin is missing. Declarative rejects the Jenkinsfile before any stage runs, so the parameters never get registered either. Install it, then **Build Now** |
| `RejectedAccessException: Scripts not permitted…` | The Groovy sandbox blocked a call. Approve it under *Manage Jenkins → In-process Script Approval* |
| Image pushed but not shown on the repository page | The package is not linked. Check the `source` label: `docker inspect --format '{{json .Config.Labels}}' <image>` |

## Changing the pipeline

- **Workflows.** Validate with
  `go run github.com/rhysd/actionlint/cmd/actionlint@latest`. It also runs
  shellcheck on `run:` blocks if `shellcheck` is on your `PATH`.
  `.github/actionlint.yaml` silences one false positive: actionlint does not
  know `concurrency.queue` yet. Remove that entry once it does.
- **The trigger script.** `shellcheck .github/scripts/trigger-jenkins.sh`, then
  run it against Jenkins as in [GitHub setup](#4-github).
- **The Jenkinsfile.** Validate against your Jenkins before committing:

  ```bash
  curl -sS -u "$JENKINS_USER:$JENKINS_API_TOKEN" -X POST \
    -F "jenkinsfile=<Jenkinsfile" "$JENKINS_URL/pipeline-model-converter/validate"
  ```

  The job reads the Jenkinsfile from `main`, so a change takes effect once it
  is merged. To try one first, point a scratch job's *Branch Specifier* at your
  branch and build an already-merged SHA with `DRY_RUN`. The Dockerfile still
  comes from that SHA, not from your branch.
- **The image name.** `IMAGE` in the Jenkinsfile is the only place that
  changes behaviour. The name is also written out in this document, the CI/CD
  section of `README.md`, the Container image section of `internal/README.md`
  and `CLAUDE.md`. `grep -rn 'ghcr.io/ferriyusra/clean-arch-go-gin' .` finds
  them all. The `Dockerfile`'s `org.opencontainers.image.source` label holds
  the *repository* URL. Change it when the owning account or repository
  changes, because GHCR links a package only to a repository with the same
  owner.
- **Renaming `CI`, `build-and-test` or `lint`.** Nothing breaks, because
  `jenkins.yml` is called by path and `needs:` names jobs in the same file.
  Keep the `needs:` list in step with any job added to CI that should gate the
  image, and keep the rulesets' required checks in step with the job names.

These files have to agree with this document, like the others listed in
`CLAUDE.md` under *Doc Accuracy*.

## Possible next steps

Not done yet, in rough order of value:

- **Build the image on pull requests.** A `docker build` job in `ci.yml`, with
  no push and no secrets, would catch a broken Dockerfile before merge instead
  of in Jenkins afterwards.
- **Scan the image before pushing.** A Trivy stage between *Smoke test* and
  *Push* that fails on HIGH/CRITICAL findings with a fix available.
- **Pin govulncheck.** `make vuln` and the action both install `@latest`.
  Declaring it in `go.mod`'s `tool` block, like mockgen and air, would make the
  scan reproducible.
- **Sign and attest.** `cosign sign` plus an SBOM (`docker buildx build --sbom`)
  so a consumer can verify where an image came from.
- **Multi-arch.** `docker buildx build --platform linux/amd64,linux/arm64`.
  The binary is static and the base image supports both.
