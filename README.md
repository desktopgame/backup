# backup

`backup` は Go で書かれた、単一バイナリの小さなバックアップ CLI です。

マシン上に散らばったファイルやディレクトリを集めてアーカイブし、ローカルで
圧縮・暗号化してから、別の場所へアップロードします。想定している流れは次の
とおりです。

```text
設定したパス -> tar -> zstd -> age -> ストレージ
```

すべてストリーム処理で行い、中間ファイル（`.tar` / `.tar.zst` / 平文）は
作りません。外部コマンドに依存しない単一の実行ファイルとして動作します
（`tar`、`zstd`、`age`、`openssl`、`scp` などは不要です）。

典型的な用途は、PC や VPS から、FTP でフォルダを公開している Android
端末へのバックアップです。端末側は暗号化済みのスナップショットを置くだけ
で、復号鍵を持つ必要はありません。

## 特徴

- 複数のバックアップ元（ディレクトリ／単一ファイル）を、それぞれ任意の名前
  でアーカイブ直下に配置できます。
- `~/.backupignore` による gitignore 風の除外ルール。
- tar + zstd + age を Go で実装したストリーミング処理。
- データはマシンから出る前に暗号化。ストレージが平文を見ることはありません。
- 安全なアップロード: `<name>.tar.zst.age.part` として書き込み、完了後にのみ
  最終名へリネームします。
- 日次／週次／月次のローテーション（`rotation`）と `prune --dry-run`。
- 同じバイナリで復元。age の秘密鍵は別途指定します。
- ストレージは `local` と `ftp` に対応。
- Windows / Linux（amd64 / arm64）。

## 必要なもの

- ビルドには Go 1.25 以降。
- age の X25519 鍵ペア。`backup keygen` で生成できます（後述）。

## ビルド

```sh
go build -o backup .
```

クロスコンパイル:

```sh
GOOS=linux   GOARCH=amd64 go build -o backup-linux-amd64 .
GOOS=linux   GOARCH=arm64 go build -o backup-linux-arm64 .
GOOS=windows GOARCH=amd64 go build -o backup.exe .
```

## クイックスタート

### 1. 鍵ペアを生成する

```sh
backup keygen -o identity.txt
```

age の秘密鍵（identity）を `identity.txt` に書き出し、公開鍵（recipient、
`age1...`）をファイル内のコメントとして表示します。

- 設定ファイルに書くのは **recipient** です。公開情報なので安全に置けます。
- **identity** はバックアップ元のマシンに置かず、復元時にだけ使います。
  `backup run` に秘密鍵は不要です。

### 2. 設定ファイルを作る

既定の場所は `~/.backup`（Windows では `C:\Users\<user>\.backup`）です。

```toml
machine = "desktop"
recipient = "age1xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"

[storage]
type = "local"
path = 'C:\Backups\store'

[rotation]
daily = 7
weekly = 4
monthly = 6

[[path]]
name = "vps"
source = 'C:\Users\example\Work\VPS'

[[path]]
name = "pubmir"
source = 'C:\Users\example\Work\Repository\pubmir'

[[path]]
name = "ssh-config"
source = 'C:\Users\example\.ssh\config'
```

Windows のパスは TOML のリテラル文字列（`'...'`）が楽です。基本文字列を使う
場合はバックスラッシュをエスケープします（`"C:\\Users\\example"`）。

リポジトリ直下の `.backup.example` をコピーして書き換えることもできます。

### 3. ignore ファイルを作る

既定の場所は `~/.backupignore` です。

```text
node_modules/
.venv/
venv/
__pycache__/
dist/
build/
*.pyc
```

### 4. 検証して実行する

```sh
backup check
backup run
backup list
```

`check` は設定、パス、recipient、ストレージ接続（小さな書き込み／削除テスト
を含む）を、最初の本番バックアップの前に検証します。

## コマンド

```text
backup run                 新しいバックアップを作成してアップロード
backup check               設定とストレージ接続を検証
backup list                利用可能なスナップショットを一覧表示
backup prune [--dry-run]   保持ルールを適用
backup restore <snapshot>  スナップショットを復元
backup keygen [-o file]    age の identity と recipient を生成
backup version             バージョンを表示
```

