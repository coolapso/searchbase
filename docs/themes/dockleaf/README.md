# Dockleaf vendored theme

Source checkout: `~/Dev/dockleaf`
Upstream: https://github.com/coolapso/hugo-theme-dockleaf
License: MIT (see LICENSE and NOTICE).

This directory is a self-contained copy of Dockleaf. Searchbase uses it through
`theme = 'dockleaf'` in `docs/hugo.toml`. No sibling checkout, Go module download,
or submodule initialization is required for builds.

Make reusable changes in Dockleaf first, run its `task test`, then synchronize
`layouts/`, `static/`, `i18n/`, `hugo.toml`, `theme.toml`, `LICENSE`, and `NOTICE`
into this directory. Review the diff and run `task docs:check` from Searchbase.
Keep Searchbase copy, menus, homepage cards, and artwork in the parent site.
