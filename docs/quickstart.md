# Quick-Start How-Tos

> Twelve hands-on walkthroughs. Copy, paste, run. No PhD in YAML required.
>
> You'll go from "what is this?" to "I just automated financial chart generation with a local 3B model" in about 30 minutes.

---

## 1. Zero to LLM in 30 Seconds

Pull a model, run a prompt. That's it.

```bash
# Build guff (one time)
make build

# Pull a model from HuggingFace
export GUFF_HUGGINGFACE_TOKEN="hf_xxxx"
guff pull granite-3b

# Run a single prompt
guff run "Explain quicksort in 3 sentences"
```

GPU acceleration is automatic. If you have an NVIDIA GPU, CUDA kicks in. macOS ARM gets Metal. Linux/Windows can use Vulkan. No flags, no config, no driver surgery. CPU fallback is automatic if no GPU is detected.

Want streaming output?

```bash
guff run --stream "Write a haiku about debugging"
```

Key flags for `guff run`:

| Flag | Default | Description |
|------|---------|-------------|
| `-m, --model` | config default | Model to use |
| `--max-tokens` | 512 | Max tokens to generate |
| `-t, --temperature` | 0 | Sampling temperature |
| `--stream` | false | Stream tokens as they generate |
| `--seed` | 0 | Random seed (0 = random) |

---

## 2. Chat That Remembers

Interactive chat with persistent sessions, context tracking, and slash commands.

```bash
# Start a new chat session
guff chat

# Start with an opening message
guff chat "What's the difference between a mutex and a semaphore?"

# Resume a previous session
guff chat --session <session-id>

# List all saved sessions
guff chat --list-sessions
```

Sessions are stored in SQLite at `~/.local/share/guff/`. Every message, every turn, persisted automatically.

### Slash Commands

Inside a chat session:

| Command | What it does |
|---------|-------------|
| `/status` | Show context window usage, token counts, strategy |
| `/clear` | Clear conversation history |
| `/exit` | Exit chat |

After each turn you'll see a live status line:

```
[12 msgs | 847/1024 tokens | sliding_window]
```

That tells you exactly how much context runway you have left. No guessing, no surprise truncation.

### Disable Persistence

```bash
guff chat --no-persist
```

Sessions won't be saved to disk. Useful for throwaway conversations.

---

## 3. One Syntax, Every Provider

Local model. OpenAI. DeepSeek. Anthropic. Same command, different prefix.

```bash
# Local model (no prefix)
guff chat --model granite-3b

# OpenAI
guff chat --model openai/gpt-4o

# DeepSeek
guff chat --model deepseek/deepseek-chat

# Anthropic
guff chat --model anthropic/claude-sonnet-4-5-20250929
```

### Configure Providers

Add API keys to `~/.config/guff/config.yaml`:

```yaml
providers:
  openai:
    type: openai
    api_key: ${OPENAI_API_KEY}
  anthropic:
    type: anthropic
    api_key: ${ANTHROPIC_API_KEY}
  deepseek:
    type: deepseek
    api_key: ${DEEPSEEK_API_KEY}
  together:
    type: openai-compatible
    api_key: ${TOGETHER_API_KEY}
    base_url: https://api.together.xyz/v1
```

### Model Aliases

Tired of typing `anthropic/claude-sonnet-4-5-20250929`? Create a shortcut:

```yaml
model_routes:
  sonnet:
    provider: anthropic
    model: claude-sonnet-4-5-20250929
  gpt4:
    provider: openai
    model: gpt-4o
```

Now `guff chat --model sonnet` routes to Anthropic. Hot-swap between local and remote with a single flag change.

---

## 4. Your API, Their Clients

Start guff as an API server and any tool that speaks OpenAI or Ollama works out of the box.

```bash
guff serve --host 0.0.0.0 --port 8080
```

### OpenAI-Compatible Endpoint

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "granite-3b",
    "messages": [{"role": "user", "content": "Hello"}],
    "temperature": 0.8
  }'
```

Works with any OpenAI SDK. Point your Python scripts at `http://localhost:8080/v1` and they just work:

```python
from openai import OpenAI
client = OpenAI(base_url="http://localhost:8080/v1", api_key="not-needed")
response = client.chat.completions.create(
    model="granite-3b",
    messages=[{"role": "user", "content": "Hello"}]
)
```

