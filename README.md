# aqtpserver

[日本語](README.ja.md)

A local HTTP server for your installed AquesTalkPlayer desktop app. It provides
an OpenAI-compatible `POST /v1/audio/speech` endpoint that returns WAV audio,
including support for `@aituber-onair/voice`'s `openaiCompatible` engine.

aqtpserver does not include, download, or redistribute AquesTalkPlayer, its
engines, dictionaries, or voice data. The Web version is not supported.

## About AquesTalkPlayer

[AquesTalkPlayer](https://www.a-quest.com/products/aquestalkplayer.html) is a
speech synthesis app developed by AQUEST Corp. It combines the AquesTalk1,
AquesTalk2, and AquesTalk10 engines with AqKanji2Koe for reading Japanese text,
including the voices commonly known as "yukkuri" voices. AQUEST provides Windows,
macOS, and Web versions; aqtpserver uses only the Windows and macOS desktop apps.

The speech synthesis technology is AQUEST's work, and we thank AQUEST for making
it available. Download AquesTalkPlayer from the official product page linked
above and follow its usage terms.

## Prerequisites

- macOS 14 or later, or Windows. Server release binaries target macOS and Windows
  on amd64 and arm64; the installed player must also run on your machine.
- Install [AquesTalkPlayer](https://www.a-quest.com/products/aquestalkplayer.html)
  yourself and open it once to configure it.
- Use a built-in preset such as `れいむ` or `まりさ`, or create your own in the GUI.
  Any GUI preset name can be used as `voice`. To change the voice type, speed,
  volume, or pitch, edit a preset or create one such as `slow`. These settings
  have no individual CLI flags; aqtpserver never reads or edits preset files.

## Install

Release archives and SHA-256 checksums are published to
[GitHub Releases](https://github.com/shinshin86/aqtpserver/releases) when a version
is released. Before the first release, build from source.

macOS:

```sh
curl -fsSL https://raw.githubusercontent.com/shinshin86/aqtpserver/main/install.sh -o install.sh
# Review the script, then run it.
bash install.sh
# Optional: select a release and destination.
BIN_DIR="$HOME/.local/bin" bash install.sh vX.Y.Z
export PATH="$HOME/.local/bin:$PATH"
```

Windows (PowerShell):

```powershell
Invoke-WebRequest https://raw.githubusercontent.com/shinshin86/aqtpserver/main/install.ps1 -OutFile install.ps1
# Review the script, then run it.
.\install.ps1
# Optional: select a release.
.\install.ps1 -Version vX.Y.Z
```

Replace `vX.Y.Z` with an actual release tag. Both installers verify checksums and
back up an existing binary as `.bak`. macOS defaults to `~/.local/bin`; Windows
defaults to `%LOCALAPPDATA%\Programs\aqtpserver` and adds that directory to the
user PATH. Restart the Windows terminal afterward. Set `BIN_DIR` to override
the destination. Neither installer installs AquesTalkPlayer.

From source (Go 1.23.2 or later):

```sh
git clone https://github.com/shinshin86/aqtpserver.git
cd aqtpserver
go build -trimpath -o aqtpserver .
```

On Windows, use `go build -trimpath -o aqtpserver.exe .`. Building and testing
work on Linux, but starting the server there is not supported.

## Run

macOS uses `/Applications/AquesTalkPlayer.app/Contents/MacOS/AquesTalkPlayer`:

```sh
aqtpserver --default-preset 'れいむ'
```

Windows has no default player location. Point to the executable in the directory
where you extracted the official package:

```powershell
aqtpserver --aquestalkplayer-path "C:\Apps\AquesTalkPlayer\AquesTalkPlayer.exe" --default-preset "れいむ"
```

The default address is `http://127.0.0.1:20203`. A missing executable is a startup
error with a setup hint. The server runs one synthesis process at a time; the
timeout includes time spent waiting for earlier requests. Each request uses a
separate temporary directory, which is removed after success, error, or timeout.
The process is killed if its request expires or is canceled.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--host` | `127.0.0.1` | Loopback only; also accepts `localhost` or `::1` |
| `--port` | `20203` | HTTP port, 1–65535 |
| `--aquestalkplayer-path` | macOS path above; none on Windows | Installed executable, not the `.app` directory |
| `--default-preset` | Empty | Preset when `voice` is omitted or empty |
| `--timeout` | `30s` | Positive Go duration, including queue time |
| `--max-input-chars` | `1000` | Positive input limit in Unicode code points |
| `--allowed-origin` | Empty | Additional exact CORS origins; repeat or separate with spaces |
| `--version` | | Print version and exit |
| `--help` | | Print usage |

`localhost` binds to `127.0.0.1` without a DNS lookup. Non-loopback bind addresses
and non-loopback HTTP `Host` headers are rejected. CORS permits HTTP loopback
origins such as `http://localhost:5173`, `http://127.0.0.1:3000`, and
`http://[::1]:3000`, plus `app://` origins. Other origins require an exact match:

```sh
aqtpserver --allowed-origin https://your-app.example
```

Disallowed origins receive 403 before synthesis. Preflight supports
`Content-Type` and `Authorization`; an API key is not required and Authorization
is ignored. There is no LAN mode. Do not expose the service through a proxy,
tunnel, or port forwarding to other people.

## API

### GET /

```sh
curl http://127.0.0.1:20203/
```

```json
{"engine":"aqtpserver","model":"aquestalkplayer"}
```

Returns server information for OpenAI-compatible clients. The AITuber OnAir
React examples use this response to show AquesTalkPlayer-specific hints.
Only the exact path `/` serves this information; unknown paths return 404.
`HEAD /` returns 200 with the JSON content type and no body. `OPTIONS /` supports
CORS preflight for GET and HEAD. Other methods return 405. The same Host checks,
CORS rules, and request logging apply as for the other endpoints.

### POST /v1/audio/speech

```sh
curl --fail-with-body http://127.0.0.1:20203/v1/audio/speech \
  -H 'Content-Type: application/json' \
  -d '{"model":"aquestalkplayer","input":"こんにちは。","voice":"れいむ","response_format":"wav","speed":1.0}' \
  -o speech.wav
```

In PowerShell, use `curl.exe` and a UTF-8 JSON file with `--data-binary @request.json`
to avoid shell quoting differences. Successful responses are `200 audio/wav`.

| Member | Accepted values |
| --- | --- |
| `input` | Required non-blank string, up to 1000 Unicode code points by default |
| `voice` | GUI preset name; omitted or empty uses `--default-preset` |
| `model` | `aquestalkplayer` or omitted |
| `response_format` | `wav` or omitted |
| `speed` | Numeric `1` / `1.0` or omitted; choose a GUI preset to change actual speed |
| `instructions` | Empty string or omitted; voice properties come from presets |

If both `voice` and `--default-preset` are empty, the CLI preset argument is
omitted and AquesTalkPlayer uses the last preset selected in its GUI. Input
starting with `#>` is passed through as a phonetic symbol string. Arguments are
passed directly to the executable without a shell. See the official
[Windows manual](https://www.a-quest.com/products/aquestalkplayer_man.html) and
[macOS manual](https://www.a-quest.com/products/aquestalkplayer_mac_man.html).

Unknown members, unsupported values, wrong types, and explicit `null` values
return 400. NUL characters in `input` or `voice` are rejected. The JSON body must
be UTF-8, has a separate 1 MiB limit, and requires `Content-Type: application/json`.
Input is preserved; the server does not trim or normalize it before synthesis.

Errors use this shape:

```json
{"error":{"message":"...","type":"invalid_request_error","param":"voice","code":"preset_not_found"}}
```

`param` is `null` when the error does not concern one field. Invalid presets and
empty/overlong text return 400; other synthesis failures return 500. A missing
executable after startup returns 503, timeout returns 504, and cancellation
returns 408 if the client is still connected. Invalid content type returns 415
and an oversized body returns 413. Request logs contain method, known route,
status, and duration, without input text, query strings, or CLI output.

### GET /v1/models

```sh
curl http://127.0.0.1:20203/v1/models
```

```json
{"object":"list","data":[{"id":"aquestalkplayer","object":"model","created":0,"owned_by":"local"}]}
```

### GET /health

```sh
curl http://127.0.0.1:20203/health
```

```json
{"status":"ok","executable_exists":true}
```

This checks the configured executable file, not synthesis readiness. If it is
removed after startup, health still returns 200 with `status: "degraded"` and
`executable_exists: false`. No preset-list endpoint is provided because the
player's preset file format is undocumented.

## Use with @aituber-onair/voice

```ts
import { VoiceEngineAdapter } from '@aituber-onair/voice';

const voice = new VoiceEngineAdapter({
  engineType: 'openaiCompatible',
  openAiCompatibleApiUrl: 'http://127.0.0.1:20203/v1/audio/speech',
  openAiCompatibleModel: 'aquestalkplayer',
  speaker: 'れいむ', // A built-in preset, or the name of your own GUI preset.
  openAiCompatibleResponseFormat: 'wav',
});

await voice.speakText('こんにちは。');
```

`VoiceService` is a type-only export; use `VoiceEngineAdapter` to create an
instance. The adapter sends directly to `openAiCompatibleApiUrl`, so use the full
speech URL. Example apps that call `resolveOpenAICompatibleSpeechEndpoint` can also
accept the origin `http://127.0.0.1:20203` and resolve the endpoint themselves.
Leave `openAiCompatibleSpeed` at 1 and instructions empty. To use a slow voice,
create a slow preset and set `speaker` to its name. If you increase the server
timeout, also increase `openAiCompatibleTimeoutMs` in the client.

In Node.js, audio playback requires an optional player (`speaker` or `play-sound`)
or an `onPlay` callback. Without one, synthesis runs but audio is not played.

## Development and verification

```sh
gofmt -l .
go vet ./...
go test ./...
go test -race ./...
```

Tests use a fake runner and a Go helper executable; they need no AquesTalkPlayer
files. CI runs vet and tests on Ubuntu, macOS, and Windows. Version tags of the
form `vX.Y.Z` run CI, build the four release targets, and publish archives and
`checksums.txt`.

Verified on macOS with the AquesTalkPlayer for Mac package from the official
site as of October 2026 (app bundle version 1.0):

- `-T` with `-W`, Japanese text and preset arguments, and WAV output to the OS
  temporary directory.
- Synthesis with the built-in `れいむ` and `まりさ` presets, returning
  `200 audio/wav` with 44 kHz, mono, 16-bit audio.
- An unknown preset returns `400 preset_not_found`.
- `VoiceEngineAdapter` from `@aituber-onair/voice` can synthesize through the
  server with its default speed of 1.0, and the returned WAV can be played.

Windows execution, the PowerShell installer, hosted CI/release workflows, and
downloads from a published release remain unverified. Custom preset persistence
and phonetic input also remain unverified with the real player. Automated tests
verify HTTP and process behavior, not voice quality.

## License

aqtpserver is distributed under the [MIT License](LICENSE). It is an unofficial
tool and is not affiliated with AQUEST Corp. That license does not grant rights to AquesTalkPlayer or its data.

### How aqtpserver uses AquesTalkPlayer

- aqtpserver contains no AQUEST code, libraries, DLLs, dictionaries, or voice
  data, and does not use AQUEST's SDKs. It is not an app that embeds and
  redistributes AQUEST's libraries.
- It only starts the AquesTalkPlayer that you downloaded and installed yourself,
  through the command-line interface described in AQUEST's official manuals
  ([Windows](https://www.a-quest.com/products/aquestalkplayer_man.html),
  [macOS](https://www.a-quest.com/products/aquestalkplayer_mac_man.html)). The
  [product page](https://www.a-quest.com/products/aquestalkplayer.html) states
  that this interface can be used for batch processing and for calls from
  external apps.
- It runs on your own computer and accepts connections only from that computer.

[AquesTalkPlayer's terms](https://www.a-quest.com/products/aquestalkplayer.html)
allow free use only for personal non-commercial use. Other uses require the
user to purchase AQUEST's usage license (for commercial content) and set its
license key in the player. The
terms prohibit making the downloaded player available to third parties over a
network; this is why aqtpserver is restricted to loopback. Use it only with your
own installed player and follow the current AQUEST terms.

Consult AQUEST's official [personal use rules](https://www.a-quest.com/licence_free.html),
[license overview](https://www.a-quest.com/licence.html), and
[video and streaming guidance](https://www.a-quest.com/licence_video.html).
A usage license is required for monetized videos or streams, for use by
companies, schools, or other organizations, and for content that promotes
products or services (even when that content earns no advertising revenue).

AQUEST's [license terms](https://www.a-quest.com/licence.html) also prohibit using
generated audio to train AI and distributing, publishing, or allowing third
parties to use that audio, the trained models, or resulting speech synthesis
systems. Users are responsible for checking and following AQUEST's current terms.
