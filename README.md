<img src="icon/app-256.png" width="64" height="64" alt="" style="image-rendering: pixelated">

# ACME Wizard Maker (install wizards)

Makes install wizards for Windows programs: one self-contained `setup.exe` per program, which installs it
for the current user (no administrator rights), lists it in **Apps & features** and leaves an
uninstaller. ACME Inc. is fictional; the wizard maker is the seventh program of its portfolio, and it
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
  "id": "ACME.CMS", "name": "ACME Journal", "version": "0.2.0", "publisher": "ACME Inc. (fictional)",
  "group": "ACME Inc", "icon": "icon/app.ico", "license": "LICENSE",
  "files": [{"from": "acme-cms.exe"}, {"from": "mock", "to": "mock"}],
  "exclude": ["*.pyc", "__pycache__"],
  "run": {"program": "acme-cms.exe", "args": []},
  "askFolder": true, "shortcuts": {"ask": true, "startMenu": true, "desktop": false},
  "purge": ["{localappdata}\\ACME Inc\\ACME.CMS"],
  "output": "dist/{name}-{version}-setup.exe"
}
```

| Field | Means |
|---|---|
| `id` | The app id: its Apps & features key, and what an upgrade matches on (letters, digits, `.`, `_`, `-`) |
| `name`, `version`, `publisher` | As shown; `version` is numbers like `1.2.0` |
| `group` | A folder for the program and for its Start-menu shortcut (`ACME Inc` is where the other ACME programs go) |
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
sandbox: everything under DIR and the registry in `DIR\registry.json`, like the other ACME programs'
`-root`). The uninstaller takes `-quiet`, `-purge`, `-root` and `-log`. Every role also has `-version`
and `-smoke FILE`, which walks its window, captures each page and writes a report.

A program is installed in `%LOCALAPPDATA%\Programs\<group>\<name>` with an `install.json` marker in the
other ACME programs' format (plus file hashes), so either installer can take over or remove the other's
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

## Tests

```bash
go vet ./...
```

```bash
go test ./...
```

`go test -short ./...` skips the tests that build or run exes or touch the real registry. The others
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