### Ollama-Compatible Endpoint

```bash
curl http://localhost:8080/api/chat \
  -d '{"model": "granite-3b", "messages": [{"role": "user", "content": "Hello"}]}'
```

### Streaming

Both endpoints support SSE streaming. Add `"stream": true` to any request:

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "granite-3b",
    "messages": [{"role": "user", "content": "Write a poem"}],
    "stream": true
  }'
```

### Route Remote Models Through the API

The API supports provider routing too. Any client connected to guff can access any provider:

```bash
# Route to OpenAI through guff
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model": "openai/gpt-4o", "messages": [{"role": "user", "content": "Hello"}]}'
```

---

## 5. The Dashboard You Didn't Know You Needed

guff ships with an embedded web UI. No extra install, no npm, no build step.

```bash
guff serve --port 8080
# Open http://localhost:8080/ui
```

The dashboard gives you:

- **Streaming chat interface** -- talk to any model, watch tokens arrive in real time
- **Model selector** -- switch between local and remote models from a dropdown
- **Parameter controls** -- temperature, top_p, top_k, max_tokens sliders
- **Tool call visualization** -- see exactly what tools the model invoked and what came back
- **Server status** -- uptime, active model, registered tools, connected providers

### Status API

Programmatic access to server state:

```bash
curl http://localhost:8080/api/status
# {"uptime":"2h30m15s","model":"granite-3b","tools_count":5,"providers":3}

curl http://localhost:8080/api/tools
# {"tools":[{"name":"read_file","description":"Read a file from the filesystem"},...]}
```

---

## 6. Give Your Models Superpowers

MCP (Model Context Protocol) lets your models use tools -- read files, query databases, interact with GitHub, anything with an MCP server.

### Configure MCP Servers

Add to `~/.config/guff/config.yaml`:

```yaml
mcp:
  filesystem:
    command: npx
    args: ["-y", "@anthropic/mcp-filesystem", "/home/user/projects"]
  github:
    command: npx
    args: ["-y", "@modelcontextprotocol/server-github"]
    env:
      GITHUB_TOKEN: ${GITHUB_TOKEN}
  sqlite:
    command: npx
    args: ["-y", "@anthropic/mcp-sqlite", "/path/to/database.db"]
```

### Use Tools in Chat

```bash
guff chat --tools
# Tools are enabled by default -- this flag is explicit

> What files are in /home/user/projects?
```

guff automatically:
1. Launches MCP servers at startup
2. Discovers available tools via JSON-RPC
3. Injects tool descriptions into the system prompt
4. Parses tool calls from model output
5. Executes tools and feeds results back

Your local 3B model gets the same tool-calling loop as frontier APIs. The model says "I need to read that file," guff reads it, and feeds the content back. No manual intervention.

### Disable Tools

```bash
guff chat --tools=false
guff serve --tools=false
```

---

## 7. JSON That Never Breaks

Grammar constraints force the model to produce structurally valid output. The model literally cannot generate malformed JSON.

### Use the Built-In JSON Grammar

guff ships with grammar files in `third_party/llama.cpp/grammars/`. The most useful is `json.gbnf`:

```bash
guff run --model granite-3b \
  "Generate a JSON object with name, age, and hobbies fields" \
  --grammar "$(cat third_party/llama.cpp/grammars/json.gbnf)"
```

Every token the model emits is checked against the grammar in real time. If a token would break the JSON structure, it's rejected before it ever appears.

### Via the API

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "granite-3b",
    "messages": [{"role": "user", "content": "List 3 programming languages as JSON"}],
    "grammar": "root ::= \"{\" ws \"\\\"languages\\\"\" ws \":\" ws \"[\" ws string (\",\" ws string)* ws \"]\" ws \"}\"\nstring ::= \"\\\"\" [a-zA-Z]+ \"\\\"\"\nws ::= [ \\t\\n]*"
  }'
```

The `grammar` field accepts any GBNF grammar string. It's a local-model feature -- the grammar constraint is applied at the sampler chain level (stage 1 of 12).

### Available Grammars

