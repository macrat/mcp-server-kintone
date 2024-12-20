# MCP server for kintone

[Model Context Protocol (MCP)](https://modelcontextprotocol.io/) server for [kintone](https://kintone.cybozu.co.jp/).
This server allows you to explore and manipulate kintone data using AI tools such as [Claude Desktop](https://claude.ai/download)!

Japanese version: 日本語の説明は[README.ja.md](README.ja.md)にあります。


## Usage

### 1. Install

Download the latest release from the [rleases page](https://github.com/macrat/mcp-server-kintone/releases).
You can place the executable file anywhere you like.


### 2. Configure mcp-server-kintone

Create a configuration file like below:

```json
{
    "url": "https://<your-domain>.cybozu.com",
    "username": "<your-username>",
    "password": "<your-password>",
    "token": "<your-app-token-1>, <your-app-token-2>, ...",
    "apps": [
        {
            "id": "<your-app-id>",
            "description": "<your-app-description>",
            "permissions": {
                "read": true,
                "write": false,
                "delete": false
            }
        }
    ]
}
```

**Configuration parameters:**

- `url`: (required) URL of your kintone domain.

- `username`: (optional) Username for login.

- `password`: (optional) Password for login.

- `token`: (optional) App tokens for login.

- `apps`: (required) List of apps you want to access.
  - `id`: (required) App ID.
  - `description`: (optional) App description for AI.
  - `permissions`: (optional) Permissions for AI.
    - `read`: (optional) Read permission. Default is `true`.
    - `write`: (optional) Write permission. Default is `false`.
    - `delete`: (optional) Delete permission. Default is `false`.

**Notes:**

- For connect to kintone, at least of `username` and `password` or `token` is required.

- Please make sure that all apps you want to access are included in the `apps` list.
  For security reasons, this server does not allow clients to access apps that are not included in the `apps` list.


For example, your configuration file should look like this:

```json
{
    "url": "https://example.cybozu.com",
    "username": "alice",
    "password": "password",
    "apps": [
        {
            "id": "1",
            "description": "An app that stores information about customers. It contains the name of the person in charge and contact information.",
            "permissions": {
                "read": true,
                "write": false,
                "delete": false
            }
        },
        {
            "id": "2",
            "description": "An app that stores information about projects. It contains an overview of the project and its progress.",
            "permissions": {
                "read": true,
                "write": true,
                "delete": false
            }
        }
    ]
}
```


### 3. Configure MCP client like Claude Desktop

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
      "args": [
        "C:\\path\\to\\configuration.json"
      ]
    }
  }
}
```

You may need to restart Claude Desktop to apply the changes.


### 4. Start to use

Now you can interact with kintone using your own AI tools!

For example, you can say:
- "What is the latest status of Customer A's project?"
- "Update the progress of Project B to 50%."
- "Show me the projects that are behind schedule."
