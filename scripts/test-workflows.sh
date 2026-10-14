#!/bin/sh
# Check workflow YAML, secret isolation, permissions and the order of stable publication.
set -eu
cd "$(dirname "$0")/.."

version_gt() {
  if scripts/semver-gt.sh "$1" "$2"; then :; else
    echo "test-workflows: expected $1 to be newer than $2" >&2
    exit 1
  fi
}
version_not_gt() {
  if scripts/semver-gt.sh "$1" "$2"; then
    echo "test-workflows: expected $1 not to be newer than $2" >&2
    exit 1
  else
    result=$?
    [ "$result" -eq 1 ] || { echo "test-workflows: invalid version in $1 or $2" >&2; exit 1; }
  fi
}
version_gt 4.24.2 4.24.2-preview.1
version_gt 4.24.2-preview.10 4.24.2-preview.9
version_gt 4.24.2-rc.1 4.24.2-preview.2
version_gt 4.24.3-preview.1 4.24.2
version_not_gt 4.24.2-preview.1 4.24.2
version_not_gt 4.24.2 4.24.2
version_not_gt 4.24.1 4.24.2-preview.1

command -v ruby >/dev/null 2>&1 || { echo "test-workflows: ruby is needed"; exit 1; }
ruby -ryaml -rjson - <<'RUBY'
$failures = []
def fail(msg); $failures << msg; end
def check(cond, msg); cond || fail(msg); end

def load(path)
  YAML.safe_load(File.read(path), aliases: true)
rescue => e
  fail("#{path} is not valid YAML: #{e.message}")
  nil
end

DIR = ENV["WORKFLOW_DIR"] || ".github/workflows"
workflows = Dir["#{DIR}/*.yml"].sort.to_h { |p| [p, load(p)] }
check(!workflows.empty?, "no workflows found")
workflows.each { |p, w| check(w.is_a?(Hash) && w["jobs"].is_a?(Hash), "#{p} has no jobs") }

def triggers(w); w["on"] || w[true] || {}; end
def trigger_names(w)
  t = triggers(w)
  t.is_a?(Hash) ? t.keys : Array(t)
end
def uses_of(job); (job["steps"] || []).map { |s| s["uses"] }.compact; end
def mentions_secret?(obj); JSON.generate(obj).include?("secrets."); end
def perms(job, workflow); job["permissions"] || workflow["permissions"]; end
def needs(job); Array(job["needs"]); end

workflows.each do |path, workflow|
  next unless workflow
  names = trigger_names(workflow)
  from_pull_request = names.include?("pull_request") || names.include?("pull_request_target")
  if from_pull_request
    check(!mentions_secret?(workflow), "#{path} is started by pull requests and must not mention a secret")
    check(!JSON.generate(workflow).include?("environment"), "#{path} is started by pull requests and must not use an environment")
    check(!names.include?("pull_request_target"), "#{path} uses pull_request_target")
  end
  workflow["jobs"].each do |name, job|
    permissions = perms(job, workflow)
    writable = permissions.is_a?(Hash) && permissions.values.include?("write")
    if writable
      check(!mentions_secret?(job), "#{path}: writable job #{name} mentions a secret")
      check(!job.key?("environment"), "#{path}: writable job #{name} uses an environment with secrets")
    end
  end
end

