#!/bin/sh
# Apple's notarytool and stapler, as they behave in the situation $FAKE_MODE names (scripts/test-notarize-desktop.sh,
# scripts/test-desktop-sign.sh). Every call is appended to $FAKE_LOG; $FAKE_COUNT counts submissions; $FAKE_KEY is the key file.
# Modes: accepted invalid invalid-exit1 flaky down unauthorized staple-fails validate-fails gatekeeper-rejects
echo "xcrun $*" >> "$FAKE_LOG"
tool=$1; sub=$2
case "$tool" in
notarytool)
  case "$sub" in
  submit)
    n=$(cat "$FAKE_COUNT" 2>/dev/null || echo 0); n=$((n + 1)); echo $n > "$FAKE_COUNT"
    case "$FAKE_MODE" in
    accepted|staple-fails|validate-fails|gatekeeper-rejects) printf '{\n  "id" : "aaaaaaaa-1111-2222-3333-444444444444",\n  "message" : "Successfully received submission info",\n  "status" : "Accepted"\n}\n' ;;
    invalid) printf '{"id":"bbbbbbbb-1111-2222-3333-444444444444","message":"Processing complete","status":"Invalid"}\n' ;;
    invalid-exit1) printf '{"id":"bbbbbbbb-1111-2222-3333-444444444444","status":"Invalid"}\n'; exit 1 ;;
    flaky) # the network fails twice, then Apple answers
      if [ $n -lt 3 ]; then echo "Error: The Internet connection appears to be offline." >&2; exit 1; fi
      printf '{"id":"cccccccc-1111-2222-3333-444444444444","status":"Accepted"}\n' ;;
    down) echo "Error: The Internet connection appears to be offline." >&2; exit 1 ;;
    unauthorized) echo "Error: HTTP status code: 401. Invalid credentials. key KEYIDSENTINEL99 issuer ISSUERSENTINEL99 file $FAKE_KEY" >&2; exit 1 ;;
    esac ;;
  log) printf '{"status":"Invalid","issues":[{"severity":"error","path":"Werkbord.zip/Werkbord.app/Contents/Helpers/werkbord","message":"The signature of the binary is invalid.","docUrl":"https://developer.apple.com/documentation/security/notarizing_macos_software_before_distribution/resolving_common_notarization_issues"}]}\n' ;;
  esac ;;
stapler)
  case "$sub:$FAKE_MODE" in
  staple:staple-fails) echo "Could not staple: the ticket is not available (Error 65)" >&2; exit 65 ;;
  validate:validate-fails) echo "The validate action failed! Error 65." >&2; exit 65 ;;
  *) echo "The $sub action worked!" ;;
  esac ;;
esac
