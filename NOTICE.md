# Third-party notices

sopds-go itself is licensed under the **GNU Affero General Public License v3.0** —
see [LICENSE](LICENSE) at the repo root. This file lists upstream works that ship
inside the binary (bundled), live alongside it (`fb2converters/`), are linked at
build time (Go modules / Rust crates), or are invoked as subprocesses at runtime,
together with their own licenses and attribution notices.

AGPL-3.0 compatibility has been verified for every direct dependency listed below.

---

## Original SOPDS — Python implementation

sopds-go is a Go rewrite of [V.A. Onishchenko's Python SOPDS](https://github.com/sergey-dryabzhinsky/sopds), which is itself GPL-licensed. The architecture, config schema, MySQL database layout (preserved for the `import-mysql` migration tool), OPDS feed structure, and many UI conventions were carried over directly. Significant credit to Onishchenko and the upstream SOPDS contributors.

---

## Bundled converters (`fb2converters/`)

The `fb2converters/` directory ships two third-party FB2 converter tools that
sopds-go can optionally invoke as subprocesses for FB2 → MOBI / FB2 → ePub
conversion. They are NOT linked into the sopds-go binary; users opt in by
pointing the `converters.fb2tomobi` / `converters.fb2toepub` config keys at
the appropriate executable.

### `fb2converters/fb2mobi/`

FB2-to-MOBI converter, MIT License. Originally based on [`fb2conv`](https://github.com/dnkorpushov) by dnkorpushov; later contributors translated messages to English, ported to Python 3, added GUI and macOS binaries. Copyright held by the listed authors.

### `fb2converters/fb2epub/`

FB2-to-ePub converter (originally a .NET project by eBook .NET), MIT License,
Copyright (c) 2020 eBook .NET.

---

## Go module dependencies (direct)

All linked into the sopds-go binary at build time. Run `go list -m all` for the
full transitive list; this section covers direct deps only.

| Module | License | Use |
|---|---|---|
| `github.com/bodgit/sevenzip` | MIT | 7z archive support (book ZIP scanning) |
| `github.com/dhowden/tag` | BSD-2-Clause | Audio file tag extraction (audiobook support) |
| `github.com/go-chi/chi/v5` | MIT | HTTP router |
| `github.com/go-sql-driver/mysql` | MPL-2.0 | MySQL driver (for `import-mysql` migration tool) |
| `github.com/golang-jwt/jwt/v5` | MIT | JWT auth tokens |
| `github.com/google/uuid` | BSD-3-Clause | UUID generation |
| `github.com/robfig/cron/v3` | MIT | Cron-format scheduler |
| `github.com/saracen/go7z` | MIT | Alternative 7z library |
| `github.com/spf13/cobra` | Apache-2.0 | CLI command framework |
| `golang.org/x/crypto` | BSD-3-Clause | Crypto primitives (auth) |
| `golang.org/x/text` | BSD-3-Clause | Unicode text handling |
| `gopkg.in/yaml.v3` | MIT (and Apache-2.0 for some files) | YAML config parsing |
| `gorm.io/driver/postgres` | MIT | PostgreSQL ORM driver |
| `gorm.io/gorm` | MIT | ORM framework |

MPL-2.0 (mysql driver) is AGPL-compatible per FSF's license-compatibility list
because MPL-2.0 §3.3 explicitly permits combination with secondary licenses
including AGPL-3.0.

---

## Rust crate dependencies

### `sopds-tts-rs/` — direct dependencies

| Crate | License | Use |
|---|---|---|
| `ort` (=2.0.0-rc.10) | Apache-2.0 / MIT (dual) | ONNX Runtime Rust binding (CUDA EP) |
| `serde` | Apache-2.0 / MIT (dual) | Serialization (model JSON metadata) |
| `serde_json` | Apache-2.0 / MIT (dual) | JSON parsing |
| `hound` | Apache-2.0 | WAV file encoding |

`ort` transitively builds and links **ONNX Runtime** (Microsoft, MIT License),
which in turn builds against the **NVIDIA CUDA Toolkit** and **cuDNN** at compile
time on systems with GPU support enabled. Both NVIDIA libraries are proprietary,
distributed under the [NVIDIA CUDA Toolkit EULA](https://docs.nvidia.com/cuda/eula/index.html)
and [cuDNN License Agreement](https://docs.nvidia.com/deeplearning/cudnn/sla/index.html).
sopds-tts-rs does NOT redistribute these libraries — they are linked dynamically
from the user's installation, so the binary itself remains AGPL-distributable.

### `zipdupes-rs/` — direct dependencies

| Crate | License | Use |
|---|---|---|
| `clap` | Apache-2.0 / MIT (dual) | CLI argument parsing |
| `walkdir` | MIT / Unlicense (dual) | Recursive directory walker |

---

## External tools called as subprocesses

These are NOT linked into sopds-go and NOT bundled — they must be installed
separately by the user. sopds-go invokes them via `exec.Command`. Their
licenses do not transitively affect sopds-go's binary distribution.

| Tool | License | Use |
|---|---|---|
| **Calibre** (`ebook-convert`) | GPL-3.0 | FB2 → MOBI conversion (optional) |
| **espeak-ng** | GPL-3.0 | IPA phoneme generation for Piper TTS (`internal/tts/`) |
| **Piper TTS** | MIT | TTS engine — sopds-go ships a Go binding to Piper's ONNX models, but Piper itself is upstream |
| **PostgreSQL** | PostgreSQL License (BSD-like) | Database server |

GPL-3.0 tools (Calibre, espeak-ng) are called via subprocess and therefore
do NOT impose the GPL on sopds-go itself. sopds-go uses these as CLI tools,
not as library code.

---

## Voice models (TTS)

Piper voice models (`*.onnx` + `*.onnx.json`) are NOT bundled with sopds-go.
Users download models from [rhasspy/piper](https://github.com/rhasspy/piper#voices)
or other sources. Each model has its own license — most are MIT or CC-BY licensed
by their respective authors. Check the source repository of each model before
redistribution.

The Ukrainian-voice models referenced in PROGRESS Rev 51 (`uk_UA-ukrainian_tts-medium`,
speakers lada/mykyta/tetiana) are from the [piper-voices](https://huggingface.co/rhasspy/piper-voices) collection, MIT-licensed by their respective authors.

---

## Web UI assets

The web UI uses [Font Awesome](https://fontawesome.com/) icons (Free version,
SIL OFL 1.1 / CC BY 4.0 / MIT). Icons are loaded from the Font Awesome CDN at
runtime — not bundled into the binary.

---

## Reporting an attribution issue

If you believe a third-party work has not been correctly attributed in this file
or that a license has been misclassified, please open an issue at
https://github.com/dimgord/sopds-go/issues with `[NOTICE]` in the title.
