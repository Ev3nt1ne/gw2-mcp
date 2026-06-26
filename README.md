# GW2 MCP Server

[![Add MCP Server gw2-mcp to LM Studio](https://files.lmstudio.ai/deeplink/mcp-install-light.svg#gh-light-mode-only)](https://lmstudio.ai/install-mcp?name=gw2-mcp&config=eyJjb21tYW5kIjoiZG9ja2VyIiwiYXJncyI6WyJydW4iLCItLXJtIiwiLWkiLCJhbHl4cGluay9ndzItbWNwOnYxIl19#gh-light-mode-only)
[![Add MCP Server gw2-mcp to LM Studio](https://files.lmstudio.ai/deeplink/mcp-install-dark.svg#gh-dark-mode-only)](https://lmstudio.ai/install-mcp?name=gw2-mcp&config=eyJjb21tYW5kIjoiZG9ja2VyIiwiYXJncyI6WyJydW4iLCItLXJtIiwiLWkiLCJhbHl4cGluay9ndzItbWNwOnYxIl19#gh-dark-mode-only)

A Model Context Provider (MCP) server for Guild Wars 2 that bridges Large Language Models (LLMs) with Guild Wars 2 data sources.

## Features

- **Wiki Search**: Search and retrieve content from the Guild Wars 2 wiki
- **Wallet Information**: Access user wallet and currency data via GW2 API
- **Chat Link Decode/Encode**: Decode/encode `[&...]` chat links — build templates, skills, traits, items, recipes, achievements, map points of interest
- **Generic API Lookup**: Query any allow-listed public `/v2/` collection by ID, no API key needed
- **Achievement Search**: Case-insensitive name search over the achievement group/category hierarchy
- **Smart Caching**: Efficient caching with appropriate TTL for static and dynamic data
- **Rate Limiting**: Respectful API usage with built-in rate limiting (TODO)
- **Extensible Architecture**: Modular design for easy feature additions

## Requirements

- Go 1.24 or higher
- Guild Wars 2 API key (for wallet functionality)

## Installation

1. Clone the repository:
```bash
git clone https://github.com/AlyxPink/gw2-mcp.git
cd gw2-mcp
```

2. Install dependencies:
```bash
go mod tidy
```

3. Build the server:
```bash
go build -o gw2-mcp ./cmd/server
```

## Usage

### Running the Server

[![Add MCP Server gw2-mcp to LM Studio](https://files.lmstudio.ai/deeplink/mcp-install-light.svg#gh-light-mode-only)](https://lmstudio.ai/install-mcp?name=gw2-mcp&config=eyJjb21tYW5kIjoiZG9ja2VyIiwiYXJncyI6WyJydW4iLCItLXJtIiwiLWkiLCJhbHl4cGluay9ndzItbWNwOnYxIl19#gh-light-mode-only)
[![Add MCP Server gw2-mcp to LM Studio](https://files.lmstudio.ai/deeplink/mcp-install-dark.svg#gh-dark-mode-only)](https://lmstudio.ai/install-mcp?name=gw2-mcp&config=eyJjb21tYW5kIjoiZG9ja2VyIiwiYXJncyI6WyJydW4iLCItLXJtIiwiLWkiLCJhbHl4cGluay9ndzItbWNwOnYxIl19#gh-dark-mode-only)

The MCP server communicates via stdio (standard input/output):

```bash
./gw2-mcp
```

You can configure Claude Desktop, LM Studio, or other LLM tools to interact with the server using this configuration:
```json
{
  "mcpServers": {
    "gw2-mcp": {
      "command": "docker",
      "args": [
        "run",
        "--rm",
        "-i",
        "alyxpink/gw2-mcp:v1"
      ]
    }
  }
}
```

### MCP Tools

The server provides the following tools for LLM interaction:

#### 1. Wiki Search (`wiki_search`)

Search the Guild Wars 2 wiki for information.

**Parameters:**
- `query` (required): Search query string
- `limit` (optional): Maximum number of results (default: 5)

**Example:**
```json
{
  "tool": "wiki_search",
  "arguments": {
    "query": "Dragon Bash",
    "limit": 3
  }
}
```

#### 2. Get Wallet (`get_wallet`)

Retrieve user's wallet information including all currencies.

**Parameters:**
- `api_key` (required): Guild Wars 2 API key with account scope

**Example:**
```json
{
  "tool": "get_wallet",
  "arguments": {
    "api_key": "YOUR_GW2_API_KEY"
  }
}
```

#### 3. Get Currencies (`get_currencies`)

Get information about Guild Wars 2 currencies.

**Parameters:**
- `ids` (optional): Array of specific currency IDs to fetch

**Example:**
```json
{
  "tool": "get_currencies",
  "arguments": {
    "ids": [1, 2, 3]
  }
}
```

#### 4. Chat Link Decode (`chatlink_decode`)

Decode a Guild Wars 2 chat link (`[&...]` code) into its structured contents — build templates, skills, traits, items, recipes, achievements, or map points of interest.

**Parameters:**
- `code` (required): The chat link code, e.g. `"[&AgEAAAA=]"`
- `resolve` (optional): Resolve IDs to human-readable names via the public GW2 API (default: `false`). Name resolution is best-effort — a failed lookup is reported in `resolve_warnings` rather than failing the whole decode.

**Example:**
```json
{
  "tool": "chatlink_decode",
  "arguments": {
    "code": "[&AgEAAAA=]",
    "resolve": true
  }
}
```

#### 5. Chat Link Encode (`chatlink_encode`)

Encode a skill/trait/item/recipe/achievement/map ID into a chat link (`[&...]` code). Build templates aren't supported here — only single-ID links.

**Parameters:**
- `link_type` (required): One of `skill`, `trait`, `item`, `recipe`, `achievement`, `map`
- `id` (required): The public GW2 API ID for the skill/trait/item/recipe/achievement/map point
- `quantity` (optional): Stack size to encode (only meaningful for `link_type: "item"`; default: `1`)

**Example:**
```json
{
  "tool": "chatlink_encode",
  "arguments": {
    "link_type": "item",
    "id": 19721,
    "quantity": 1
  }
}
```

#### 6. GW2 Lookup (`gw2_lookup`)

Look up Guild Wars 2 game data by ID from the public API. No API key needed. Returns the raw API response.

**Parameters:**
- `endpoint` (required): Which `/v2/` collection to query — `items`, `skills`, `traits`, `specializations`, `recipes`, `maps`, `achievements`, `achievements/groups`, `achievements/categories`, `colors`, `legends`, `professions`, or `continents`
- `ids` (optional): Integer IDs to fetch. Required for most endpoints (their collections are too large to fetch in full); `legends`, `professions`, `continents`, `achievements/groups`, and `achievements/categories` are small enough to omit `ids` and fetch the whole collection. Only integer IDs are supported, so string-keyed collections (e.g. named professions) are only reachable via the no-`ids` whole-collection fetch.

**Example:**
```json
{
  "tool": "gw2_lookup",
  "arguments": {
    "endpoint": "skills",
    "ids": [12503]
  }
}
```

#### 7. Achievement Search (`achievement_search`)

Search Guild Wars 2 achievement groups/categories by name (case-insensitive substring match). The public API has no text search, only lookup-by-ID, so this walks the small groups/categories hierarchy (19 groups, 355 categories) instead of the full ~8,000-achievement catalog. Matches broad themes (category/group names like "Festival" or "Slayer"), not necessarily an individual achievement's exact title — use `gw2_lookup` with `endpoint: "achievements"` and the returned achievement IDs for full details.

**Parameters:**
- `query` (required): Case-insensitive substring to match against achievement group/category names

**Example:**
```json
{
  "tool": "achievement_search",
  "arguments": {
    "query": "Festival"
  }
}
```

### MCP Resources

The server provides the following resources:

#### Currency List (`gw2://currencies`)

Complete list of all Guild Wars 2 currencies with metadata.

## API Key Setup

To use wallet functionality, you need a Guild Wars 2 API key:

1. Visit [Guild Wars 2 API Key Management](https://account.arena.net/applications)
2. Create a new API key with the following permissions:
   - `account` - Required for wallet access
   - `wallet` - Required for currency information
3. Copy the generated API key

**Security Note:** API keys are hashed before caching for security. Never share your API key.

## Caching Strategy

The server implements intelligent caching:

- **Static Data** (currencies, wiki content): Cached for 24 hours to 1 year
- **Dynamic Data** (wallet balances): Cached for 5 minutes
- **Search Results**: Cached for 24 hours

## Architecture

The project follows Clean Architecture principles:

```
internal/
├── server/          # MCP server implementation
├── cache/           # Caching layer
├── gw2api/          # GW2 API client
└── wiki/            # Wiki API client
```

## Development

### Code Standards

- Format code with `gofumpt`
- Lint with `golangci-lint`
- Write unit tests for core functionality
- Follow conventional commit messages

### Running Tests

```bash
go test ./...
```

### Linting

```bash
golangci-lint run
```

### Formatting

```bash
gofumpt -w .
```

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests if applicable
5. Run linting and formatting
6. Submit a pull request

## License

GNU Affero General Public License v3.0 - see LICENSE file for details.

## Acknowledgments

- [Guild Wars 2 API](https://wiki.guildwars2.com/wiki/API:Main) for providing comprehensive game data
- [Guild Wars 2 Wiki](https://wiki.guildwars2.com/) for extensive game documentation
- [MCP Go](https://github.com/mark3labs/mcp-go) for the MCP implementation framework