release = workflows["#{DIR}/release.yml"]
check(release, "release.yml is missing")
if release
  names = trigger_names(release)
  check(names.include?("push") && names.include?("workflow_dispatch"), "release.yml supports tag pushes and manual dry runs")
  check(!names.include?("pull_request") && !names.include?("pull_request_target"), "release.yml must not run on pull requests")
  tags = release.dig(true, "push", "tags") || release.dig("on", "push", "tags") || []
  check(!tags.empty? && tags.all? { |tag| tag.start_with?("werkbord-v") }, "release.yml uses only the unified Werkbord tag series")
  check(perms({}, release) == { "contents" => "read" }, "release.yml defaults to contents: read")

  jobs = release["jobs"]
  required = %w[plan release-build preview-publish desktop-build stable-draft stable-publish macos-public-verify desktop-feed-public-verify stable-latest]
  required.each { |name| check(jobs.key?(name), "release.yml has no job #{name}") }

  %w[linux-desktop-build windows-desktop-build linux-public-verify windows-public-verify].each { |name| check(!jobs.key?(name), "macOS-only release must not include #{name}") }

  secret_jobs = jobs.select { |_, job| mentions_secret?(job) }.keys.sort
  check(secret_jobs == %w[desktop-build], "release credentials are exposed to #{secret_jobs.inspect}")
  %w[desktop-build].each do |name|
    job = jobs[name] or next
    check(perms(job, release) == { "contents" => "read" }, "#{name} has release credentials and must be read-only")
    check(job["environment"] == "desktop-release", "#{name} uses the protected desktop-release environment")
    check(needs(job).include?("plan") && needs(job).include?("release-build"), "#{name} waits for the plan and release checks")
    check(job["if"].to_s.include?("workflow_dispatch") && job["if"].to_s.include?("release-build.result"), "#{name} is a dry run on dispatch and waits for CI on tags")
    check(JSON.generate(job).include?("scripts/check-release-secrets.sh"), "#{name} checks the credentials it needs before building")
  end

  writers = jobs.select { |_, job| (perms(job, release).is_a?(Hash) && perms(job, release).values.include?("write")) }.keys.sort
  check(writers == %w[preview-publish stable-draft stable-latest stable-publish], "only the release publisher jobs can write: #{writers.inspect}")
  writers.each { |name| check(!mentions_secret?(jobs[name]) && !jobs[name].key?("environment"), "writable job #{name} must not hold release credentials") }

  release_build = jobs["release-build"]
  if release_build
    steps = JSON.generate(release_build["steps"])
    check(perms(release_build, release) == { "contents" => "read" }, "release-build is read-only")
    gate_index = release_build["steps"].index { |step| step["run"].to_s.include?("check-team-production-gate.sh") }
    checks_index = release_build["steps"].index { |step| step["run"].to_s.include?("make check") }
    check(gate_index && checks_index && gate_index < checks_index, "stable Team approval is checked before release builds and repository tests")
    gate_condition = release_build["steps"].dig(gate_index || 0, "if").to_s
    check(gate_condition.include?("github.event_name == 'push'") && gate_condition.include?("!contains"), "the Team production gate applies only to stable tag pushes")
    check(steps.include?("make check") && steps.include?("make test-nebula") && steps.include?("make test-rqlite"), "release-build runs repository, native network and real-cluster checks")
    check(steps.include?('make dist PRODUCT=werkbord PLATFORMS=\"darwin/arm64 darwin/amd64\"'), "controller publication includes only macOS architectures")
    check(!steps.include?("gh release"), "release-build does not create a public release before platform builds finish")
    check(!JSON.generate(release).include?("make dist PRODUCT=werkbord-team"), "standalone Team Host archives are never built for publication")
  end

  stable_draft = jobs["stable-draft"]
  if stable_draft
    check(needs(stable_draft).sort == %w[desktop-build plan release-build].sort, "stable draft waits for the Mac desktop build and controller checks")
    check(stable_draft["if"].to_s.include?("stable"), "only a stable tag can create the public release draft")
    check(perms(stable_draft, release) == { "contents" => "write" }, "stable-draft is the release asset publisher")
    steps = JSON.generate(stable_draft["steps"])
    check(steps.include?("Release sequence:") && steps.include?("0.9.0") && steps.include?("Original build version:") && steps.include?("$version"), "stable release notes use the reserved display sequence and tag-derived build version")
    check(steps.include?("--draft --latest=false"), "stable release is drafted without becoming latest")
    check(!steps.include?("assets/linux") && !steps.include?("assets/windows"), "the release gathers only macOS assets")
    check(steps.include?("Standalone Team Host archives must not be published"), "the release rejects standalone Team Host archives")
    check(steps.include?("check-team-production-gate.sh"), "a stable release requires a version-specific Team production approval")
  end

  stable_publish = jobs["stable-publish"]
  check(needs(stable_publish).include?("stable-draft"), "stable-publish follows the assembled draft")
  check(JSON.generate(stable_publish["steps"]).include?("--draft=false") && !JSON.generate(stable_publish["steps"]).include?("--latest"), "stable-publish makes the complete release public but does not promote it yet")

  %w[macos-public-verify desktop-feed-public-verify].each do |name|
    job = jobs[name] or next
    check(needs(job).include?("stable-publish"), "#{name} verifies publicly after the release is visible")
    check(job["if"].to_s.include?("push"), "#{name} never runs during a manual dry run")
  end
  check(JSON.generate(jobs["macos-public-verify"]).include?("verify-desktop-release.sh"), "Mac public verification checks the signed and notarized app")
  latest = jobs["stable-latest"]
  check(needs(latest).sort == %w[desktop-feed-public-verify macos-public-verify].sort, "latest promotion waits for all public checks")
  check(JSON.generate(latest["steps"]).include?("--latest --verify-tag"), "only the final job promotes the verified stable release")
  check(latest["if"].to_s.include?("push"), "manual dry runs never promote a release")

  # Every action is pinned to a full commit SHA, including artifact transport.
  jobs.each do |name, job|
    uses_of(job).each do |use|
      check(use.match?(%r{\Aactions/[a-z-]+@[0-9a-f]{40}\z}), "#{name} uses #{use}; pin actions to full commit SHAs")
    end
  end

  all = JSON.generate(release)
  check(!all.include?("werkbord-team-v"), "release.yml creates no Team tag series")
  check(!all.include?("checksums-team.txt.sig") && !all.include?("make dist PRODUCT=werkbord-team"), "release.yml publishes no standalone Team Host archives")
end

sequence_doc = File.read("docs/RELEASE_SEQUENCE.md")
check(sequence_doc.include?("werkbord-v4.28.2") && sequence_doc.include?("0.9.0") && sequence_doc.include?("Reserved stable"), "the unified release display number is reserved in docs/RELEASE_SEQUENCE.md")

check(!workflows.key?("#{DIR}/release-team-desktop.yml"), "there is no separate Team release workflow")
if $failures.empty?
  puts "test-workflows: #{workflows.size} workflows parse; platform builds, release credentials and stable promotion are gated"
else
  $failures.each { |failure| warn "FAIL  #{failure}" }
  exit 1
end
RUBY