| File | Constrains output to |
|------|---------------------|
| `json.gbnf` | Valid JSON objects |
| `json_arr.gbnf` | Valid JSON arrays |
| `list.gbnf` | List format |
| `arithmetic.gbnf` | Arithmetic expressions |
| `c.gbnf` | C language syntax |
| `chess.gbnf` | Chess notation |

---

## 8. Find What's Similar

Embeddings turn text into vectors. Use them for semantic search, clustering, or building RAG pipelines.

### Local Embeddings

```bash
curl http://localhost:8080/v1/embeddings \
  -H "Content-Type: application/json" \
  -d '{"model": "granite-3b", "input": "How do I sort a list in Python?"}'
```

Returns a vector you can use for cosine similarity, nearest-neighbor search, or feed into a vector database.

### Remote Embeddings

Route to OpenAI's embedding models through the same API:

```bash
curl http://localhost:8080/v1/embeddings \
  -H "Content-Type: application/json" \
  -d '{"model": "openai/text-embedding-3-small", "input": "How do I sort a list in Python?"}'
```

### Batch Embeddings

Pass an array to embed multiple texts in one call:

```bash
curl http://localhost:8080/v1/embeddings \
  -H "Content-Type: application/json" \
  -d '{
    "model": "granite-3b",
    "input": ["first document", "second document", "third document"]
  }'
```

### Quick Cosine Similarity

Compute similarity between two texts with a simple script:

```python
import requests, numpy as np

def embed(text):
    r = requests.post("http://localhost:8080/v1/embeddings",
        json={"model": "granite-3b", "input": text})
    return np.array(r.json()["data"][0]["embedding"])

a = embed("machine learning algorithms")
b = embed("neural network training")
c = embed("chocolate cake recipe")

similarity_ab = np.dot(a, b) / (np.linalg.norm(a) * np.linalg.norm(b))
similarity_ac = np.dot(a, c) / (np.linalg.norm(a) * np.linalg.norm(c))

print(f"ML vs Neural Networks: {similarity_ab:.3f}")  # High
print(f"ML vs Cake:            {similarity_ac:.3f}")  # Low
```

---

## 9. Context That Doesn't Explode

guff tracks your context window in real time. You always know how much space is left.

### Check Context Status

In chat, use `/status`:

```
> /status
Context: 847/2048 tokens used
Messages: 12
Strategy: sliding_window
Model: granite-3b
```

Or watch the live status line after each turn:

```
[12 msgs | 847/2048 tokens | sliding_window]
```

### How Sliding Window Works

When the conversation exceeds the token budget, the `sliding_window` strategy drops the oldest messages (preserving the system prompt) until everything fits. You keep talking; guff keeps trimming. No crashes, no errors, no manual intervention.

The budget is calculated as: `context_size - max_gen_tokens`. With a 2048-token context and 512 max generation tokens, you get 1536 tokens for conversation history.

### Context Size

Set it globally or per-command:

```bash
# CLI flag
guff chat --ctx-size 4096

# Config
# model:
#   context_size: 4096
```

### Strict Mode

For testing or when you need guaranteed full context:

```yaml
# In config.yaml
chat:
  context_strategy: fail
```

The `fail` strategy returns an error instead of truncating. Useful for testing or when you absolutely must not lose any messages.

---

## 10. Teach Your Model Who It Is

System prompts shape model behavior. guff gives you five ways to set them, from quick CLI flags to project-level auto-discovery.

### Quick: CLI Flag

```bash
guff chat --system "You are a senior Go developer. Be concise. Use code examples."
```

Or load from a file:

```bash
guff chat --system-file ./my-prompt.txt
```

### Project-Level: Auto-Discovery

Create a `.guff/prompt.md` file anywhere in your project:

```bash
mkdir -p .guff
echo "You are a coding assistant for the Acme project. Use TypeScript." > .guff/prompt.md
```

guff walks up from your current directory looking for `.guff/prompt.md`. Run `guff chat` from inside your project and the prompt is injected automatically. Different projects, different prompts, zero config.

### User-Level: Default Prompt

```bash
echo "Always respond in English. Be helpful and concise." > ~/.config/guff/system-prompt.txt
```

### Advanced: Multi-Part Prompt Builder

Compose prompts from multiple sections in config:

