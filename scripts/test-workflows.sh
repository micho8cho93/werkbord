#!/bin/sh
# Checks the GitHub workflows: that they are YAML, and the properties the Mac app's release relies on
# for its secrets to be safe. It needs Ruby (on every macOS and Ubuntu runner, and on a Mac).
#
#   scripts/test-workflows.sh
#
#  - every workflow parses;
#  - nothing that a pull request or a fork starts (ci.yml, anything triggered by pull_request or
#    pull_request_target) can reach a secret or an environment;
#  - secrets are named in exactly one job of release.yml (desktop-build), which cannot write to the release;
#  - a job that can write to the repository (contents: write) holds no secret and no environment;
#  - the jobs that handle the Mac app use only actions pinned to a full commit, and nothing third-party;
#  - the Mac app is only for the individual product's tags: the job chain has the plan that excludes Team's
#    and pre-releases', desktop-build needs `release`, and publishing happens only on a tag push, never on a dry run;
#  - the existing release job still creates the release with --latest for the individual product only.
set -eu
cd "$(dirname "$0")/.."
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
def perms(job, w); job["permissions"] || w["permissions"]; end

workflows.each do |path, w|
  next unless w
  names = trigger_names(w)
  from_outside = names.include?("pull_request") || names.include?("pull_request_target")
  if from_outside
    check(!mentions_secret?(w), "#{path} is started by pull requests and must not mention a secret")
    check(!JSON.generate(w).include?("environment"), "#{path} is started by pull requests and must not use an environment")
    check(!names.include?("pull_request_target"), "#{path} uses pull_request_target, which runs with a fork's code and the repository's token")
  end
  w["jobs"].each do |name, job|
    p = perms(job, w)
    writes = p.is_a?(Hash) && p.values.include?("write")
    if writes
      check(!mentions_secret?(job), "#{path}: job #{name} can write to the repository and mentions a secret")
      check(!job.key?("environment"), "#{path}: job #{name} can write to the repository and is in an environment (which has secrets)")
    end
  end
end

ci = workflows["#{DIR}/ci.yml"]
check(ci, "ci.yml is missing")

