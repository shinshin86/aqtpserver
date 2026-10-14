# aqtpserver

[English](README.md)

インストール済みの AquesTalkPlayer を HTTP API から呼び出すローカルサーバーです。
OpenAI 互換の `POST /v1/audio/speech` で WAV を返し、
`@aituber-onair/voice` の `openaiCompatible` エンジンから利用できます。

AquesTalkPlayer 本体、音声合成エンジン、辞書、音声データの同梱・ダウンロード・
再配布は行いません。AquesTalkPlayer Web は対象外です。

## AquesTalkPlayer について

[AquesTalkPlayer](https://www.a-quest.com/products/aquestalkplayer.html) は、
株式会社アクエスト（AQUEST）が開発する音声合成アプリです。AquesTalk1・AquesTalk2・
AquesTalk10 と、日本語の読み上げに使う AqKanji2Koe を組み合わせ、
「ゆっくりボイス」として知られる声などを利用できます。Windows・macOS・Web 版があり、
aqtpserver が使用するのは Windows・macOS のデスクトップ版です。

音声合成技術を開発・提供するアクエスト社に感謝します。
AquesTalkPlayer は上記の公式製品ページからダウンロードし、同社の利用条件に従って
ご利用ください。

## 利用前の準備

- macOS 14 以降、または Windows が必要です。サーバーの配布対象は両 OS の
  amd64 / arm64 です。AquesTalkPlayer 自体も利用環境で動作する必要があります。
- [AquesTalkPlayer](https://www.a-quest.com/products/aquestalkplayer.html) を
  ご自身でインストールし、一度 GUI を開いて設定してください。
- AquesTalkPlayer に最初から用意されている `れいむ`、`まりさ` などのプリセットを
  そのまま使えます。
  GUI にあるプリセット名なら、自分で作成したものも `voice` に指定できます。
  声種・話速・音量・高さを変える場合は、GUI で編集するか `slow` などのプリセットを
  追加してください。個別指定する CLI 引数はありません。
  aqtpserver はプリセットファイルを読み書きしません。

## インストール

バージョンのリリース後は、[GitHub Releases](https://github.com/shinshin86/aqtpserver/releases)
からバイナリと SHA-256 チェックサムを取得できます。初回リリース前はソースから
ビルドしてください。

macOS:

```sh
curl -fsSL https://raw.githubusercontent.com/shinshin86/aqtpserver/main/install.sh -o install.sh
# 内容を確認してから実行してください。
bash install.sh
# バージョンと保存先を指定する場合
BIN_DIR="$HOME/.local/bin" bash install.sh vX.Y.Z
export PATH="$HOME/.local/bin:$PATH"
```

Windows（PowerShell）:

```powershell
Invoke-WebRequest https://raw.githubusercontent.com/shinshin86/aqtpserver/main/install.ps1 -OutFile install.ps1
# 内容を確認してから実行してください。
.\install.ps1
# バージョンを指定する場合
.\install.ps1 -Version vX.Y.Z
```

`vX.Y.Z` は実際のリリースタグに置き換えてください。両スクリプトともチェックサムを
検証し、既存のバイナリがあれば `.bak` に保存します。標準の保存先は macOS が
`~/.local/bin`、Windows が `%LOCALAPPDATA%\Programs\aqtpserver` です。
Windows ではユーザーの PATH に追加するため、実行後にターミナルを開き直してください。
保存先は `BIN_DIR` で変更できます。AquesTalkPlayer 本体はインストールされません。

ソースからビルドする場合（Go 1.23.2 以降）:

```sh
git clone https://github.com/shinshin86/aqtpserver.git
cd aqtpserver
go build -trimpath -o aqtpserver .
```

Windows では `go build -trimpath -o aqtpserver.exe .` としてください。
Linux でもビルドとテストはできますが、サーバーの起動には対応していません。

## 起動

macOS の標準実行パスは `/Applications/AquesTalkPlayer.app/Contents/MacOS/AquesTalkPlayer`
です。

```sh
aqtpserver --default-preset 'れいむ'
```

Windows では、公式パッケージを展開した場所の実行ファイルを指定してください。

```powershell
aqtpserver --aquestalkplayer-path "C:\Apps\AquesTalkPlayer\AquesTalkPlayer.exe" --default-preset "れいむ"
```

標準の接続先は `http://127.0.0.1:20203` です。実行ファイルが見つからない場合は、
設定方法を表示して起動を中止します。音声合成は一度に 1 件ずつ実行し、待ち時間も
タイムアウトに含めます。リクエストごとに一時ディレクトリを作り、成功・失敗・
タイムアウトのいずれの場合も削除します。期限切れやキャンセル時は実行中の
プロセスを終了します。

| 引数 | 初期値 | 内容 |
| --- | --- | --- |
| `--host` | `127.0.0.1` | ループバックのみ。`localhost`、`::1` も指定可能 |
| `--port` | `20203` | HTTP ポート。1〜65535 |
| `--aquestalkplayer-path` | macOS は上記パス、Windows は指定必須 | `.app` フォルダではなく実行ファイルを指定 |
| `--default-preset` | 空 | `voice` 省略時・空文字時のプリセット |
| `--timeout` | `30s` | 待ち時間を含む正の期間。例: `1m` |
| `--max-input-chars` | `1000` | 入力の最大 Unicode コードポイント数。正の整数 |
| `--allowed-origin` | 空 | 追加で許可するオリジン。繰り返し指定または空白区切り |
| `--version` | | バージョンを表示して終了 |
| `--help` | | 引数の説明を表示 |

`localhost` は DNS を使わず `127.0.0.1` にバインドします。ループバック以外への
バインドと、ループバック以外の HTTP `Host` ヘッダーを拒否します。
CORS は `http://localhost:5173`、`http://127.0.0.1:3000`、`http://[::1]:3000` などの
HTTP ループバックオリジンと `app://` を許可します。追加する場合は完全一致で指定します。

```sh
aqtpserver --allowed-origin https://your-app.example
```

許可されていないオリジンには、合成を実行せず 403 を返します。プリフライトは
`Content-Type` と `Authorization` に対応します。API キーは不要で、Authorization の
値は使用しません。LAN 公開用の設定はありません。プロキシ・トンネル・ポート転送で
他人にサービスを提供しないでください。

## API

### GET /

```sh
curl http://127.0.0.1:20203/
```

```json
{"engine":"aqtpserver","model":"aquestalkplayer"}
```

OpenAI 互換クライアント向けにサーバー情報を返します。AITuber OnAir の React
サンプルは、この応答を使って AquesTalkPlayer 固有の案内を表示します。
この情報を返すのはパスが `/` の場合のみで、未知のパスには 404 を返します。
`HEAD /` は JSON の Content-Type と 200 を返し、本文は含めません。`OPTIONS /` は
GET・HEAD の CORS プリフライトに対応し、他のメソッドには 405 を返します。
Host 検証・CORS・リクエストログには、他の API と同じルールを適用します。

### POST /v1/audio/speech

```sh
curl --fail-with-body http://127.0.0.1:20203/v1/audio/speech \
  -H 'Content-Type: application/json' \
  -d '{"model":"aquestalkplayer","input":"こんにちは。","voice":"れいむ","response_format":"wav","speed":1.0}' \
  -o speech.wav
```

PowerShell では `curl.exe` を使い、UTF-8 の JSON ファイルを
`--data-binary @request.json` で指定すると引用符の扱いによる問題を避けられます。
成功時は `200 audio/wav` を返します。

| メンバー | 指定できる値 |
| --- | --- |
| `input` | 必須。空白だけではない文字列。標準で 1000 Unicode コードポイントまで |
| `voice` | GUI のプリセット名。省略・空文字なら `--default-preset` を使用 |
| `model` | `aquestalkplayer` または省略 |
| `response_format` | `wav` または省略 |
| `speed` | 数値の `1` / `1.0` または省略。実際の話速は GUI のプリセットで変更 |
| `instructions` | 空文字または省略。声の設定はプリセットを使用 |

`voice` と `--default-preset` が両方空なら、CLI のプリセット引数を省略します。
この場合、AquesTalkPlayer は GUI で最後に選択したプリセットを使います。
`#>` で始まる音声記号列もそのまま渡します。実行時にシェルは使用しません。
CLI の仕様は公式の [Windows マニュアル](https://www.a-quest.com/products/aquestalkplayer_man.html)
と [macOS マニュアル](https://www.a-quest.com/products/aquestalkplayer_mac_man.html) を参照してください。

未知のメンバー、非対応の値、型の不一致、明示的な `null` は 400 を返します。
`input` と `voice` の NUL 文字も拒否します。リクエストは UTF-8 JSON とし、
`Content-Type: application/json` を指定してください。本文には文字数制限とは別に
1 MiB の上限があります。入力文字列の前後の空白除去や正規化は行いません。

エラー形式:

```json
{"error":{"message":"...","type":"invalid_request_error","param":"voice","code":"preset_not_found"}}
```

特定の項目に関係しないエラーでは `param` は `null` です。プリセット不正・空文・
長すぎる入力は 400、その他の合成失敗は 500、起動後の実行ファイル消失は 503、
タイムアウトは 504 を返します。キャンセル時は接続が残っていれば 408 を返します。
Content-Type の不一致は 415、本文サイズ超過は 413 です。
ログにはメソッド・既知のルート・ステータス・処理時間を記録します。
入力本文・クエリ文字列・CLI 出力は記録しません。

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

実行ファイルの存在を確認するもので、音声合成の成功までは確認しません。
起動後にファイルが削除された場合も HTTP 200 ですが、`status` は `degraded`、
`executable_exists` は `false` になります。プリセットの保存形式が非公開のため、
プリセット一覧 API は設けていません。

## @aituber-onair/voice から使う

```ts
import { VoiceEngineAdapter } from '@aituber-onair/voice';

const voice = new VoiceEngineAdapter({
  engineType: 'openaiCompatible',
  openAiCompatibleApiUrl: 'http://127.0.0.1:20203/v1/audio/speech',
  openAiCompatibleModel: 'aquestalkplayer',
  speaker: 'れいむ',
  openAiCompatibleResponseFormat: 'wav',
});

await voice.speakText('こんにちは。');
```

`VoiceService` は型だけの export です。インスタンスの作成には
`VoiceEngineAdapter` を使ってください。Adapter は指定先に直接 POST するため、
URL は `/v1/audio/speech` まで含めます。
`resolveOpenAICompatibleSpeechEndpoint` を使うサンプルアプリなら、
`http://127.0.0.1:20203` の入力から接続先を補完できます。
`openAiCompatibleSpeed` は 1 のままにし、instructions は空にしてください。
話速を変える場合は別のプリセットを作り、その名前を `speaker` に指定します。
サーバーのタイムアウトを延ばす場合は、クライアント側の
`openAiCompatibleTimeoutMs` も調整してください。

Node.js での再生には、追加の再生ライブラリ（`speaker` または `play-sound`）か
`onPlay` コールバックが必要です。どちらもない場合、合成は行われますが再生されません。

## 開発・検証

```sh
gofmt -l .
go vet ./...
go test ./...
go test -race ./...
```

テストは偽の実行処理と Go のテスト用実行ファイルを使うため、AquesTalkPlayer の
ファイルは不要です。CI は Ubuntu・macOS・Windows で vet とテストを実行します。
`vX.Y.Z` 形式のタグを作成すると、CI 後に 4 種類の配布バイナリをビルドし、
アーカイブと `checksums.txt` を公開します。

2026年10月時点で公式サイトから配布されている AquesTalkPlayer for Mac
（アプリバンドルのバージョンは 1.0）で、以下を確認しています。

- `-T` と `-W` の併用、日本語の本文・プリセット名、OS の一時ディレクトリへの WAV 出力。
- 同梱プリセット `れいむ` と `まりさ` での合成。
  `200 audio/wav` で 44 kHz・モノラル・16 bit の音声を取得できます。
- 存在しないプリセット名に対する `400 preset_not_found`。
- `@aituber-onair/voice` の `VoiceEngineAdapter` から、既定の speed 1.0 での合成と、
  返された WAV の再生。

Windows での実行、PowerShell インストーラー、GitHub Actions 上の CI・リリース処理、
公開リリースからのダウンロードは未検証です。独自プリセットの保存・音声記号列も
実アプリでは未検証です。自動テストの対象は HTTP とプロセス制御であり、
音声の品質は検証しません。

## ライセンス

aqtpserver は [MIT License](LICENSE) で公開しています。株式会社アクエストの
公式プロジェクトではなく、同社とは関係のない非公式のツールです。このライセンスは AquesTalkPlayer 本体やデータの利用権を含みません。

### aqtpserver による AquesTalkPlayer の利用方法

- aqtpserver には、アクエスト社のコード・ライブラリ・DLL・辞書・音声データは
  一切含まれておらず、同社の SDK も使用していません。同社のライブラリを
  組み込んで配布するアプリではありません。
- 利用者が自身でダウンロード・インストールした AquesTalkPlayer を、同社の公式
  マニュアル（[Windows 版](https://www.a-quest.com/products/aquestalkplayer_man.html)、
  [macOS 版](https://www.a-quest.com/products/aquestalkplayer_mac_man.html)）に
  記載されたコマンドラインで起動するだけです。このコマンドラインについて、
  [製品ページ](https://www.a-quest.com/products/aquestalkplayer.html)には
  「バッチ処理や外部アプリからの呼び出しも可能です」と記載されています。
- 利用者自身の PC 上で動作し、同じ PC からの接続のみを受け付けます。

[AquesTalkPlayer の利用条件](https://www.a-quest.com/products/aquestalkplayer.html) では、
無償利用は個人の非営利利用に限られます。それ以外の利用では、利用者自身が「使用ライセンス
（商用コンテンツ向け）」を購入し、ライセンスキーを設定してください。ダウンロードしたアプリを
ネットワーク経由で第三者に使用させることは禁止されています。
そのため、このサーバーはループバック接続に限定しています。
ご自身のインストール済みアプリで使用し、AQUEST の最新の利用条件に従ってください。

公式の [個人利用規定](https://www.a-quest.com/licence_free.html)、
[ライセンス案内](https://www.a-quest.com/licence.html)、
[動画・配信での利用案内](https://www.a-quest.com/licence_video.html) を確認してください。
収益化する動画・配信、会社・学校・団体での利用、商品やサービスの宣伝を目的とする
コンテンツ（広告収入がない場合を含む）には、使用ライセンスが必要です。

また、[公式のライセンス条件](https://www.a-quest.com/licence.html) では、生成した音声を
AI 等の学習に使い、その音声データ・学習済みモデル・新たな音声合成システムを
第三者に配布・公開・使用させることを禁止しています。
最新の利用条件を確認し、遵守する責任は利用者にあります。
