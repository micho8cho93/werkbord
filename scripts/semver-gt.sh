#!/bin/sh
# Exit successfully when the first semantic version has higher precedence than the second.
set -eu

[ "$#" -eq 2 ] || { echo "usage: $0 <newer> <older>" >&2; exit 2; }

LC_ALL=C awk -v newer="$1" -v older="$2" '
function parse(version, side,    dash, core, pre, count, fields, ids, i) {
  if (index(version, "+")) return 0
  dash = index(version, "-")
  if (dash) {
    core = substr(version, 1, dash - 1)
    pre = substr(version, dash + 1)
    if (pre == "") return 0
  } else {
    core = version
    pre = ""
  }
  if (split(core, fields, ".") != 3) return 0
  for (i = 1; i <= 3; i++) {
    if (fields[i] !~ /^(0|[1-9][0-9]*)$/) return 0
    if (side == "newer") new_core[i] = fields[i]
    else old_core[i] = fields[i]
  }
  count = 0
  if (pre != "") {
    count = split(pre, ids, ".")
    for (i = 1; i <= count; i++) {
      if (ids[i] !~ /^[0-9A-Za-z-]+$/) return 0
      if (ids[i] ~ /^[0-9]+$/ && ids[i] ~ /^0[0-9]+$/) return 0
      if (side == "newer") new_pre[i] = ids[i]
      else old_pre[i] = ids[i]
    }
  }
  if (side == "newer") new_pre_count = count
  else old_pre_count = count
  return 1
}

function number_compare(a, b,    i, da, db) {
  if (length(a) < length(b)) return -1
  if (length(a) > length(b)) return 1
  for (i = 1; i <= length(a); i++) {
    da = index("0123456789", substr(a, i, 1))
    db = index("0123456789", substr(b, i, 1))
    if (da < db) return -1
    if (da > db) return 1
  }
  return 0
}

function identifier_compare(a, b,    an, bn) {
  an = (a ~ /^[0-9]+$/)
  bn = (b ~ /^[0-9]+$/)
  if (an && bn) return number_compare(a, b)
  if (an) return -1
  if (bn) return 1
  if (a < b) return -1
  if (a > b) return 1
  return 0
}

BEGIN {
  if (!parse(newer, "newer") || !parse(older, "older")) exit 2
  for (i = 1; i <= 3; i++) {
    order = number_compare(new_core[i], old_core[i])
    if (order) exit(order > 0 ? 0 : 1)
  }
  if (!new_pre_count && old_pre_count) exit 0
  if (new_pre_count && !old_pre_count) exit 1
  limit = new_pre_count < old_pre_count ? new_pre_count : old_pre_count
  for (i = 1; i <= limit; i++) {
    order = identifier_compare(new_pre[i], old_pre[i])
    if (order) exit(order > 0 ? 0 : 1)
  }
  exit(new_pre_count > old_pre_count ? 0 : 1)
}'
