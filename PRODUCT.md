# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

Developers who run coding agents (Claude Code, Codex) on their own computers and want to steer them from a
desk or, away from it, from a phone. Werkbord Team adds people who coordinate work across members.

## Product Purpose

Werkbord is a local-first remote control for coding agents. Each repository is a project with a board,
a calendar, Git, and runs; the controller runs on the user's computer and keeps working with the browser
closed. Success is that the person sees what needs them immediately, answers it in one step, and reviews and
merges finished work without leaving the app.

## Positioning

No hosted backend and no account: the controller, data and agent sign-ins stay on the user's machines; a
phone reaches it over the user's own private network. An agent finishing never merges; the person does.

## Operating Context

Desk use on a large screen alongside an editor and terminal; quick checks and answers from a phone as an
installed web app. Several projects and machines (runners) at once.

## Capabilities and Constraints

One product with one version and one release (see AGENTS.md and docs/STRUCTURE.md). The Individual app
is a Svelte PWA embedded in the `werkbord` executable (`devboard` is kept as an alias).
The CSP forbids inline scripts and remote resources.

## Brand Commitments

Name: Werkbord (wordmark `werkbord`, lowercase). Tagline: "Werk local, ship global." Visual identity: the
identity sheet "Quiet technical · v1" (see DESIGN.md), and the app replicas on the landing site.

## Product Principles

- What needs you comes first and is answerable where it is shown.
- Fewer clicks for weekly work; nothing needed weekly hides behind a toggle.
- The window is the app on desktop: fit the viewport, scroll inside regions.
- Refuse rather than guess; confirm anything that changes a repository, with the evidence.
