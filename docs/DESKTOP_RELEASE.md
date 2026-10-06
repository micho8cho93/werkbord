# Releasing the Mac app: what only the owner can do

Everything an agent could build is built and tested ([DESKTOP.md](DESKTOP.md)). What is left needs **your** Apple account
and **your** GitHub settings, and an agent cannot do it for you: Apple has to know who you are, and the secrets have to be
yours. This is the whole list, in order, with what each step unlocks. Nothing here can be skipped silently: a release
fails, naming the secret that is missing, rather than publish something unsigned.

Until step 8 is done, the repository releases the command-line program as it always did, and the Mac app is built and tested
but not published.

| Step | You do | It unlocks |
| --- | --- | --- |
| 1 | Enrol in the Apple Developer Program | everything below |
| 2 | Create a *Developer ID Application* certificate | signing |
| 3 | Export it as a `.p12` | signing in CI |
| 4 | Create an App Store Connect API key | notarization |
| 5 | Make the `desktop-release` environment and add the Apple secrets | the signed, notarized disk image |
| 6 | Make the update-signing key; commit the public half, store the private half | the app updating itself |
| 7 | Try it: a dry run | knowing it works before a real tag |
| 8 | Release for real, and check it | the first published app |
| 9 | If something goes wrong | the way back |

## Test the app while approval is pending

`make desktop-preview` builds a universal DMG from the current individual VERSION with an ad-hoc signature and no notarization or Sparkle updater.
It writes `dist/desktop-preview/Werkbord-preview.dmg` and its SHA-256 checksum, alongside the versioned disk image.
This target is for an explicitly requested test preview. Publish it only on a product prerelease (for example
`werkbord-v1.3.1-preview.1`), marked prerelease and `--latest=false`, with notes naming the lack of notarization.
Never upload it as `Werkbord.dmg`, never make an appcast for it, and never use it to satisfy the signed release verifier.
The signed release workflow and its requirements remain in force.

The website can link directly to the preview while no signed installer is available. On first launch, a tester may need
**System Settings → Privacy & Security → Open Anyway** after attempting to open the app from Applications.
See [Apple’s instructions](https://support.apple.com/en-us/102445). Replace the preview by dragging the signed app to Applications when it is available.

## 1. Enrol in the Apple Developer Program

<https://developer.apple.com/programs/enroll/> → sign in with your Apple ID → enrol (an individual or an organization; US$99
a year). Approval can take a day or two. You need the **Account Holder** role to make the certificate in step 2.

Note your **Team ID**: <https://developer.apple.com/account> → **Membership details** → *Team ID* (ten letters and digits).

## 2. Create the "Developer ID Application" certificate

On a Mac, with your account holder's Apple ID:

1. **Keychain Access** → menu **Keychain Access → Certificate Assistant → Request a Certificate From a Certificate Authority…**
   Your email, a common name (say "Werkbord signing"), *Saved to disk*. This writes `CertificateSigningRequest.certSigningRequest`
   and, in your login keychain, the **private key** the certificate will belong to. Do this on the Mac you will export from.
2. <https://developer.apple.com/account/resources/certificates/add> → choose **Developer ID Application** (not
   "Developer ID Installer", not "Apple Development") → *G2 Sub-CA* if asked → upload the `.certSigningRequest` → **Download**
   `developerID_application.cer`.
3. Double-click the `.cer`. It goes into your login keychain, next to the private key from step 1. In **My Certificates** it is
   "Developer ID Application: Your Name (TEAMID)", with a small arrow that opens to show the private key beneath it. If there
   is no key under it, you made the request on another Mac.

## 3. Export it as a `.p12`

1. In **Keychain Access → My Certificates**, select **the certificate** ("Developer ID Application: …", not the key below it).
2. **File → Export Items…** → format **Personal Information Exchange (.p12)** → save as `werkbord-signing.p12`.
3. Choose a long random password (a password manager's). You will need it in step 5. Remember it is the only thing that
   protects the file.
4. Make it base64, one line, for the secret: `base64 -i werkbord-signing.p12 | tr -d '\n' | pbcopy` (it is now on the clipboard).
5. **Then delete `werkbord-signing.p12` from disk** (and empty the Trash) once the secret is saved. Keep the certificate and
   its key in your keychain, and a backup of the `.p12` in your password manager's secure storage: if the private key is lost
   it cannot be recovered, and you would have to make a new certificate (step 2), which costs nothing but changes who signs
   future updates to Apple's eyes (not to Sparkle's: that is step 6).

## 4. Create the App Store Connect API key (for notarization)

Notarization uses an API key, not your Apple ID password.

1. <https://appstoreconnect.apple.com/access/integrations/api> → **Team Keys** → **Generate API Key** (or the **+**). If it asks
   you to *Request Access*, do (the Account Holder can).
2. Name: "Werkbord notarization". Access: **Developer**. Generate.
3. **Download API Key** once (`AuthKey_XXXXXXXXXX.p8`): Apple will not give it to you again. Keep it like the `.p12`.
4. On the same page copy **Issuer ID** (a UUID, above the table) and the key's **Key ID** (ten letters and digits, also in
   the file name).

