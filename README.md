<img src="icon/app-256.png" width="64" height="64" alt="" style="image-rendering: pixelated">

# Generic Company Wizard Maker (install wizards)

Makes install wizards for Windows programs: one self-contained `setup.exe` per program, which installs it
for the current user (no administrator rights), lists it in **Apps & features** and leaves an
uninstaller. Generic Company Inc. is fictional; the wizard maker is the seventh program of its portfolio, and it
installs itself the same way.

Go 1.24, standard library only: the windows are native Win32 controls driven through `syscall`, the
shortcuts are made through COM, the registry is written through advapi32.

- **A setup** is a copy of the maker's own exe with the program's icon and version info written into its
  resources, then a zip of the files and a 32-byte trailer. It shows Welcome, License, Install Folder,
  Shortcuts, Installing and Finish. An earlier install of the same program (same app id) is upgraded in
  place, repaired at the same version, and refused at an older one.
- **Installing** is one transaction: files are unpacked and checked (SHA-256) beside the target first,
  replaced files are set aside, and any failure, or Cancel, puts back exactly what was there.
- **Uninstalling** removes only what setup installed, and only files still as it installed them; files
  you changed or added are kept and listed.
- The same input makes a byte-identical `setup.exe`, wherever its folder is.

## How it works

One exe plays three roles. `main.go` opens its own file (`payload.Open`) and reads the 32-byte trailer
at the end: no trailer means the maker, `Setup` a setup.exe, `Uninstall` an uninstall.exe.

Making a setup (the maker):

1. `main` (`main.go`) restricts DLL loading to System32 (`sys.HardenDLLSearch`) and, with no payload,
   calls `maker.Main` (`internal/maker/cli.go`): `build` on the command line, `-install`/`-uninstall`
   (`self.go`), or the window (`window_windows.go`, which edits the fields through `Form` in `form.go`).
2. `spec.Load` (`internal/spec/spec.go`) reads `setup.json`; `Spec.Check` (`check.go`) vets it and
   `Spec.Sources` (`sources.go`) lists every file to pack, sorted.
3. `maker.Build` (`build.go`) copies the maker's own exe and writes the program's icon and version info
   into its resources (`internal/winres`: `SetResources`, `IconResources`, the version block in
   `version.go`).
4. `payload.Write` (`internal/payload`) appends a zip (`files/<path>` for each file, hashed as read,
   then `app.ico`, then the manifest as `setup.json`, from `Spec.Manifest` in `manifest.go`) and the
   trailer. The output is written to a temp file and renamed into place.

Running a setup (the wizard):

1. `main` sees the `Setup` trailer and calls `wizard.RunSetup` (`internal/wizard/setup.go`): it parses
   the flags, reads the manifest back with `spec.ParseManifest` (which checks it again), refuses to run
   as administrator (`sys.Elevated`) and takes the per-program lock (`sys.Lock`).
2. `install.Existing` (`internal/install/plan.go`) finds an earlier install; `CheckFolder` vets the
   chosen folder. Without `-quiet`, `setupWindow` (`setup_windows.go`) shows the pages of `SetupFlow`
   (`flow.go`) in the frame from `frame_windows.go`.
3. `install.MakePlan` turns the manifest, the folders (`places.go`, from `shell.UserPlaces` or
   `SandboxPlaces` for `-root`) and the choices into a plan: data only.
4. `install.Apply` (`apply.go`) carries it out as one transaction: files, `uninstall.exe` (made by
   `makeUninstaller`), `install.json`, shortcuts (`shell.MakeLinkDir`) and the Apps & features entry
   (`internal/reg`).
5. Later, `wizard.RunUninstall` (`uninstall.go`, window in `uninstall_windows.go`) reads the marker
   (`install.ReadMarker`), calls `install.Uninstall` and removes itself with `DeleteAfterExit`.

Where state lives:

| What | Where |
|---|---|
| A setup's description | `setup.json`, next to the program's files; the maker keeps nothing of its own |
| A built setup | The `output` path (by default `dist\{name}-{version}-setup.exe`); inside it, the zip and trailer after the program part |
| An install | `%LOCALAPPDATA%\Programs\<group>\<name>\`: the files, `app.ico`, `uninstall.exe` (its payload holds `uninstall.json`) and the marker `install.json` (files, SHA-256s, shortcuts, purge folders) |
| An install in progress | `<dir>\.setup\new` (unpacked, checked files) and `<dir>\.setup\old` (replaced files), removed when it completes |
| Shortcuts | `<group>\<name>.lnk` in the Start menu's Programs folder, and `<name>.lnk` on the desktop if ticked |
| Apps & features | `HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall\<id>` (`internal/reg/hkcu_windows.go`); with `-root DIR`, `DIR\registry.json` (`internal/reg/file.go`), and every folder under DIR |
| One setup at a time | A session mutex named `GenericCompany.Wizard.<id>` (`lockName` in `internal/wizard/setup.go`) |
| Python | Read only: the PEP 514 `PythonCore` keys (`internal/python`) |

## Code map

| File | What |
|---|---|
| `main.go` | Entry point: picks the role from the exe's own payload; embeds `LICENSE` for the maker's own setup |
| `internal/app/app.go` | Identity (`Version`, `ID`, `Name`, `Title`, `Credit`) and the exit codes 0 to 4 |
| `internal/spec/spec.go` | `Spec`: `Load`, `Save`, paths, the license text, the output path |
| `internal/spec/check.go` | Every rule `Spec.Check` applies: identity, `run`, `exclude`, `purge`, Windows names |
| `internal/spec/sources.go` | `Spec.Sources`: the file rules walked into a sorted list of files to pack |
| `internal/spec/placeholders.go` | `{app}`, `{name}`, `{localappdata}` and the like; `Expand`, `PurgePath` |
| `internal/spec/version.go` | `CompareVersions` for dotted numeric versions |
| `internal/spec/manifest.go` | `Manifest`: the checked, hashed `setup.json` a setup carries; `ParseManifest` re-checks it |
| `internal/maker/cli.go` | `Main`, the maker's command line, and the `build` command |
| `internal/maker/build.go` | `Build`: icon, version info, payload, written atomically |
| `internal/maker/self.go` | The maker installing and uninstalling itself through its own setup (`SelfSpec`) |
| `internal/maker/form.go` | `Form`, the window's fields as plain data, to and from a `Spec` |
| `internal/maker/window_windows.go` | The maker window: menu, fields, open, save, commands |
| `internal/maker/window_build_windows.go` | The Build command on a worker goroutine, and the maker's `-smoke` walk |
| `internal/maker/window_help_windows.go` | The Help menu: About, install and uninstall the maker |
| `internal/payload/payload.go` | The zip and trailer after the program: `Write`, `Open`, `Layout`, `CopyChecked` |
| `internal/winres/winres.go`, `pe.go`, `version.go` | Resources in pure Go: `.ico` files and icon groups, the PE resource section, version info |
| `internal/wizard/setup.go` | `RunSetup`, its flags, the quiet install, `makeUninstaller`, `lockName` |
| `internal/wizard/flow.go` | `SetupFlow` and `UninstallFlow`: which pages show, in order |
| `internal/wizard/frame_windows.go` | The wizard frame shared by both windows: layout and command ids |
| `internal/wizard/setup_windows.go` | The install wizard's pages |
| `internal/wizard/uninstall.go`, `uninstall_windows.go` | `RunUninstall`, its quiet run, and the Confirm, Removing and Done pages |
| `internal/wizard/python.go` | `findPython` for `run.program` `{python}` |
| `internal/install/plan.go` | The marker (`install.json`), `MakePlan`, `CheckFolder`, `Existing`; start reading here |
| `internal/install/places.go` | `Places`: the folders an install touches, real or under `-root` |
| `internal/install/registry.go` | The Apps & features values and the `Registry` interface |
| `internal/install/apply.go` | `Apply`: the install transaction and its rollback |
| `internal/install/uninstall.go` | `Uninstall` and `DeleteAfterExit` |
| `internal/reg/file.go`, `hkcu_windows.go` | The `Registry` as a sandbox `registry.json`, or the real HKCU through advapi32 |
| `internal/python/python.go`, `python_windows.go` | `Find`: the newest registered CPython at or above a minimum |
| `internal/shell/com_windows.go`, `link_windows.go`, `dialog_windows.go` | Hand-made COM; `.lnk` shortcuts and known folders; file and folder pickers |
| `internal/sys/sys_windows.go`, `checks_windows.go` | DLL search path, console output, message boxes, the lock, elevation and free-space checks |
| `internal/win/api_windows.go` | The Win32 toolkit's DLL procs and helpers |
| `internal/win/window_windows.go` | The top-level window: DPI-aware layout, fonts, the message loop |
| `internal/win/control_windows.go`, `menu_windows.go`, `draw_windows.go` | Native controls, menus and accelerators, pixel-art icon drawing |
| `internal/win/smoke_windows.go` | The `-smoke` harness: step captures (PNG) and a JSON report |
| `internal/testexe/testexe.go` | Builds the maker once per test binary, as released, for tests that need a real exe |
| `icon/icon.go` | Embeds `icon/app.ico` |
| `main_test.go` | End-to-end: build the maker, make a setup of `testdata/fixture`, install, upgrade, refuse, uninstall, and walk every window with `-smoke` |
| `internal/*/*_test.go` | One test file per package (`internal/install` has `plan_test.go`, `apply_test.go`, `uninstall_test.go` and shared `fixtures_test.go`); `icon/icon_test.go` checks the icon files |
| `testdata/fixture/` | The test program's `setup.json`, license and files (the tests supply `hello.exe`) |
| `icon/`, `rsrc_windows_amd64.syso` | The icon files and the linked icon and manifest; see `icon/README.md` |
| `setup.json` | The maker's own setup description |

## Build and run

```bash
go build -trimpath -ldflags "-H windowsgui -s -w" -o wizard-maker.exe .
```

```bash
wizard-maker.exe
```

opens the window: fill in the program's details (or open a `setup.json`) and **Build** (F5). A build is
always of `setup.json` as saved. From a script:

```bash
wizard-maker.exe build -o out\setup.exe setup.json | Out-Host
```

Flags go before `setup.json`; piping makes PowerShell wait and set `$LASTEXITCODE`. Every role shares the
exit codes: 0 done, 1 failed (rolled back), 2 bad arguments or `setup.json`, 3 cancelled, 4 refused (a
newer version installed, a folder that is not new, empty or the program's own, a file in use, run as
administrator, Python missing).

`rsrc_windows_amd64.syso` embeds the pixel icon and a manifest (Common Controls 6, asInvoker,
per-monitor DPI); the portfolio's `tools/pixel_icons.py --syso` generates it from `icon/app.res`.
`-trimpath` keeps the build machine's paths out of every setup.exe.

## setup.json

```json
{
  "id": "GenericCompany.CMS", "name": "Generic Company Journal", "version": "0.2.0", "publisher": "Generic Company Inc. (fictional)",
  "group": "Generic Company Inc", "icon": "icon/app.ico", "license": "LICENSE",
  "files": [{"from": "generic-company-cms.exe"}, {"from": "mock", "to": "mock"}],
  "exclude": ["*.pyc", "__pycache__"],
  "run": {"program": "generic-company-cms.exe", "args": []},
  "askFolder": true, "shortcuts": {"ask": true, "startMenu": true, "desktop": false},
  "purge": ["{localappdata}\\Generic Company Inc\\GenericCompany.CMS"],
  "output": "dist/{name}-{version}-setup.exe"
}
```

| Field | Means |
|---|---|
| `id` | The app id: its Apps & features key, and what an upgrade matches on (letters, digits, `.`, `_`, `-`) |
| `name`, `version`, `publisher` | As shown; `version` is numbers like `1.2.0` |
| `group` | A folder for the program and for its Start-menu shortcut (`Generic Company Inc` is where the other Generic Company programs go) |
| `icon` | An `.ico` for the setup, the shortcuts and Apps & features; by default the program's own icon |
| `license` | A text file; with one, setup shows a License page and Next waits for "I accept" |
| `files` | Each `from` (a file, or a folder's contents) goes into the folder `to` of the install folder |
| `exclude` | Patterns such as `*.pyc` or `__pycache__`; one with a `/` matches the whole relative path |
| `run` | The `program` the shortcuts start (a packed file, or `{python}`), its `args` (`{app}` is the install folder), and an optional `appUserModelId` |
| `requires.python` | With `{python}`: the lowest CPython, e.g. `3.10`; setup uses the newest one registered (PEP 514) |
| `askFolder`, `shortcuts` | Whether setup asks, and which shortcuts start ticked |
| `purge` | Settings folders (`{localappdata}\…` or `{appdata}\…`, two levels deep) the uninstaller offers to remove |
| `output` | Where the setup.exe goes (`{name}` and `{version}` are filled in) |

Paths are relative to `setup.json`, and unknown fields are errors.

## Running a setup

`setup.exe` shows the wizard. `setup.exe -quiet` (or `/S`) installs with the defaults and accepts the
license. It also takes `-dir PATH`, `-desktop-shortcut`, `-no-start-menu`, `-log FILE` and `-root DIR` (a
sandbox: everything under DIR and the registry in `DIR\registry.json`, like the other Generic Company programs'
`-root`). The uninstaller takes `-quiet`, `-purge`, `-root` and `-log`. Every role also has `-version`
and `-smoke FILE`, which walks its window, captures each page and writes a report.

A program is installed in `%LOCALAPPDATA%\Programs\<group>\<name>` with an `install.json` marker in the
other Generic Company programs' format (plus file hashes), so either installer can take over or remove the other's
install. Setup never runs as administrator, installs only into a new or empty folder (or the program's
own), and lets one setup or uninstaller of a program run at a time.

## Install

```bash
wizard-maker.exe -install
```

installs the maker for the current user through its own setup (with `-desktop-shortcut`, `-quiet` or
`-root DIR`); `wizard-maker.exe -uninstall`, or Apps & features, removes it. **Help** in the window does
the same. To make the maker's own `setup.exe`, build the maker, then:

```bash
wizard-maker.exe build setup.json
```

## Dependencies

None outside the Go standard library.

## Developing

Everything runs from this folder (`company/wizard_maker`), with Go 1.24.

| Step | Command |
|---|---|
| Run | `go run .` opens the maker window (the build line under **Build and run** makes the real exe) |
| Vet | `go vet ./...` |
| Test | `go test ./...` (a few minutes); `go test -short ./...` for the fast part |
| Smoke-check | `wizard-maker.exe -smoke smoke.json setup.json \| Out-Host`: opens the window on the maker's own `setup.json`, builds `dist\Generic Company Wizard Maker-0.1.0-setup.exe`, writes `smoke.json` and a PNG per step beside it, and exits 0 if the build worked |
| Try a setup safely | `setup.exe -quiet -root sb \| Out-Host`, then `sb\Programs\<group>\<name>\uninstall.exe -quiet -root sb \| Out-Host`: install and remove inside the folder `sb`, registry included |
| Build | the `go build` line under **Build and run** |
| Package | `wizard-maker.exe build setup.json` (see **Install**) |

Where to add a typical feature:

- **A new `setup.json` field**: a field on `Spec` in `internal/spec/spec.go` and its rule in
  `check.go`; carry it into `Manifest` (`manifest.go`, and `Manifest.Check` if setups must re-check
  it); a field in `Form` (`internal/maker/form.go`) and a control in `window_windows.go`; tests in
  `spec_test.go` and `form_test.go`; a row in the **setup.json** table above. Unknown fields are
  errors, so a new setup.json only loads in a maker that knows the field.
- **A new wizard page**: a `Page` constant and its name in `pageNames` (`internal/wizard/flow.go`),
  its place in `SetupFlow` (test in `flow_test.go`), and its controls in `setup_windows.go`;
  `TestWizardWindows` in `main_test.go` walks every page with `-smoke`.

Twins: none file for file. The install layout and the `install.json` marker are shared with the
other Generic Company programs' self-installers (`company/journal/internal/install`,
`company/notify_hub/internal/install`, `company/crud_hub/installer.py`,
`company/doc_pipeline/docpipe/installer.py`, `company/ticket_tracker/src/host/install.rs`,
`company/data_dash/src/host/install.rs`), so each can take over or remove the other's install. Change
the marker's fields or the folder layout (`internal/install/plan.go`, `places.go`) only together with
them; `TestTakeOverASiblingInstall` in `main_test.go` checks one such hand-over.

Traps the code comments warn about:

- `Version = "0.1.0"` in `internal/app/app.go` must stay the first line there that starts with
  `Version` and `=`: the Dev Console (`tools/dev_console`) rewrites it on a version bump.
  `setup.json` repeats the id, name, version and publisher and must agree (`TestOwnSetupJSON`).
- Old setups and uninstallers must keep working with new ones: never change `lockName`
  (`internal/wizard/setup.go`), the trailer's `GENCOWZ1` magic and layout (`internal/payload`), or the
  exit codes (`internal/app`).
- The `-smoke` report's JSON tags (`internal/win/smoke_windows.go`) and the page names in `pageNames`
  are read by `main_test.go`; renaming one breaks the end-to-end tests.
- Reproducible builds: zip entry times are fixed (`payload.go`), `Spec.Sources` sorts, and the
  manifest holds no build paths. Keep it so; `TestDeterministic` and the build tests check it.
- `icon/` and `rsrc_windows_amd64.syso` are generated by `tools/pixel_icons.py`; do not edit them by
  hand. `icon/icon_test.go` checks the icon files against the hashes in `icon/README.md`.
- Every source file starts with a portfolio header and ends with a stamp line. After editing one,
  run `python tools/stamp.py --restamp <path>` from the portfolio root; `--check` verifies.

## Tests

```bash
go vet ./...
```

```bash
go test ./...
```

Run both from this folder. `go test -short ./...` skips the tests that build or run exes or touch the real registry. The others
build the maker, make a setup of `testdata/fixture`, and install, upgrade and uninstall it in a temporary
sandbox, through the quiet path and through the windows (`-smoke`; every control must sit inside its
window, clear of the others, with its text fitting).

## Not done yet

- Code signing: setups are unsigned, so Windows SmartScreen asks before running one downloaded from
  elsewhere.
- A running Python program is not noticed as "in use": it runs as `pythonw.exe` from elsewhere.

## Icon and license

Icon: Silk "wand" by Mark James, CC BY 2.5, enlarged by nearest-neighbour (`icon/README.md`). Code:
AGPL-3.0, as the rest of the portfolio.
