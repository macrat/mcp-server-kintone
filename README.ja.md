# kintone用のMCPサーバー

[kintone](https://www.kintone.com/)用の[Model Context Protocol (MCP)](https://modelcontextprotocol.io/)サーバーです。
このサーバーを使うことで、[Claude Desktop](https://claude.ai/download)などのAIツールを使ってkintoneデータを閲覧・操作できるようになります。

English description is [README.md](README.md).


## 2025-08-28 お知らせ
サイボウズが公式のkintone MCPサーバーをリリースしました！ 🎉
本プロジェクトの役割は果したと考えますので、今後は新規の機能追加は行いません。ただし、公式MCPサーバーが本MCPサーバーが持つ機能をカバーするまではメンテナンスは継続します。
ぜひ公式のMCPサーバーを利用してください！


## 使い方

### 1. mcp-server-kintoneをダウンロードする

[リリースページ](https://github.com/macrat/mcp-server-kintone/releases)から最新のリリースをダウンロードしてください。
ダウンロードした実行ファイルはどこに保存しても構いません。


### 2. Claude DesktopなどのMCPクライアントを設定する

クライアントのマニュアルを見ながら、サーバーへの接続を設定してください。

Claude Desktopで使いたい場合は、以下のファイルを編集してください。
- MacOS/Linux: `~/Library/Application\ Support/Claude/claude_desktop_config.json`
- Windows: `%APPDATA%\Claude\claude_desktop_config.json`

ファイルには以下のような内容を記述します。
ファイルの場所や設定値は、実際の値に置き換えてください。

```json
{
  "mcpServers": {
    "kintone": {
      "command": "C:\\path\\to\\mcp-server-kintone.exe",
      "env": {
        "KINTONE_BASE_URL": "https://<契約ドメイン名>.cybozu.com",
        "KINTONE_USERNAME": "<ユーザー名>",
        "KINTONE_PASSWORD": "<パスワード>",
        "KINTONE_API_TOKEN": "<APIトークン>, <別のAPIトークン>, ...",
        "KINTONE_ALLOW_APPS": "1, 2, 3, ...",
        "KINTONE_DENY_APPS": "4, 5, ..."
      }
    }
  }
}
```

**環境変数**:
- `KINTONE_BASE_URL`: **(必須)** kintoneのベースURLを指定します。
- `KINTONE_USERNAME`: kintoneのユーザー名を指定します。
- `KINTONE_PASSWORD`: kintoneのパスワードを指定します。
- `KINTONE_API_TOKEN`: カンマ区切りでAPIトークンを指定します。
  `KINTONE_USERNAME`と`KINTONE_PASSWORD`のどちらか、または両方を指定する必要があります。
- `KINTONE_ALLOW_APPS`: アクセスを許可するアプリIDのカンマ区切りのリストを指定します。デフォルトでは全てのアプリが許可されます。
- `KINTONE_DENY_APPS`: アクセスを拒否するアプリIDのカンマ区切りのリストを指定します。ALLOW\_APPSよりも優先されます。

設定が完了したら、Claude Desktopを再起動して変更を反映してください。


### 3. 試してみる

これで、AIツールを使ってkintoneのデータを閲覧・操作できるようになりました。

例えば、以下のような指示をAIに出すことができます。
- 「A社に関するプロジェクトの進捗状況を教えて」
- 「Bプロジェクトの進捗を50%に設定して」
- 「遅れているプロジェクトの一覧を表示して」