```yaml
prompt:
  sections:
    - type: base
      content: "You are a helpful coding assistant."
    - type: project
      auto: true          # auto-discovers .guff/prompt.md
    - type: user
      auto: true          # loads ~/.config/guff/user-prompt.txt
    - type: tools
      auto: true          # injected at runtime from MCP/tool registry
```

### Per-Model Overrides

Different models, different instructions:

```yaml
prompt:
  models:
    granite-3b:
      sections:
        - type: base
          content: "You are a concise coding assistant. Keep responses under 200 words."
```

### Priority Order

1. `--system` flag (highest)
2. `--system-file` flag
3. Per-model config
4. Config default
5. `~/.config/guff/system-prompt.txt` (lowest)

---

## 11. Fine-Tune the Randomness

guff runs a 12-stage sampler chain. Each stage filters or transforms the token probability distribution before the final token is selected.

### Quick Presets

**Deterministic (testing, reproducible output):**
```bash
guff run -t 0 --seed 42 "What is 2+2?"
```

**Code generation (focused, low randomness):**
```bash
guff run -t 0.2 --top-p 0.9 --top-k 40 --repeat-penalty 1.1 "Write a binary search in Go"
```

**Creative writing (high randomness, diverse output):**
```bash
guff run -t 1.0 --top-p 0.95 --top-k 100 --repeat-penalty 1.2 "Write an opening paragraph for a noir detective novel"
```

**Balanced chat (the defaults):**
```bash
guff chat -t 0.8 --top-p 0.9 --top-k 40 --repeat-penalty 1.1
```

### The Chain

```
Raw Logits
 → Grammar (constrain to valid syntax)
 → Logit Bias (manual adjustments)
 → Temperature (scale randomness)
 → Top-K (keep K most probable)
 → Top-P (nucleus sampling)
 → Typical-P (information content filter)
 → Min-P (relative threshold)
 → Top-N-Sigma (statistical filter)
 → Penalties (repeat/frequency/presence)
 → DRY (anti-repetition patterns)
 → XTC (extended token control)
 → Terminal (dist or greedy)
```

Filters with their value set to 0 or disabled are skipped. The terminal sampler is always last -- `SamplerInitDist` for probabilistic sampling (temp > 0), `SamplerInitGreedy` for deterministic (temp == 0).

### API Sampling Parameters

All parameters are available via the API:

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "granite-3b",
    "messages": [{"role": "user", "content": "Hello"}],
    "temperature": 0.5,
    "top_p": 0.9,
    "top_k": 40,
    "min_p": 0.05,
    "seed": 42,
    "typical_p": 0.9,
    "top_n_sigma": 2.0,
    "dry_multiplier": 0.8,
    "logit_bias": {"128": 5.0}
  }'
```

---

## 12. The Big One: Autonomous Financial Charts

This is where everything comes together. Grammar constraints + MCP tools + system prompts + the API = a local model that generates valid financial charts and writes them to disk. No cloud. No parsing errors. No manual intervention.

### Step 1: Grammar-Constrained Chart Data

Force the model to output valid JSON chart data. Every time. No exceptions.

```bash
guff run --model granite-3b \
  --grammar "$(cat third_party/llama.cpp/grammars/json.gbnf)" \
  "Generate a JSON object with quarterly revenue data for a tech company.
   Include fields: company, quarters (array of {quarter, revenue, profit}).
   Use realistic numbers in millions."
```

The grammar constraint means the model physically cannot produce malformed JSON. The output will always parse. Always.

Example output:
```json
{
  "company": "TechCorp",
  "quarters": [
    {"quarter": "Q1 2025", "revenue": 142.5, "profit": 38.2},
    {"quarter": "Q2 2025", "revenue": 156.8, "profit": 42.1},
    {"quarter": "Q3 2025", "revenue": 168.3, "profit": 45.7},
    {"quarter": "Q4 2025", "revenue": 185.2, "profit": 51.4}
  ]
}
```

### Step 2: Full Chart.js HTML Generation

Ask the model to generate a complete, self-contained HTML page with Chart.js:

```bash
guff run --model granite-3b --max-tokens 2048 \
  --system "You generate self-contained HTML files with Chart.js loaded from CDN. Output ONLY the HTML, no explanation." \
  "Create an HTML page with a bar chart showing quarterly revenue:
   Q1: $142.5M, Q2: $156.8M, Q3: $168.3M, Q4: $185.2M.
   Use a professional color scheme. Include a title." \
  > chart.html