共通フラグ:

| フラグ | 説明 |
| --- | --- |
| `-c`, `--config <path>` | 設定ファイル（既定 `~/.backup`） |
| `--ignore <path>` | ignore ファイル（既定 `~/.backupignore`） |
| `-v`, `--verbose` | 詳細出力 |

`restore` のフラグ:

| フラグ | 説明 |
| --- | --- |
| `-o`, `--output <dir>` | 出力先ディレクトリ（既定 `.`） |
| `--identity <path>` | age の identity ファイル。複数回指定可 |

`--identity` を省略した場合は環境変数 `BACKUP_AGE_IDENTITY` を使います。
複数指定する場合は OS のパス区切り（Windows は `;`、それ以外は `:`）で
区切ります。

終了コード: `0` 成功、`1` 実行時／設定エラー、`2` 使い方のエラー。

## 設定リファレンス

### トップレベル

| キー | 説明 |
| --- | --- |
| `machine` | スナップショット名の接頭辞（既定: ホスト名を整形したもの） |
| `recipient` | age の recipient を1つ |
| `recipients` | age の recipient のリスト（すべて使用） |
| `[storage]` | ストレージバックエンド |
| `[rotation]` | 保持ポリシー |
| `[[path]]` | バックアップ元（複数可） |

### `[[path]]`

| キー | 説明 |
| --- | --- |
| `name` | アーカイブ直下の名前。重複不可 |
| `source` | バックアップするディレクトリまたは単一ファイル |

相対 `source` は設定ファイルのあるディレクトリを基準に解決されます。設定した
`name` がアーカイブのルートになるため、元マシンの絶対パスがアーカイブ構造に
混ざらず、復元先のマシン構成に依存しません。

名前の重複は拒否されます。存在しないパスは `check` が失敗し、`run` 中に
消えたパスは不完全なバックアップを黙って作らずエラーになります。

### `[storage]`

`type = "local"`

| キー | 説明 |
| --- | --- |
| `path` | 保存先ディレクトリ（無ければ作成） |

`type = "ftp"`

| キー | 説明 |
| --- | --- |
| `host` | サーバーのホスト |
| `port` | サーバーのポート（既定 21） |
| `username` | FTP ユーザー |
| `password` | FTP パスワード（環境変数の利用を推奨） |
| `path` | スナップショットを置くリモートディレクトリ |

FTP の `path` はサーバーが公開している構成に依存し、アプリごとに異なります。
単一フォルダを FTP のルートとして公開するアプリなら `path = "/"`（または
`/Backup` のようなサブフォルダ）を使います。Primitive FTPd のように実際の
ファイルシステムを公開するアプリでは `/storage/emulated/0/Backup` が使えます。
確認するには、何か FTP クライアントで接続してディレクトリを一覧するか、
`path = "/"` にして `backup check` を実行してください。

### 環境変数の展開

`recipient`、`recipients`、`[storage]` の各文字列、各 `[[path]]` の
`name`/`source` で `${VAR}` / `$VAR` が展開されます。これにより秘密情報を
設定ファイルに直書きせずに済みます。

```toml
[storage]
password = "${BACKUP_FTP_PASSWORD}"
```

`local` ストレージの `path` と `source` では、先頭の `~` がホームディレクトリ
に展開されます。

## ignore ルール

`~/.backupignore` は gitignore 風の構文で、設定したすべてのパスに対して
再帰的に適用されます。

- 空行と `#` コメントは無視されます。
- `!pattern` は以前に除外したパスを再度含めます。
- `pattern/` はディレクトリのみに一致します。
- 先頭の `/` はパターンをバックアップルートに固定します。
- `/` を含むパターンはルートに固定され、含まないパターンは任意の深さの
  ベース名に一致します。
- `*`、`?`、`[...]`、`**` のワイルドカードに対応します。

