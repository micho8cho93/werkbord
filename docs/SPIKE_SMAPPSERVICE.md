# Spike: a Team service registered with Apple's own mechanism (SMAppService)

Throwaway. It answers the questions that decide whether Team's background service can move from "installed by an osascript
administrator prompt" ([TEAM_DESKTOP.md](TEAM_DESKTOP.md)) to a daemon the person approves in System Settings → Login Items, which
launchd then runs from inside the signed app. Nothing in the product uses it. The kit is `scripts/spike-smappservice/`: a stand-in app
(`spike`), a stand-in service (`spiked`, which mirrors how the real service keeps its sealing key in the System keychain) and `spike.sh`.
Nothing needs `sudo`. The service listens on `127.0.0.1:47633` and answers `/state`, `/exit` and `/cleanup` to anyone on the computer, so
**do the clean-up below when you are done.**

## Questions

1. **Does registering work, and what does the person see?** (`status` after `register`: `requiresApproval`, then `enabled` once approved.)
2. **Does it run as root from inside the app?** (`/state`: `euid` 0 and `exe` inside `Spike.app`.)
3. **After the app is replaced, which program runs?** (the old process keeps running; after `/exit`, launchd should start the new one.)
4. **Is the approval kept across the swap, or asked again?** (`status` after the swap.)
5. **Can the sealing key still be read by the new program?** (`keychainReadable` in `/state`, and `keychainFind`: 0 is fine; -25293 or -25308 means
   the keychain item is tied to the program that made it and the new one is refused.) *Only a Developer ID build answers this: an ad hoc
   build has a different identity at every build.*
6. **The one that decides it: can a program running as the person replace the service's binary in a user-owned app, and does launchd then run
   it as root?** The attacker is modelled by an ad hoc build named `evil`, which cannot carry the release signature.

## Procedure (a Terminal, in `scripts/spike-smappservice/`)

```sh
# Questions 5 needs the release identity in your keychain; without it, leave SIGN_IDENTITY out (ad hoc) and skip that question.
export SIGN_IDENTITY="Developer ID Application: <your name> (<TEAMID>)"
./spike.sh build v1; ./spike.sh build v2
SIGN_IDENTITY=- ./spike.sh build evil          # always ad hoc: the attacker has no release key

./spike.sh install v1                          # into ~/Applications/WerkbordSpike, owned by you, as a dragged-in app is
./spike.sh register                            # 1: expect requiresApproval (a notification appears)
./spike.sh settings                            # approve "Spike" in System Settings → Login Items
./spike.sh status                              # 1: enabled
./spike.sh state                               # 2: euid 0, exe inside the bundle, keychainAdd 0, keychainReadable true

./spike.sh install v2                          # 3, 4: the swap an update makes
./spike.sh status                              # 4: still enabled, or requiresApproval again?
./spike.sh state                               # 3: still v1 (the old process)
./spike.sh exit; sleep 6; ./spike.sh state     # 3: v2? 5: keychainReadable, keychainFind

# 6: a program running as you replaces the service binary, then the service restarts.
cp /tmp/werkbord-spike/evil/Spike.app/Contents/MacOS/spiked ~/Applications/WerkbordSpike/Spike.app/Contents/MacOS/spiked
./spike.sh exit; sleep 6; ./spike.sh state     # version "evil" with euid 0 = launchd ran the attacker's binary as root
codesign --verify --strict --deep ~/Applications/WerkbordSpike/Spike.app   # it fails: the seal is broken; did launchd care?
```

## What the answers mean

- **6 ends in `evil` as root** (or the `cp` is allowed and launchd does not refuse): SMAppService alone does not fix the swap problem for an
  app the person owns. Stage 2 would then keep the root-owned copy of the programs that stage 1 makes, and use SMAppService only to register
  that daemon (or the helpers must live outside the bundle).
- **6 is refused** (by the `cp`, by macOS's protection of other apps, or by launchd refusing a broken seal): the daemon can run from the
  bundle, and the root-owned copy is unnecessary for the programs.
- **3 stays on v1 until `/exit`**: the new app must ask the running service to restart after an update; the service cannot rely on launchd.
- **5 refused**: the sealing key's keychain items cannot follow a program change; Team's service would need its own migration of the key
  (or an access list that trusts the release's designated requirement) before any move.
- **4 asks again**: every update would send the person back to System Settings, which is no better than today.

## Clean-up

```sh
./spike.sh cleanup        # removes the keychain item
./spike.sh unregister     # stops and removes the service
rm -rf ~/Applications/WerkbordSpike /tmp/werkbord-spike
sudo rm -f /var/tmp/werkbord-spike.log /var/tmp/werkbord-spike.err    # two small root-owned files; harmless if left
```