## 5. Make the GitHub environment and add the Apple secrets

Secrets live in an **environment**, which only the macOS signing job uses and which can require your approval:

1. <https://github.com/micho8cho93/werkbord/settings/environments> → **New environment** → name it exactly **`desktop-release`**.
2. In it: **Required reviewers** → add yourself (a release then waits for your click before it can use a secret).
   **Deployment branches and tags** → *Selected branches and tags* → add `main` and the tag pattern `werkbord-v*`. (So a
   branch someone pushed cannot reach the secrets, and a dry run from `main` can.)
3. **Environment secrets → Add secret**, exactly these names:

| Secret | What to paste |
| --- | --- |
| `APPLE_CERTIFICATE_P12` | the one-line base64 of the `.p12` (step 3, item 4) |
| `APPLE_CERTIFICATE_PASSWORD` | the password you chose in step 3 |
| `APPLE_NOTARY_KEY` | the **whole text** of `AuthKey_XXXXXXXXXX.p8`, including the `-----BEGIN PRIVATE KEY-----` lines |
| `APPLE_NOTARY_KEY_ID` | the Key ID (step 4) |
| `APPLE_NOTARY_ISSUER` | the Issuer ID (step 4) |

   (`APPLE_SIGNING_IDENTITY` is optional: only if the `.p12` holds more than one "Developer ID Application" identity, the
   full name, "Developer ID Application: Your Name (TEAMID)".)

