# MCP server for kintone

[Model Context Protocol (MCP)](https://modelcontextprotocol.io/) server for [kintone](https://kintone.cybozu.co.jp/).
This server allows you to explore and manipulate kintone data using AI tools such as [Claude Desktop](https://claude.ai/download)!

Japanese version: 日本語の説明は[README.ja.md](README.ja.md)にあります。


## Usage

### 1. Install

Download the latest release from the [release page](https://github.com/macrat/mcp-server-kintone/releases).
You can place the executable file anywhere you like.


### 2. Configure MCP client like Claude Desktop

Configure your client to connect to the server.

For Claude Desktop, please edit file below:
- MacOS/Linux: `~/Library/Application\ Support/Claude/claude_desktop_config.json`
- Windows: `%APPDATA%\Claude\claude_desktop_config.json`

Add the following configuration to the `mcpServers` section:

```json
{
  "mcpServers": {
    "kintone": {
      "command": "C:\\path\\to\\mcp-server-kintone.exe",
      "env": {
        "KINTONE_BASE_URL": "https://<domain>.cybozu.com",
        "KINTONE_USERNAME": "<your username>",
        "KINTONE_PASSWORD": "<your password>",
        "KINTONE_API_TOKEN": "<your api token>, <another api token>, ...",
        "KINTONE_ALLOW_APPS": "1, 2, 3, ...",
        "KINTONE_DENY_APPS": "4, 5, ..."
      }
    }
  }
}
```

**Environment variables**:
- `KINTONE_BASE_URL`: **(Required)** The base URL of your kintone.
- `KINTONE_USERNAME`: Your username for kintone.
- `KINTONE_PASSWORD`: Your password for kintone.
- `KINTONE_API_TOKEN`: Comma separated API token for kintone.
  You need to set either `KINTONE_USERNAME` and `KINTONE_PASSWORD` or `KINTONE_API_TOKEN`.
- `KINTONE_ALLOW_APPS`: A comma-separated list of app IDs that you want to allow access. In default, all apps are allowed.
- `KINTONE_DENY_APPS`: A comma-separated list of app IDs that you want to deny access. The deny has a higher priority than the allow.

You may need to restart Claude Desktop to apply the changes.


### 3. Start to use

Now you can interact with kintone using your own AI tools!

For example, you can say:
- "What is the latest status of Customer A's project?"
- "Update the progress of Project B to 50%."
- "Show me the projects that are behind schedule."
