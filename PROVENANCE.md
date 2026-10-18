# Provenance

Nothing here comes from the owner's private staging folder: no installer, Win32 or COM code was among the
staged originals. A few pieces follow the portfolio's own published Go programs, identified below by
project, file and commit. Everything else is new.

## Files

### `internal/install/plan.go`, `uninstall.go`

From `content_management_system/internal/install/install.go` (commit `d38f121`, "Add self-installer";
the same file is in `notification_and_message_hub`):

- The folder layout and the `-root` sandbox (`Locations` → `SandboxPlaces`, `Places.Dir`,
  `StartMenuLink`, `DesktopLink`): the same paths, so both installers meet in the same places.
- The marker's fields (`marker` → `Marker`: `app_id`, `name`, `version`, `files`, `shortcuts`), kept
  so either installer reads the other's; `sha256`, `purge` and `installer` are new.
- The Apps & features values (`MakePlan`'s `Registry` list), with `UninstallString` now always quoted
  (`quote` → `quoteArg`).
- `DeleteAfterExit`: the hidden `cmd` that removes the uninstaller after it exits. Changed: the paths
  travel in environment variables, the exe goes with `del` and the folders with a plain `rd` (never
  `/s`), so files the user kept stay.
- `SafeToRemove`'s rule (a folder is ours only if its marker names us) → `CheckFolder` and `Existing`.

### `internal/reg/file.go`

The sandbox `registry.json` format follows `editRegistryFile` and `registryValues` in the same file (key
names, strings as strings, DWORDs as numbers).

### `internal/win/window_windows.go`, `internal/sys/sys_windows.go`

`Window.IconSet` follows `Window.IconSet`, and `sys.MessageBox` follows `MessageBox`, in
`content_management_system/internal/desktop/window_windows.go` (commit `5745f71`).

### `internal/app/app.go`, `icon/icon_test.go`

Shaped like `content_management_system/internal/app/app.go` and `icon_test.go` (commit `4bb634d`): the
same constants, and the same pixel and credit checks, reading this project's own files.