```

Open `chart.html` in your browser. You have a financial chart generated entirely by a local model running on your hardware.

### Step 3: MCP Autonomous Pipeline

With the filesystem MCP server configured, the model writes the file itself:

```yaml
# ~/.config/guff/config.yaml
mcp:
  filesystem:
    command: npx
    args: ["-y", "@anthropic/mcp-filesystem", "/home/user/reports"]
```

```bash
guff chat --model granite-3b --system "You are a financial report generator. \
  Use the write_file tool to save reports. Always generate self-contained HTML \
  with Chart.js from https://cdn.jsdelivr.net/npm/chart.js CDN."

> Generate a quarterly revenue chart for TechCorp and save it as report-q4.html
```

The model generates the HTML, calls the `write_file` tool, and the chart lands on disk. Zero manual steps between "generate a chart" and "chart exists as a file."

### Step 4: API Automation

Automate chart generation via `curl`. Drop this in a cron job or script:

```bash
#!/bin/bash
# generate-report.sh -- automated financial chart generation

QUARTER="Q4 2025"
DATA='{"revenue": 185.2, "profit": 51.4, "expenses": 133.8}'

RESPONSE=$(curl -s http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d "{
    \"model\": \"granite-3b\",
    \"messages\": [
      {\"role\": \"system\", \"content\": \"Generate a complete self-contained HTML page with a Chart.js chart. Output ONLY valid HTML. No markdown, no explanation.\"},
      {\"role\": \"user\", \"content\": \"Create a financial dashboard for ${QUARTER} with this data: ${DATA}. Include a bar chart for revenue vs expenses, and a line showing profit margin.\"}
    ],
    \"max_tokens\": 2048,
    \"temperature\": 0.3
  }")

# Extract the HTML from the response
echo "$RESPONSE" | jq -r '.choices[0].message.content' > "report-${QUARTER}.html"

echo "Report saved to report-${QUARTER}.html"
```

Run it on a schedule. Feed it live data. Automated financial reporting, running entirely on your machine.

### Step 5: Embeddings for Financial Analysis

Use embeddings to find similar tickers, cluster financial news, or build a financial RAG pipeline:

```python
import requests
import numpy as np

def embed(text):
    r = requests.post("http://localhost:8080/v1/embeddings",
        json={"model": "granite-3b", "input": text})
    return np.array(r.json()["data"][0]["embedding"])

# Embed company descriptions
companies = {
    "AAPL": embed("Apple designs consumer electronics and software"),
    "MSFT": embed("Microsoft develops enterprise software and cloud computing"),
    "TSLA": embed("Tesla manufactures electric vehicles and energy storage"),
    "JPM":  embed("JPMorgan Chase provides banking and financial services"),
}

# Find which company is most similar to a query
query = embed("cloud computing and productivity software")
for ticker, vec in companies.items():
    sim = np.dot(query, vec) / (np.linalg.norm(query) * np.linalg.norm(vec))
    print(f"{ticker}: {sim:.3f}")

# MSFT will score highest -- the model understands semantic similarity
```

### Why This Matters

A local 3B model running on your hardware just:
1. Generated structurally valid JSON (grammar constraints)
2. Created a self-contained Chart.js HTML page
3. Wrote it to disk autonomously (MCP filesystem tools)
4. Did it all through a standard API (curl/SDK compatible)
5. Used embeddings for semantic financial analysis

No API keys required. No cloud dependency. No data leaving your machine. That's not a toy -- that's a production pipeline.

---

## What's Next

You've seen the full surface area. Here's where to go deeper:

| Want to... | Read |
|------------|------|
| Understand the architecture | [Architecture](architecture.md) |
| Configure everything | [Configuration](configuration.md) |
| Set up remote providers | [Providers](providers.md) |
| Build MCP tool integrations | [MCP & Tools](mcp-tools.md) |
| Master context management | [Context Management](context-management.md) |
| Design advanced prompts | [Prompt Builder](prompt-builder.md) |
| Use every API endpoint | [API Reference](api-reference.md) |
| Tune the sampler chain | [Sampling](sampling.md) |
| Understand memory architecture | [Memory Deep-Dive](memory-deep-dive.md) |