Check them without building anything: the dry run (step 7) starts with `scripts/check-release-secrets.sh`, which names what is
missing and what looks like the wrong thing (a `.p12` that was not base64-encoded, a key that is not the `.p8`'s text).

## 6. Make the update-signing key

This is a different key from Apple's. It is what the *installed app* uses to decide whether to trust an update. Whoever holds
the private half can ship code to every installed Werkbord, so treat it like the certificate, and never put it in the repository.

1. Get Sparkle's tool (the version the repository pins is fetched and checked for you):
   ```bash
   scripts/fetch-sparkle.sh        # prints the directory it is in, e.g. .cache/sparkle/2.10.0
   ```
2. Make the key pair (Sparkle's tool keeps it in your login keychain, where macOS may ask you to allow it) and export the
   private half to a file:
   ```bash
   SPARKLE=$(scripts/fetch-sparkle.sh)
   "$SPARKLE/bin/generate_keys"                              # prints the PUBLIC key: copy it
   "$SPARKLE/bin/generate_keys" -x /tmp/werkbord-sparkle-private.key   # the PRIVATE key: one line of base64
   ```
3. **Public half:** put the printed key (one line, 44 characters) in `desktop/build/darwin/sparkle-public-key` and commit it.
   It is public by design: it is inside every copy of the app.
4. **Private half:** `pbcopy < /tmp/werkbord-sparkle-private.key` and add it as the environment secret
   **`SPARKLE_ED_PRIVATE_KEY`**. Then store it in your password manager as well, and **delete the file**:
   `rm /tmp/werkbord-sparkle-private.key` (the key also stays in your login keychain, which is one more backup). If you lose it, installed apps can never be updated again by this key (they would
   need a new disk image, installed by hand, carrying a new public key); if it leaks, anyone can sign an update they would accept.
   There is no way to rotate it without a new disk image, so back it up on purpose.

## 7. Try it: a dry run

Merge to `main` first (a manual run uses the workflow file on the default branch), then:

<https://github.com/micho8cho93/werkbord/actions/workflows/release.yml> → **Run workflow** → branch `main` → **Run workflow**.
Or: `gh workflow run release.yml --ref main`. Approve it when it asks for the `desktop-release` environment.

It checks the secrets, builds the universal app, signs it, has Apple notarize it (this takes five to fifteen minutes the first
time), staples it, checks it the way a stranger's Mac would, makes the update archive and the signed feed, and **publishes
nothing**: the files are in the run's *Artifacts* (`desktop-release`, `desktop-appcast`) for 14 days. A dry run builds whatever
version `cmd/werkbord/VERSION` says; it does not need a tag.

If it fails, the message says which step and why. The usual ones: the secret is missing or mis-pasted (step 5), Apple rejected
a file (the log it prints names it), the certificate is not a *Developer ID Application* one (step 2).

Download the artifact and open the disk image on a Mac that has never seen Werkbord if you can: it should show the usual
"downloaded from the Internet" question, once, and open.

## 8. Release for real, and check it

When `cmd/werkbord/VERSION` says what you want to ship (the repository's [versioning policy](VERSIONING.md) has done that,
and tagged the commit locally), push the tag, and nothing else:

```bash
git push origin main
git push origin werkbord-v1.2.1          # the tag of the version in cmd/werkbord/VERSION; this starts the release
```

CI publishes the command-line archives first (as it always did), and then, only for an individual stable tag, builds and
publishes the disk image, checks it on a fresh Mac runner, and last of all publishes the feed (`appcast.xml`) that tells installed
apps about it. Approve the environment when asked.

Then check it once yourself, by hand, which is the only check no CI run can replace:

```bash
scripts/verify-desktop-release.sh werkbord-v1.2.1     # downloads it as a user would; every line should say ok
scripts/verify-appcast.sh werkbord-v1.2.1             # the feed, against the published update archive
```

Then delete the "Not published yet" note from the top of the README's *Install* section (and the same sentence on the website's
install block, if it was added: the website's button already checks for itself whether a release carries the app).

and: on a Mac that is not yours (or a new user account), download `Werkbord_1.2.1_darwin_universal.dmg` **in Safari** from the
[release page](https://github.com/micho8cho93/werkbord/releases/latest), open it, drag Werkbord to Applications, and open it.
Expected: the one standard question and then the window, no "cannot be opened", no "damaged". Later, on a Mac with the previous
version, choose **Werkbord → Check for Updates…**: Sparkle's window should offer the new version.

## 9. If something goes wrong

**Apple rejects the disk image** (the build fails in `notarize-desktop.sh`, printing Apple's log). Nothing was published. The log
names the file and the reason ("The signature of the binary is invalid", "The executable does not have the hardened runtime
enabled", "The binary uses an SDK older than 10.9"…). Fix it in the repository and release the next patch version. The command-line
release that was already made stays as it is, and `werkbord update` works on it.

**A published disk image is bad** (the `desktop-verify` job went red). The CLI release is untouched. Withdraw the image so
nobody downloads it, and the feed was not published (it comes after), so installed apps were never offered it:

```bash
gh release delete-asset werkbord-v1.2.1 Werkbord_1.2.1_darwin_universal.dmg -y
gh release delete-asset werkbord-v1.2.1 Werkbord_1.2.1_darwin_universal.dmg.sha256 -y
gh release delete-asset werkbord-v1.2.1 Werkbord_1.2.1_darwin_universal.zip -y
gh release delete-asset werkbord-v1.2.1 Werkbord_1.2.1_darwin_universal.zip.sha256 -y
```

then fix, bump the patch version, and release that. Never re-upload different bytes under the same tag's file names.

**A bad feed went out** (an update that fails on people's Macs). Installed apps ask the feed only when a person clicks **Check for
Updates…** or **Update now**, so the damage is limited to people who do that. Remove it at once: apps then find no feed ("up to
date" or "could not check"), which is harmless:

```bash
gh release delete-asset werkbord-v1.2.1 appcast.xml -y
```

Then release a fixed version; its feed replaces the old one. (Sparkle never installs a version lower than the one running, so a
bad update cannot be "rolled back" by publishing an older feed: publish a higher patch version with the fix.) The controller
program is never part of this: it is replaced only by its own installer, which refuses to start while agents work, takes a
database snapshot first, and puts the old program back if the new one does not start.

**The update-signing key leaked.** Treat it as the worst case: delete the feed (above) so nothing is offered, make a new key
(step 6), publish a new disk image carrying its public key, and tell people to install it by hand once: installed apps cannot
be moved to a new key by an update signed with the old one on their behalf.

**The certificate expired or was revoked.** Developer ID certificates last five years. Make a new one (step 2), export it
(step 3) and replace `APPLE_CERTIFICATE_P12` and `APPLE_CERTIFICATE_PASSWORD`. Already-notarized apps keep opening; Sparkle
keeps working because it trusts the update key and the app's Apple signature's *team*, which does not change.

## Decisions this release process rests on

**One universal disk image, not two.** A person should not have to know their chip, and a wrong choice gives an app that
does not open. The window and the program are each built for Apple Silicon and Intel and joined with `lipo`; the system runs
the half that fits. Measured on the 1.1.x tree: the program is 71 MB universal (36 MB a chip), the window 22 MB, the app 90 MB,
the disk image 39 MB (about twice a single-chip image, which is the whole price). The alternative, two images, would also mean
two notarizations, two update archives and two feeds or a hardware-requirement rule in the feed, for a saving of about 20 MB per
download. Both halves were run: the signing test starts the signed app natively and as an Intel process under Rosetta.

**The program is not thinned when the app installs it.** The copy in `~/.local/bin` is therefore the universal one (+35 MB on disk)
until the first `werkbord update` from a terminal, which replaces it with the thin release archive. Thinning would put a
transformation of a signed executable on the two paths that must never fail (the first install, and `install-release`), for a
saving that does not change how Werkbord runs: the system maps only the slice it needs. If it ever matters, do it in Go with
`debug/macho` where the program is copied (a slice keeps its own signature), not with `lipo`, which a person without the
developer tools does not have.

**One update archive and one feed per release, from the individual release only.** The feed is `releases/latest/download/appcast.xml`
because only individual stable releases are marked "latest" (`release.yml`; a test pins it), so a Team release cannot become the
feed, and each release's feed holds that release alone, so an older item can never be offered again.

**The feed is published last.** A bad disk image is caught on a fresh Mac before any installed app can be told about it.

**Sparkle has no schedule.** Werkbord is local-first and already makes one request on its own (the controller's, which has an
off switch). A second, automatic request to the same server would be a second thing to switch off and to explain; the banner
already tells a person an update exists, and Sparkle is what that banner's button opens. The cost: someone who has turned the
banner off (or never opens the window) only learns of an update when they choose **Check for Updates…**.