rel = workflows["#{DIR}/release.yml"]
check(rel, "release.yml is missing")
if rel
  names = trigger_names(rel)
  check(names.include?("push") && names.include?("workflow_dispatch"), "release.yml runs on a tag push and, for the dry run, workflow_dispatch")
  check(!names.include?("pull_request") && !names.include?("pull_request_target"), "release.yml must not be started by a pull request")
  tags = rel.dig(true, "push", "tags") || rel.dig("on", "push", "tags") || []
  check(!tags.empty? && tags.all? { |t| t.start_with?("werkbord-v") }, "release.yml's tags are the release's tags (werkbord-v...): there is no Team tag series")
  check(perms({}, rel) == { "contents" => "read" }, "release.yml's default permissions are contents: read")

  jobs = rel["jobs"]
  %w[plan release desktop-build desktop-publish desktop-verify desktop-appcast].each { |j| check(jobs.key?(j), "release.yml has no job #{j}") }

  # secrets: one job, and it is read-only
  with_secrets = jobs.select { |_, j| mentions_secret?(j) }.keys
  check(with_secrets == ["desktop-build"], "secrets are used in #{with_secrets.inspect}; they belong in desktop-build alone")
  build = jobs["desktop-build"]
  if build
    check(perms(build, rel) == { "contents" => "read" }, "desktop-build (which holds the secrets) must have contents: read only")
    check(build["environment"] == "desktop-release", "desktop-build uses the desktop-release environment")
    check(Array(build["needs"]).include?("release"), "desktop-build needs the release job")
    check(Array(build["needs"]).include?("plan"), "desktop-build needs plan, which keeps pre-releases out")
    check(build.dig("env", "LICENSE_ISSUER_PUBLIC_KEY").to_s.include?("secrets.TEAM_LICENSE_ISSUER_PUBLIC_KEY"), "desktop-build builds Team's installer, so it needs the license issuer's public key")
    check(JSON.generate(build["steps"]).include?("scripts/check-release-secrets.sh desktop"), "desktop-build checks every release secret before it builds")
    check(build["runs-on"].to_s.start_with?("macos"), "desktop-build runs on macOS")
    check(build["if"].to_s.include?("plan.outputs.desktop"), "desktop-build runs only when plan says so")
    steps = JSON.generate(build["steps"])
    check(steps.include?("ci-keychain.sh create") && steps.include?("ci-keychain.sh delete"), "desktop-build imports the certificate into a temporary keychain and deletes it")
    check(build["steps"].any? { |s| s["name"].to_s.start_with?("Remove the certificate") && s["if"].to_s == "always()" }, "the keychain is deleted in a step that always runs")
    check(!steps.include?("gh release"), "desktop-build must not touch the release")
  end
  %w[desktop-publish desktop-verify desktop-appcast].each do |n|
    j = jobs[n] or next
    check(j["if"].to_s.include?("push"), "#{n} runs only on a tag push, never on the dry run")
    check(!j.key?("environment"), "#{n} holds no secrets and needs no environment")
  end
  check(jobs.dig("desktop-publish", "permissions") == { "contents" => "write" }, "desktop-publish is what writes to the release")
  check(jobs.dig("desktop-verify", "runs-on").to_s.start_with?("macos"), "the verification runs on a macOS runner of its own")
  check(Array(jobs.dig("desktop-verify", "needs")).include?("desktop-publish"), "the verification runs after the upload")
  check(jobs.dig("desktop-appcast", "permissions") == { "contents" => "write" }, "desktop-appcast is what writes the feed to the release")
  check(Array(jobs.dig("desktop-appcast", "needs")).include?("desktop-verify"), "the feed is published only after the disk image has been verified")
  # the feed is the only thing a job may delete, and it is the job's own
  deletes = jobs.flat_map { |n, j| (j["steps"] || []).map { |st| [n, st["run"].to_s] } }.select { |_, r| r.include?("delete-asset") }
  check(deletes.map(&:first).uniq.all? { |n| n == "desktop-appcast" || n == "desktop-verify" }, "only the feed's own job (and the message of the verification) mention deleting an asset")
  check(deletes.select { |n, _| n == "desktop-appcast" }.all? { |_, r| r.scan(/delete-asset "\$GITHUB_REF_NAME" (\S+)/).flatten.uniq == ["appcast.xml"] }, "desktop-appcast deletes only appcast.xml")

  # pinned actions in the jobs that handle the app
  %w[desktop-build desktop-publish desktop-verify desktop-appcast].each do |n|
    j = jobs[n] or next
    uses_of(j).each do |u|
      check(u.match?(%r{\A(actions/[a-z-]+)@[0-9a-f]{40}\z}), "#{n} uses #{u}: only actions/* pinned to a full commit are allowed here")
    end
  end

  # the existing release job is as it was
  r = jobs["release"]
  if r
    check(r["if"].to_s == "github.event_name == 'push' && startsWith(github.ref_name, 'werkbord-v')", "the online release job publishes only the controller's archives; Team's archives are signed offline")
    check(perms(r, rel) == { "contents" => "write" }, "the release job creates the release, so it can write")
    cmd = JSON.generate(r["steps"])
    check(cmd.include?("gh release create") && cmd.include?("--latest=false") && cmd.include?("--latest"), "the release job still marks only the individual product's releases latest")
    check(cmd.include?("make dist PRODUCT="), "the release job still builds the archives with make dist")
    check(!jobs.values.any? { |j| (j["steps"] || []).any? { |st| st["run"].to_s.match?(/gh release (edit|delete)( |$)/) } }, "no job deletes or edits the release")
  end
  # nothing but the new jobs uploads, and what they upload is the disk image's two files
  uploads = jobs.flat_map { |n, j| (j["steps"] || []).map { |s| [n, s["run"].to_s] } }.select { |_, run| run.include?("gh release upload") }
  check(uploads.map(&:first).uniq.all? { |n| %w[release desktop-publish desktop-appcast].include?(n) }, "only the release and desktop publishing jobs upload to the release")
  uploads.each do |n, run|
    if n == "release"
      check(run.include?('--prerelease --latest=false') && run.include?('--json isPrerelease --jq .isPrerelease') && run.include?('gh release upload "$GITHUB_REF_NAME" dist/* --clobber'), "the release job may reuse only an existing prerelease and upload only its CLI archives")
    else
      check(run.include?("darwin_universal") || run.include?("Werkbord.dmg") || run.include?("appcast.xml"), "#{n} uploads something other than the disk image, its update archive or the appcast")
    end
  end
  check(JSON.generate(jobs.dig("release", "steps")).include?('--prerelease --latest=false'), "prereleases never replace the latest stable release")
  check(jobs.dig("desktop-publish", "steps").to_a.none? { |st| st["run"].to_s.include?("appcast.xml") }, "the feed is not published together with the disk image: it comes after the check")
end

# There is one release workflow. A second one for Team, its own protected environment, and the fetching of a separately
# released Team installer are exactly what the unification removed.
check(!workflows.key?("#{DIR}/release-team-desktop.yml"), "there is no separate Team release workflow")
if rel
  all = JSON.generate(rel)
  check(!all.include?("prepare-unified-team") && !all.include?("TEAM_RELEASE_PUBLIC_KEY") && !all.include?("TEAM_DESKTOP_APP"), "the release builds Team's installer itself and fetches no separately released one")
  check(!all.include?("werkbord-team-v"), "release.yml names no Team tag series")
end

if $failures.empty?
  puts "test-workflows: #{workflows.size} workflows parse, and their secrets, permissions and pins are as designed"
else
  $failures.each { |f| warn "FAIL  #{f}" }
  exit 1
end
RUBY
