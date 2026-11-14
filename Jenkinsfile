// Builds the container image for one tested commit, smoke-tests it and pushes
// it to ghcr.io. GitHub Actions starts this job through
// .github/workflows/jenkins.yml once CI has passed; CI_CD.md covers the
// one-time Jenkins and GHCR setup.
//
// Parameters and secrets never pass through Groovy string interpolation into a
// shell. Every sh step is a single-quoted script that reads its inputs from the
// environment. That way a crafted GIT_REF cannot become shell syntax, and a
// token never appears in a process's argument list. The only backslashes in
// these scripts are line continuations, because Groovy's ''' strings process
// escapes before the shell ever sees them.

pipeline {
    agent none

    parameters {
        string(name: 'GIT_SHA', defaultValue: '', trim: true,
               description: 'Full 40-character commit SHA to build. Left empty only by the first run, which registers these parameters.')
        string(name: 'GIT_REF', defaultValue: '', trim: true,
               description: 'refs/heads/main or refs/tags/vMAJOR.MINOR.PATCH[-prerelease]. Decides the image tags.')
        string(name: 'SOURCE_RUN_URL', defaultValue: '', trim: true,
               description: 'The GitHub Actions run that asked for this build.')
        booleanParam(name: 'DRY_RUN', defaultValue: false,
               description: 'Build and smoke-test the image, but push nothing.')
    }

    options {
        skipDefaultCheckout()
        disableConcurrentBuilds()
        timeout(time: 30, unit: 'MINUTES')
        timestamps()
        buildDiscarder(logRotator(numToKeepStr: '50'))
    }

    environment {
        IMAGE = 'ghcr.io/ferriyusra/clean-arch-go-gin'
        GHCR_CREDENTIALS_ID = 'ghcr-credentials'
        CURL_IMAGE = 'curlimages/curl:8.22.0'
        // Forces BuildKit, so a build never silently falls back to the
        // deprecated legacy builder. The agent therefore needs the buildx
        // plugin: with it missing, docker 23+ fails outright.
        DOCKER_BUILDKIT = '1'
    }

    stages {
        // Jenkins learns a Jenkinsfile's parameters only by running it, and
        // until then buildWithParameters answers 400. The first run is started
        // by hand with no GIT_SHA, and this stage makes it a harmless no-op.
        stage('Register parameters') {
            when {
                beforeAgent true
                expression { !params.GIT_SHA }
            }
            steps {
                echo 'GIT_SHA is empty, so this run only registers the job parameters. Nothing is built.'
                script { currentBuild.description = 'Parameters registered; nothing built' }
            }
        }

        stage('Validate') {
            when {
                beforeAgent true
                expression { params.GIT_SHA as boolean }
            }
            steps {
                script {
                    if (!(params.GIT_SHA ==~ /[0-9a-f]{40}/)) {
                        error("GIT_SHA must be a full 40-character lowercase commit SHA, got: ${params.GIT_SHA}")
                    }
                    if (!(params.GIT_REF ==~ /refs\/heads\/main|refs\/tags\/v\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?/)) {
                        error("GIT_REF must be refs/heads/main or refs/tags/vMAJOR.MINOR.PATCH[-prerelease], got: ${params.GIT_REF}")
                    }
                    env.SHORT_SHA = params.GIT_SHA.substring(0, 7)
                    def buildId = env.BUILD_TAG.replaceAll('[^A-Za-z0-9_.-]', '-').toLowerCase()
                    // A local-only repository name, so nothing but the Push
                    // stage ever gives this image a ghcr.io name.
                    env.LOCAL_IMAGE = "clean-arch-go-gin-ci:${buildId}"
                    env.SMOKE_CONTAINER = "smoke-${buildId}"
                    currentBuild.description = "${params.GIT_REF.replaceFirst('^refs/(heads|tags)/', '')} @ ${env.SHORT_SHA}"
                }
            }
        }

        stage('Image') {
            when {
                beforeAgent true
                expression { params.GIT_SHA as boolean }
            }
            agent { label 'docker' }
            stages {
                stage('Checkout') {
                    steps {
                        // The job reads this Jenkinsfile from main, but builds
                        // exactly the commit CI tested. Tags are fetched (the
                        // git plugin's default) so the tag checks and
                        // `git describe` below can see them. The workspace is
                        // reused between builds, so stale tags and branches are
                        // pruned. Otherwise a tag deleted on GitHub would still
                        // pass the checks below and still count as the newest
                        // release.
                        checkout([$class: 'GitSCM',
                            branches: [[name: params.GIT_SHA]],
                            userRemoteConfigs: scm.userRemoteConfigs,
                            extensions: [
                                [$class: 'CleanBeforeCheckout'],
                                [$class: 'CloneOption', noTags: false, shallow: false, depth: 0, reference: '', timeout: 10],
                                [$class: 'PruneStaleBranch'],
                                [$class: 'PruneStaleTag', pruneTags: true],
                            ],
                        ])
                        // Anyone who can start this job could pass any SHA, so
                        // the ref is checked against the repository itself. A
                        // main build must be on origin/main. A tag build must
                        // be what the tag points at, and on main or on a
                        // release/* branch, where hotfixes for older lines
                        // live. An unmerged commit therefore cannot be published
                        // by tagging it, provided main and release/* are
                        // protected branches (CI_CD.md, "Protect the refs").
                        sh '''
                            set -eu
                            head=$(git rev-parse HEAD)
                            if [ "$head" != "$GIT_SHA" ]; then
                                echo "Checked out $head, expected $GIT_SHA" >&2
                                exit 1
                            fi
                            case "$GIT_REF" in
                                refs/heads/main)
                                    if ! git merge-base --is-ancestor "$GIT_SHA" refs/remotes/origin/main; then
                                        echo "$GIT_SHA is not on origin/main; refusing to publish it" >&2
                                        exit 1
                                    fi
                                    ;;
                                refs/tags/*)
                                    tag=${GIT_REF#refs/tags/}
                                    tagged=$(git rev-parse --verify --quiet "refs/tags/$tag^{commit}" || true)
                                    if [ "$tagged" != "$GIT_SHA" ]; then
                                        echo "Tag $tag points at '${tagged:-nothing}', not $GIT_SHA; refusing to publish" >&2
                                        exit 1
                                    fi
                                    if ! git branch --remotes --contains "$GIT_SHA" --format='%(refname:short)' | grep -E -q '^origin/(main|release/.+)$'; then
                                        echo "$GIT_SHA is on neither origin/main nor an origin/release/* branch; refusing to publish $tag" >&2
                                        exit 1
                                    fi
                                    ;;
                            esac
                        '''
                        script {
                            // Same stamp as `make build`: the release tag itself,
                            // or git describe's nearest-tag-plus-commit for main.
                            env.VERSION = params.GIT_REF.startsWith('refs/tags/')
                                ? params.GIT_REF.replaceFirst('^refs/tags/', '')
                                : sh(script: 'git describe --tags --always', returnStdout: true).trim()
                            env.CREATED = sh(script: 'date -u +%Y-%m-%dT%H:%M:%SZ', returnStdout: true).trim()
                        }
                    }
                }

                stage('Build') {
                    steps {
                        // .git is excluded from the build context, so the
                        // version arrives as a build arg. --pull makes every
                        // build start from the current, patched base images.
                        // --load puts the result in the daemon's image store
                        // even when the default buildx builder is not the
                        // docker driver, because the smoke test and push need
                        // it there.
                        sh '''
                            set -eu
                            docker build --pull --load \
                                --build-arg VERSION="$VERSION" \
                                --label org.opencontainers.image.revision="$GIT_SHA" \
                                --label org.opencontainers.image.created="$CREATED" \
                                --tag "$LOCAL_IMAGE" \
                                .
                        '''
                    }
                }

                stage('Smoke test') {
                    steps {
                        // Distroless has no shell and no curl, so the probes
                        // run from a curl container that shares the app's
                        // network namespace. No host port is published, so
                        // builds on a shared agent cannot collide.
                        //
                        // The app runs as it would in production: DEV_MODE off,
                        // real-length secrets, and a read-only root filesystem.
                        // The sqlite file goes to a tmpfs /tmp, the one place
                        // uid 65532 can write. /api/v1/message reads a row that
                        // migration 2 seeds, so it proves migrations ran.
                        sh '''
                            set -eu
                            # 48 random hex characters: Validate wants 32 or more,
                            # and the two JWT secrets must differ. Jenkins traces
                            # sh steps with -x, so tracing is off while they are
                            # generated. They are throwaway, but a secret in a log
                            # is a habit worth never forming.
                            set +x
                            secret() { od -An -tx1 -N24 /dev/urandom | tr -d '[:space:]'; }
                            JWT_ACCESS_SECRET=$(secret)
                            JWT_REFRESH_SECRET=$(secret)
                            CSRF_SECRET=$(secret)
                            export JWT_ACCESS_SECRET JWT_REFRESH_SECRET CSRF_SECRET
                            set -x

                            docker run --detach --name "$SMOKE_CONTAINER" \
                                --read-only --tmpfs /tmp \
                                -e JWT_ACCESS_SECRET -e JWT_REFRESH_SECRET -e CSRF_SECRET \
                                -e DATABASE_DSN=/tmp/smoke.db \
                                "$LOCAL_IMAGE" >/dev/null

                            fail() {
                                echo "Smoke test failed: $1" >&2
                                echo "---- container logs ----" >&2
                                docker logs "$SMOKE_CONTAINER" >&2 || true
                                exit 1
                            }
                            probe() {
                                docker run --rm --network "container:$SMOKE_CONTAINER" "$CURL_IMAGE" \
                                    --fail --silent --show-error --max-time 5 \
                                    --retry 30 --retry-delay 1 --retry-connrefused \
                                    "http://127.0.0.1:8080$1"
                            }
                            expect_success() {
                                case "$2" in
                                    *'"success":true'*) echo "GET $1 ok" ;;
                                    *) fail "GET $1 answered: $2" ;;
                                esac
                            }

                            body=$(probe /api/health/live) || fail "GET /api/health/live"
                            expect_success /api/health/live "$body"
                            body=$(probe /api/health/ready) || fail "GET /api/health/ready"
                            expect_success /api/health/ready "$body"
                            body=$(probe /api/v1/message) || fail "GET /api/v1/message"
                            expect_success /api/v1/message "$body"
                            # A missing row still answers success, with "data":null.
                            case "$body" in
                                *'"content":"'*) ;;
                                *) fail "GET /api/v1/message returned no seeded message: $body" ;;
                            esac

                            # The version has to survive -ldflags into the binary.
                            if ! docker logs "$SMOKE_CONTAINER" 2>&1 | grep -F -q '"version":"'"$VERSION"'"'; then
                                fail "the startup log does not report version $VERSION"
                            fi
                            echo "Binary reports version $VERSION"
                        '''
                    }
                }

                stage('Push') {
                    when {
                        expression { !params.DRY_RUN }
                    }
                    steps {
                        withCredentials([usernamePassword(credentialsId: env.GHCR_CREDENTIALS_ID,
                                                          usernameVariable: 'GHCR_USER',
                                                          passwordVariable: 'GHCR_TOKEN')]) {
                            // The login goes to a per-build DOCKER_CONFIG, not the
                            // agent user's ~/.docker, so the token does not
                            // outlive this build or leak to other jobs on the agent.
                            // An empty config dir also drops the CLI's current
                            // context, so the daemon endpoint is pinned first.
                            // Without that, an agent on a non-default context
                            // (rootless, ssh://) would push from the wrong daemon.
                            //
                            // Every build pushes sha-<short>. It always names a
                            // build of that commit, but a rebuild, or a release
                            // tag on a commit main already built, re-pushes it
                            // with a new digest. Pin the printed digest when an
                            // exact image matters. The floating tags only move
                            // forward:
                            //   :main   only if this commit is still main's tip,
                            //           since CI runs can finish out of order;
                            //   :X.Y    only for the newest release in X.Y;
                            //   :latest only for the newest release overall.
                            // A pre-release gets its exact tag and nothing else.
                            sh '''
                                set -eu
                                DOCKER_HOST="${DOCKER_HOST:-$(docker context inspect --format '{{.Endpoints.docker.Host}}')}"
                                export DOCKER_HOST
                                export DOCKER_CONFIG="${WORKSPACE_TMP:-$WORKSPACE@tmp}/docker-ghcr"
                                mkdir -p "$DOCKER_CONFIG"
                                printf '%s' "$GHCR_TOKEN" | docker login ghcr.io --username "$GHCR_USER" --password-stdin

                                push() {
                                    docker tag "$LOCAL_IMAGE" "$IMAGE:$1"
                                    docker push "$IMAGE:$1"
                                    echo "Pushed $IMAGE:$1"
                                }
                                newest_release() {
                                    git tag --list "$1" --sort=-v:refname | grep -E '^v[0-9]+[.][0-9]+[.][0-9]+$' | head -n 1
                                }

                                push "sha-$SHORT_SHA"
                                case "$GIT_REF" in
                                    refs/heads/main)
                                        tip=$(git rev-parse refs/remotes/origin/main)
                                        if [ "$tip" = "$GIT_SHA" ]; then
                                            push main
                                        else
                                            echo "origin/main has moved on to $tip; leaving :main where it is"
                                        fi
                                        ;;
                                    refs/tags/*)
                                        tag=${GIT_REF#refs/tags/}
                                        version=${tag#v}
                                        push "$version"
                                        case "$version" in
                                            *-*)
                                                echo "$tag is a pre-release; no floating tags"
                                                ;;
                                            *)
                                                minor=${version%.*}
                                                if [ "$(newest_release "v$minor.*")" = "$tag" ]; then
                                                    push "$minor"
                                                else
                                                    echo "A newer v$minor.x release exists; leaving :$minor where it is"
                                                fi
                                                if [ "$(newest_release 'v*')" = "$tag" ]; then
                                                    push latest
                                                else
                                                    echo "A newer release exists; leaving :latest where it is"
                                                fi
                                                ;;
                                        esac
                                        ;;
                                esac

                                docker image inspect --format '{{range .RepoDigests}}{{println .}}{{end}}' "$LOCAL_IMAGE"
                                docker logout ghcr.io
                            '''
                        }
                    }
                }
            }
            post {
                always {
                    // Every tag this build created points at one image ID, so
                    // removing by ID clears them all. Base-image layers stay
                    // cached for the next build.
                    sh '''
                        docker rm --force "$SMOKE_CONTAINER" >/dev/null 2>&1 || true
                        id=$(docker image inspect --format '{{.Id}}' "$LOCAL_IMAGE" 2>/dev/null || true)
                        if [ -n "$id" ]; then docker image rm --force "$id" >/dev/null 2>&1 || true; fi
                        rm -rf "${WORKSPACE_TMP:-$WORKSPACE@tmp}/docker-ghcr"
                    '''
                }
            }
        }
    }
}
