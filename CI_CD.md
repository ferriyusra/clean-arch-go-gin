# CI/CD

GitHub Actions tests every change. Jenkins turns commits that passed into a
container image on GitHub Container Registry. There is no deployment step:
the pipeline ends at `ghcr.io/ferriyusra/clean-arch-go-gin`, and whatever runs
the service pulls from there.

```
 push / pull request
        │
        ▼
 GitHub Actions ─ ci.yml ─────────────────────────────────────────────┐
   build-and-test   gofmt · go mod tidy · vet · build · mocks · -race │
   lint             golangci-lint                                     │
        │ both green, and the event is a push to main or a v* tag     │
        ▼                                                             │
   image ─ jenkins.yml ─ .github/scripts/trigger-jenkins.sh           │
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

The GitHub check for the push stays yellow while Jenkins works and turns red if
Jenkins fails. One place shows whether a commit made it all the way to the
registry.

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
| `.github/workflows/ci.yml` | GitHub, every push to `main`, every `v*` tag, every PR | Tests and lint. On a push, also calls `jenkins.yml` once both pass |
| `.github/workflows/jenkins.yml` | GitHub, called by `ci.yml` or started by hand | Sends the tested commit to Jenkins and waits for the result |
| `.github/scripts/trigger-jenkins.sh` | Inside `jenkins.yml`, or on your laptop | The Jenkins API calls: queue the build, follow it, report the result |
| `Jenkinsfile` | Jenkins | Builds, smoke-tests and pushes the image |
| `.github/workflows/security.yml` | GitHub, push, PR and weekly | `govulncheck`. It does not gate the image; see below |
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
| Pull request | yes | **no**: PRs get no secrets, and unmerged code never reaches the registry |
| Push to `main` | yes | yes, if `build-and-test` and `lint` pass |
| Push of a tag `vMAJOR.MINOR.PATCH` or `vMAJOR.MINOR.PATCH-prerelease` | yes | yes, if both pass |
| Push of any other `v*` tag (`v1`, `v1.2`) | yes | no: Jenkins rejects the ref and the `image` job fails |
| Push to any other branch | no | no |
| Manual run of the **Jenkins image** workflow | no | yes, for `main` or a `v*` tag only; see [Re-running](#re-running-and-dry-runs) |

The trigger is a reusable-workflow call from `ci.yml`, not a `workflow_run`
trigger. That choice is deliberate:

- The called workflow sees the real `github.sha` and `github.ref` of the push.
  A `workflow_run` workflow sees only the default branch's HEAD, and its
  `head_branch` cannot tell the tag `v1.2.3` from a branch named `v1.2.3`.
- `workflow_run` also fires for fork PRs, and a fork can name its branch
  `main`. Guarding against that takes three conditions that are easy to get
  wrong. A `needs:` on a push-only job has nothing to guard.

## Image tags

Every build pushes the immutable `sha-<7 chars>` tag. The floating tags only
move forward:

| Ref | Tags pushed |
|---|---|
| `refs/heads/main` | `sha-1a2b3c4`, plus `main` **if the commit is still main's tip** |
| `refs/tags/v1.2.3` | `sha-…`, `1.2.3`, plus `1.2` if it is the newest `v1.2.x`, plus `latest` if it is the newest release overall |
| `refs/tags/v1.3.0-rc.1` | `sha-…`, `1.3.0-rc.1`, and nothing else |

The guards exist because the order in which builds finish is not the order in
which commits were made:

- CI for an older commit can finish after CI for a newer one. Without the
  tip check, the late build would move `:main` backwards.
- A `v1.1.4` hotfix released after `v1.2.0` should update `:1.1` but must not
  take `:latest` away from `1.2.0`.

The tip check compares against `origin/main` as fetched at the start of the
build. A commit that landed on main during the few minutes a build takes is
caught by its own build.

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

The same version string is stamped into the binary through `-ldflags`, which
is what `"version"` in the `server starting` log line reports. The smoke test
checks that it arrived.

## One-time setup

Do these in order. Each step is needed by the one after it.

### 1. A GHCR token for Jenkins

GHCR accepts only a **classic** personal access token from outside Actions;
fine-grained tokens are not supported for the container registry.

1. GitHub → *Settings → Developer settings → Personal access tokens → Tokens
   (classic) → Generate new token (classic)*.
2. Scope: **`write:packages`** (GitHub also ticks `read:packages` with it). Set
   an expiry and put a reminder in the calendar, since an expired token fails
   the Push stage with `denied`.
3. Prefer a dedicated machine account with access to this repository over a
   personal account. The token can push to *every* package its owner can write.

### 2. Jenkins

**Plugins.** The standard "suggested plugins" set covers everything:
*Pipeline*, *Git*, *Credentials Binding* and *Timestamper*. *Matrix
Authorization Strategy* is also needed for the least-privilege user below. The
Docker Pipeline plugin is **not** needed; the Jenkinsfile runs the `docker` CLI
directly.

**Agent.** A Linux agent with the label `docker` and:

- `git` and the `docker` CLI, with access to a Docker daemon. Engine 23 or
  later (BuildKit by default), or set `DOCKER_BUILDKIT=1`, which the
  Jenkinsfile already does.
- Outbound access to Docker Hub (the `dockerfile:1` frontend, `golang`,
  `curlimages/curl`), `gcr.io` (distroless) and `ghcr.io`.
- A POSIX `sh` with `od`, `tr` and `date`. Any normal distribution has them.

To use another label, change `agent { label 'docker' }` in the Jenkinsfile.

**Credentials.** *Manage Jenkins → Credentials → (global) → Add Credentials*:

| Field | Value |
|---|---|
| Kind | Username with password |
| Username | the GitHub account that owns the token (e.g. `ferriyusra`) |
| Password | the classic PAT from step 1 |
| ID | **`ghcr-credentials`** (the Jenkinsfile's `GHCR_CREDENTIALS_ID`) |

**The job.** *New Item → Pipeline*, named `clean-arch-go-gin` (anything works;
see `JENKINS_JOB` below):

- *Pipeline → Definition*: **Pipeline script from SCM**
- *SCM*: Git. *Repository URL*: `https://github.com/ferriyusra/clean-arch-go-gin.git`.
  Credentials only if the repository becomes private.
