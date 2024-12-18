# MCP server for kintone

[Model Context Protocol (MCP)](https://modelcontextprotocol.io/) server for [kintone](https://kintone.cybozu.co.jp/).
This server allows you to explore and manipulate kintone data using AI tools such as [Claude Desktop](https://claude.ai/download)!


## Usage

### 1. Install

Pre-build binaries are not available yet. Please build from source.


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
            "description": "<your-app-description>"
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
  - `permissions`: (optional) Permissions for the app.
    - `read`: (optional) Read permission. Default is `true`.
    - `write`: (optional) Write permission. Default is `false`.
    - `delete`: (optional) Delete permission. Default is `false`.

**Notes:**

- For connect to kintone, at least of `username` and `password` or `token` is required.

- Please make sure that all apps you want to access are included in the `apps` list.
  For security reasons, this server does not allow clients to access apps that are not included in the `apps` list.


### 3. Configure MCP client like Claude Desktop

Configure your client to connect to the server.

For Claude Desktop, you can use the following configuration:

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


### 4. Start to use

Now you can interact with kintone using your own AI tools!
(you may need to restart your tools before the changes take effect)
