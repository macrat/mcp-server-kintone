# kintone用のMCPサーバー

[kintone](https://www.kintone.com/)用の[Model Context Protocol (MCP)](https://modelcontextprotocol.io/)サーバーです。
このサーバーを使うことで、[Claude Desktop](https://claude.ai/download)などのAIツールを使ってkintoneデータを閲覧・操作できるようになります。

English description is [README.md](README.md).


## 使い方

### 1. mcp-server-kintoneをダウンロードする

[リリースページ](https://github.com/macrat/mcp-server-kintone/releases)から最新のリリースをダウンロードしてください。
ダウンロードした実行ファイルはどこに保存しても構いません。


## 2. mcp-server-kintoneの設定ファイルを作る

以下のような設定ファイルを作成してください。

```json
{
    "url": "https://<契約ドメイン>.cybozu.com",
    "username": "<ユーザー名>",
    "password": "<パスワード>",
    "token": "<アプリのトークン1>, <アプリのトークン2>, ...",
    "apps": [
        {
            "id": "<アプリID>",
            "description": "<アプリの説明>",
            "permissions": {
                "read": true,
                "write": false,
                "delete": false
            }
        }
    ]
}
```

**設定パラメータ:**

- `url`: (必須) kintoneのドメインのURL。

- `username`: (オプション) ログイン用のユーザー名。

- `password`: (オプション) ログイン用のパスワード。

- `token`: (オプション) ログイン用のアプリトークン。

- `apps`: (必須) アクセスしたいアプリのリスト。
  - `id`: (必須) アプリID。
  - `description`: (オプション) AIに向けたアプリの説明。
  - `permissions`: (オプション) AIに許可する操作。
    - `read`: (オプション) 読み取り権限。デフォルトは`true`。
    - `write`: (オプション) 書き込み権限。デフォルトは`false`。
    - `delete`: (オプション) 削除権限。デフォルトは`false`。

**注意:**

- kintoneに接続するためには、`username`と`password`または`token`の少なくとも1つが必要です。

- MCPを介して利用したい全てのアプリを `apps` に記載してください。
  誤って機密情報をAIに読み取らせてしまわないように、 `apps` に書かれていないアプリへのアクセスを許可しないようになっています。

実際の設定ファイルは、たとえば以下のようになるはずです。

```json
{
    "url": "https://example.cybozu.com",
    "username": "yuma_shida",
    "password": "password123",
    "apps": [
        {
            "id": "1",
            "description": "お客様の情報が格納されたアプリです。担当者名や連絡先などが乗っています。",
            "permissions": {
                "read": true,
                "write": false,
                "delete": false
            }
        },
        {
            "id": "2",
            "description": "案件の情報が格納されたアプリです。案件の概要や進捗状況などが乗っています。",
            "permissions": {
                "read": true,
                "write": true,
                "delete": false
            }
        }
    ]
}
```


### 3. Claude DesktopなどのMCPクライアントを設定する

クライアントのマニュアルを見ながら、サーバーへの接続を設定してください。

Claude Desktopで使いたい場合は、以下のファイルを編集してください。
- MacOS/Linux: `~/Library/Application\ Support/Claude/claude_desktop_config.json`
- Windows: `%APPDATA%\Claude\claude_desktop_config.json`

ファイルには以下のような内容を記述します。
ファイルの場所は、実際のファイルパスに置き換えてください。

```json
{
  "mcpServers": {
    "kintone": {
      "command": "C:\\path\\to\\mcp-server-kintone.exe",
      "args": [
        "C:\\path\\to\\configuration.json"
      ]
    }
  }
}
```

設定が完了したら、Claude Desktopを再起動して変更を反映してください。


### 4. 試してみる

これで、AIツールを使ってkintoneのデータを閲覧・操作できるようになりました。

例えば、以下のような指示をAIに出すことができます。
- 「A社に関するプロジェクトの進捗状況を教えて」
- 「Bプロジェクトの進捗を50%に設定して」
- 「遅れているプロジェクトの一覧を表示して」