`.gitignore` は意図的に適用しません。Git で無視されているからといって、
災害復旧用バックアップから除外すべきとは限らないためです。`.env`、ローカル
設定、SQLite データベース、認証情報、証明書などは、`.backupignore` で明示
的に除外しない限りバックアップされます。アーカイブはアップロード前に暗号化
されるため、機密ファイルを含めて問題ありません。

## スナップショット

スナップショット名は次の形式です。

```text
<machine>-YYYYMMDD-HHMMSS.tar.zst.age
```

例: `desktop-20260921-170000.tar.zst.age`。タイムスタンプはローカル時刻で、
辞書順に並びます。

アップロードは `<name>.tar.zst.age.part` として行われ、転送完了後にのみ最終名
へリネームされます。失敗したバックアップが不完全なスナップショットを公開
することはなく、既存の有効なスナップショットを削除することもありません。

## ローテーション

```toml
[rotation]
daily = 7
weekly = 4
monthly = 6
```

保持ルールは、古くなるにつれて間引いた代表スナップショットを残します。

- 直近 `daily` 日は 1 日につき 1 つ
- その前の `weekly` 週は 1 週につき 1 つ
- その前の `monthly` か月は 1 か月につき 1 つ

同じ期間内では最も新しいスナップショットを残します。全体で最も新しい
スナップショットは常に保持されます。スナップショット命名規則に一致しない
ファイル（`.part` を含む）には一切触れません。`backup prune --dry-run` は
削除対象を表示するだけで、実際には削除しません。

`[rotation]` セクションが無い場合は上記の既定値が使われます。

## 復元

```sh
backup restore desktop-20260921-170000.tar.zst.age --output ./restore --identity identity.txt
```

処理は逆順で、ストレージ -> age 復号 -> zstd 展開 -> tar 展開となります。
展開時は絶対パスや出力先ディレクトリの外へ出るエントリを拒否し、書き込み中
にシンボリックリンクをたどりません。

## セキュリティモデル

ストレージは失われたり、盗まれたり、コピーされたりする可能性があるものと
仮定します。バックエンドが受け取るのは age で暗号化されたデータだけで、
復号鍵は不要です。バックアップ元が侵害されても、過去のバックアップを復号
する鍵はそこにありません。

FTP の認証情報は FTP 自体では保護されないため、FTP は信頼できるローカル
ネットワークか VPN 上でのみ使ってください。ペイロードはすでに暗号化されて
います。トランスポートの強化が必要になれば SFTP を追加できます。独自の
暗号は実装していません。

## ログと進捗

既定の出力は簡潔です。

```text
Scanning 3 paths...
12,482 files, 184.3 MiB
Compressing + encrypting...
Uploading desktop-20260921-170000.tar.zst.age...
Uploaded 31.8 MiB
Rotation: removed 2 old snapshots
Backup complete.
```

stdout が端末のときは、転送レート付きの進捗行を表示します。認証情報、
identity、秘密の環境変数値は決して出力しません。

`backup run -v` では、解決後の設定、machine 名、ストレージ種別、各バック
アップ元パスをスキャン前に追加表示します。

## 対応プラットフォーム

- Windows amd64 / arm64
- Linux amd64 / arm64

設定内のパスは Windows / Linux のネイティブ構文を受け付けます。ファイル
システムとネットワークには Go の API を直接使い、プラットフォーム固有の
シェル挙動を避けています。

## 開発

```sh
go test ./...
go vet ./...
```

テストは、アーカイブのパス処理、ignore の一致、ローテーションの選択、復元の
安全性（パストラバーサル、絶対パス、シンボリックリンク脱出）をカバーします。

パッケージ構成:

```text
main.go                 エントリポイント
internal/app            CLI コマンド
internal/config         設定の読み込みと検証
internal/ignore         gitignore 風マッチャ
internal/archive        バックアップ元の列挙と tar 書き出し
internal/pipeline       tar -> zstd -> age のストリーム
internal/storage        ストレージ interface、local / ftp バックエンド
internal/rotation       日次／週次／月次の保持
internal/restore        復号・展開と安全な取り出し
internal/snapshot       スナップショット名の生成と解析
```