- *Branch Specifier*: `*/main`. This is where the **Jenkinsfile** is read from.
  The commit that gets **built** is always the `GIT_SHA` parameter, so a tag
  build uses main's Jenkinsfile to build the tagged commit.
- Leave the remote *Name* and *Refspec* blank. The build relies on the defaults
  (`origin`, all branches, tags fetched) to check that a commit is on
  `origin/main` or that a tag points at it.
- *Script Path*: `Jenkinsfile`. *Lightweight checkout*: on.
- Leave *Build Triggers* empty. GitHub Actions starts the job; a Jenkins
  webhook or poll would build commits that CI has not passed.

**First run.** Jenkins learns a Jenkinsfile's parameters only by running it.
Until then, the trigger gets `400 … is not parameterized`. Click **Build Now**
once. With no `GIT_SHA`, the run stops at *Register parameters*, builds
nothing, and shows *Build with Parameters* from then on.

**The trigger user.** Create a Jenkins user for GitHub (e.g. `gha-trigger`).
Give it only *Overall/Read*, and *Job/Read* + *Job/Build* on this one job
(Matrix Authorization, set in the job's *Enable project-based security*). Then,
logged in as that user, *(user) → Security → API Token → Add new token*. An
API token needs no CSRF crumb, and it can be revoked without touching a
password.

### 3. GitHub

**Reachability.** GitHub-hosted runners call Jenkins from the public
internet, so `JENKINS_URL` must be reachable from there over HTTPS. If Jenkins
is private, run the `image` job on a self-hosted runner inside the network by
changing `runs-on` in `jenkins.yml`.

*Repository → Settings → Secrets and variables → Actions*:

| Kind | Name | Value |
|---|---|---|
| Secret | `JENKINS_URL` | Jenkins root URL, e.g. `https://jenkins.example.com`. Use the exact scheme and host; a redirect is treated as a failure |
| Secret | `JENKINS_USER` | the trigger user, e.g. `gha-trigger` |
| Secret | `JENKINS_API_TOKEN` | that user's API token |
| Variable (optional) | `JENKINS_JOB` | job path if it is not `clean-arch-go-gin`, e.g. `backend/clean-arch-go-gin` for a job inside a folder |

**Check it before pushing.** The trigger script runs anywhere with bash,
`curl` and `jq`. Run it once from your machine against a commit that is
already on main:

```bash
JENKINS_URL=https://jenkins.example.com JENKINS_USER=gha-trigger \
JENKINS_API_TOKEN=... JENKINS_JOB=clean-arch-go-gin \
GIT_SHA=$(git rev-parse origin/main) GIT_REF=refs/heads/main \
bash .github/scripts/trigger-jenkins.sh
```

### 4. After the first push

A new GHCR package is **private**. Linked through the `source` label, it
inherits the repository's access *permissions* but not its visibility. To let
anyone pull it: *Package → Package settings → Change visibility → Public*.
Making a package public cannot be undone.

## Releasing

```bash
git tag -a v1.2.3 -m "v1.2.3"
git push origin v1.2.3
```

The tag push runs CI on the tagged commit, and then the image job, which
publishes `1.2.3`, `1.2` and `latest` if this is the newest release. Tags must
be `vMAJOR.MINOR.PATCH` with an optional `-prerelease`. Build metadata
(`+build.5`) is rejected, because `+` is not valid in an image tag.

## Re-running and dry runs

| Situation | Do this |
|---|---|
| Jenkins was down or flaky, and the CI run is recent | On the CI run, **Re-run failed jobs**. Only the `image` job runs again |
| The CI run is too old to re-run | *Actions → Jenkins image → Run workflow* and pick `main` or the tag. **This skips CI**, so only use it for a ref whose CI passed |
| Try a Dockerfile change without publishing | In Jenkins, *Build with Parameters* with a commit SHA that is on main, its ref, and **`DRY_RUN`** ticked. It builds and smoke-tests, then stops |
| Rebuild the same commit | Allowed. `sha-…` is overwritten with an image built from the same source |

## What the Jenkins build checks

1. **Validate.** `GIT_SHA` must be 40 lowercase hex characters, and `GIT_REF`
   must be `refs/heads/main` or a release tag. Anything else stops the build
   before an agent is used.
2. **Checkout.** Checks out exactly `GIT_SHA`, then refuses to go on unless
   the commit is on `origin/main` (for main), or the named tag points at it
   (for a tag). Anyone who can start the job can type any SHA; only commits in
   the repository's history are ever published.
3. **Build.** `docker build --pull` with `VERSION` and the OCI labels.
   `--pull` makes every build start from the current, patched base images.
4. **Smoke test.** Runs the image the way production would: `DEV_MODE` off,
   three random 48-character secrets, a **read-only root filesystem** with the
   sqlite file on a tmpfs `/tmp`. Then, from a `curlimages/curl` container on
   the app's network namespace (distroless has no shell), it checks:
   - `GET /api/health/live` and `GET /api/health/ready` answer `success: true`;
   - `GET /api/v1/message` returns a message with `content`. That row is
     seeded by migration 2, so migrations ran. A missing row would still answer
     `success: true`, with `data: null`;
   - the startup log reports the expected `version`.
5. **Push.** Logs in with `ghcr-credentials` into a per-build
   `DOCKER_CONFIG`, pushes the tags from [Image tags](#image-tags), and prints
   the digest.

The stage's `post` removes the smoke container, every local tag of the image
and the per-build Docker config, whatever the outcome.

## Security model

- **Pull requests never reach Jenkins.** The `image` job runs only for `push`
  events, and `pull_request` runs from forks get no secrets.
- **The GitHub side cannot publish.** Actions holds a Jenkins token that can
  start one job, and the `GITHUB_TOKEN` is `contents: read`. The registry
  credential lives only in Jenkins.
- **Jenkins does not trust its parameters.** The SHA and ref are
  regex-validated, then checked against the repository's own history before
  anything is built.
- **No interpolation into shells.** The workflow passes `github.*` values
  through `env:`, and the Jenkinsfile's `sh` steps are single-quoted and read
  parameters from the environment. A crafted ref cannot become a command.
- **Secrets stay off command lines.** The trigger script feeds credentials to
  curl on stdin, and Jenkins passes the GHCR token to `docker login` on stdin.
  The smoke-test secrets reach `docker run` through the environment.
- **The login does not outlive the build.** It goes to a per-build
  `DOCKER_CONFIG` that `post` deletes, never to the agent's `~/.docker`.
- **Only three secrets cross into the reusable workflow**, passed by name
  rather than `secrets: inherit`.

## Troubleshooting

| Symptom | Cause and fix |
|---|---|
| Every Actions job fails in seconds with no steps | Actions is not running for the account at all, often a billing or spending-limit lock. The job annotation says why. Fix it under *Settings → Billing* |
| `image` job: `JENKINS_URL is not set` | The secret is missing, or is named differently. See [GitHub setup](#3-github) |
| `Jenkins answered 400 … is not parameterized` | The job has never run. Click **Build Now** once ([First run](#2-jenkins)) |
| `Jenkins refused the credentials (401/403)` | Wrong user or token, or the user lacks *Overall/Read*, *Job/Read* or *Job/Build* |
| `Jenkins has no job at … (404)` | `JENKINS_JOB` does not match. A job in a folder is `folder/job` |
| `Unexpected answer 301/302` | `JENKINS_URL` redirects, e.g. `http` to `https`. Use the final URL |
| `Could not read queue item …` | The poll could not see the queue item. It needs *Job/Read*, and Jenkins forgets an item 5 minutes after it leaves the queue |
| `Timed out waiting for Jenkins to start the build` | No agent with the `docker` label is free or online |
| `GIT_REF must be refs/heads/main or refs/tags/v…` | A tag like `v1` or `v1.2`. Release tags need three numbers |
| `… is not on origin/main; refusing to publish it` | A manual run with a SHA from another branch. Merge first |
| `Tag … points at '…', not …` | The tag was moved or recreated after CI ran. Delete and re-push the tag consistently |
| `Smoke test failed: GET /api/health/ready …` | The app did not start or cannot reach its database. The container log is printed right below; configuration errors are listed one per line |
| `the startup log does not report version …` | `VERSION` did not reach `-ldflags`. Check the `ARG VERSION` / `-X main.version` pair in the `Dockerfile` |
| Push: `denied` or `unauthorized` | The PAT expired, is fine-grained, or lacks `write:packages` |
| `RejectedAccessException: Scripts not permitted…` | The Groovy sandbox blocked a call. Approve it under *Manage Jenkins → In-process Script Approval* |
| `No such DSL method 'timestamps'` | The Timestamper plugin is missing |
| Image pushed but not shown on the repository page | The package is not linked. Check the `source` label: `docker inspect --format '{{json .Config.Labels}}' <image>` |

## Changing the pipeline

- **Workflows.** Validate with
  `go run github.com/rhysd/actionlint/cmd/actionlint@latest`. It also runs
  shellcheck on `run:` blocks if `shellcheck` is on your `PATH`.
- **The trigger script.** `shellcheck .github/scripts/trigger-jenkins.sh`, then
  run it against Jenkins as in [GitHub setup](#3-github).
- **The Jenkinsfile.** Validate against your Jenkins before committing:

  ```bash
  curl -sS -u "$JENKINS_USER:$JENKINS_API_TOKEN" -X POST \
    -F "jenkinsfile=<Jenkinsfile" "$JENKINS_URL/pipeline-model-converter/validate"
  ```

  Then run it with `DRY_RUN` ticked. The job reads the Jenkinsfile from `main`,
  so a change takes effect once it is merged. To test one on a branch, point a
  scratch job's *Branch Specifier* at that branch.
- **The image name.** It appears in three places: `IMAGE` in the Jenkinsfile,
  the `source` label in the `Dockerfile`, and this document.
- **Renaming `CI`, `build-and-test` or `lint`.** Nothing breaks, because
  `jenkins.yml` is called by path and `needs:` names jobs in the same file.
  Keep the `needs:` list in step with any job added to CI that should gate the
  image.

These files have to agree with this document, like the others listed in
`CLAUDE.md` under *Doc Accuracy*.

## Possible next steps

Not done yet, in rough order of value:

- **Scan the image before pushing.** A Trivy stage between *Smoke test* and
  *Push* that fails on HIGH/CRITICAL findings with a fix available.
- **Pin govulncheck.** `make vuln` and the action both install `@latest`.
  Declaring it in `go.mod`'s `tool` block, like mockgen and air, would make the
  scan reproducible.
- **Sign and attest.** `cosign sign` plus an SBOM (`docker buildx build --sbom`)
  so a consumer can verify where an image came from.
- **Multi-arch.** `docker buildx build --platform linux/amd64,linux/arm64`.
  The binary is static and the base image supports both.
