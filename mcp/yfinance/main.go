// yfinance-mcp: MCP server for Yahoo Finance time series data.
// Speaks JSON-RPC 2.0 over stdio — wire into guff via config.yaml mcp block.
// No external dependencies; pure stdlib.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// ─── MCP JSON-RPC 2.0 types ────────────────────────────────────────────────

type rpcMsg struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method,omitempty"`
	Params  json.RawMessage  `json:"params,omitempty"`
	Result  any              `json:"result,omitempty"`
	Error   *rpcError        `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type toolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type callParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type mcpResult struct {
	Content []mcpContent `json:"content"`
	IsError bool         `json:"isError,omitempty"`
}

type mcpContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ─── Tool catalogue ────────────────────────────────────────────────────────

var catalogue = []toolDef{
	{
		Name: "yfinance_get_history",
		Description: "Fetch OHLCV candlestick price history for a ticker. " +
			"Returns CSV with Timestamp (Unix), Date, Open, High, Low, Close, Volume, AdjClose — " +
			"suitable for candlestick charts. " +
			"Use from/to (YYYY-MM-DD) for a specific date range, or period for a rolling window.",
		InputSchema: json.RawMessage(`{
			"type":"object",
			"properties":{
				"symbol":  {"type":"string","description":"Ticker, e.g. AAPL"},
				"period":  {"type":"string","description":"Rolling range (ignored if from+to set): 1d 5d 1mo 3mo 6mo 1y 2y 5y 10y ytd max","default":"1y"},
				"interval":{"type":"string","description":"Bar size: 1m 5m 15m 30m 60m 1d 1wk 1mo","default":"1d"},
				"from":    {"type":"string","description":"Start date YYYY-MM-DD (use with to for exact range)"},
				"to":      {"type":"string","description":"End date YYYY-MM-DD (defaults to today)"}
			},
			"required":["symbol"]
		}`),
	},
	{
		Name:        "yfinance_get_info",
		Description: "Fetch current quote data and key metrics for a ticker: price, market cap, P/E, 52-week range, etc.",
		InputSchema: json.RawMessage(`{
			"type":"object",
			"properties":{
				"symbol":{"type":"string","description":"Ticker, e.g. MSFT"}
			},
			"required":["symbol"]
		}`),
	},
	{
		Name:        "yfinance_get_financials",
		Description: "Fetch income statement, balance sheet, or cash flow statement for a ticker.",
		InputSchema: json.RawMessage(`{
			"type":"object",
			"properties":{
				"symbol":    {"type":"string","description":"Ticker, e.g. GOOG"},
				"statement": {"type":"string","description":"income | balance | cashflow","default":"income"},
				"frequency": {"type":"string","description":"annual | quarterly","default":"annual"}
			},
			"required":["symbol"]
		}`),
	},
	{
		Name:        "yfinance_get_dividends",
		Description: "Fetch dividend payment history for a ticker.",
		InputSchema: json.RawMessage(`{
			"type":"object",
			"properties":{
				"symbol":{"type":"string","description":"Ticker, e.g. KO"},
				"period":{"type":"string","description":"Range: 1y 2y 5y 10y max","default":"5y"}
			},
			"required":["symbol"]
		}`),
	},
}

// ─── Yahoo Finance HTTP client ─────────────────────────────────────────────

const userAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36"

type yfClient struct {
	mu   sync.Mutex
	http *http.Client
}

var yf = &yfClient{
	http: &http.Client{Timeout: 15 * time.Second},
}

func (c *yfClient) get(rawURL string) ([]byte, error) {
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	// Cookie jar sends session cookies automatically.

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}


// ─── Tool implementations ──────────────────────────────────────────────────

func getHistory(sym, period, interval, from, to string) (string, error) {
	if sym == "" {
		return "", fmt.Errorf("symbol is required")
	}
	if interval == "" {
		interval = "1d"
	}

	var u string
	if from != "" {
		// Exact date range using Unix timestamps.
		const layout = "2006-01-02"
		t1, err := time.Parse(layout, from)
		if err != nil {
			return "", fmt.Errorf("invalid from date %q (want YYYY-MM-DD): %w", from, err)
		}
		var t2 time.Time
		if to != "" {
			t2, err = time.Parse(layout, to)
			if err != nil {
				return "", fmt.Errorf("invalid to date %q (want YYYY-MM-DD): %w", to, err)
			}
			t2 = t2.Add(24 * time.Hour) // include the to day
		} else {
			t2 = time.Now().UTC().Add(24 * time.Hour)
		}
		u = fmt.Sprintf(
			"https://query1.finance.yahoo.com/v8/finance/chart/%s?period1=%d&period2=%d&interval=%s&events=history",
			url.PathEscape(strings.ToUpper(sym)), t1.Unix(), t2.Unix(), interval,
		)
	} else {
		if period == "" {
			period = "1y"
		}
		u = fmt.Sprintf(
			"https://query1.finance.yahoo.com/v8/finance/chart/%s?range=%s&interval=%s&events=history",
			url.PathEscape(strings.ToUpper(sym)), period, interval,
		)
	}

	data, err := yf.get(u)
	if err != nil {
		return "", err
	}

	var r struct {
		Chart struct {
			Result []struct {
				Timestamp  []int64 `json:"timestamp"`
				Indicators struct {
					Quote []struct {
						Open   []*float64 `json:"open"`
						High   []*float64 `json:"high"`
						Low    []*float64 `json:"low"`
						Close  []*float64 `json:"close"`
						Volume []*int64   `json:"volume"`
					} `json:"quote"`
					AdjClose []struct {
						AdjClose []*float64 `json:"adjclose"`
					} `json:"adjclose"`
				} `json:"indicators"`
			} `json:"result"`
			Error *struct{ Description string } `json:"error"`
		} `json:"chart"`
	}

	if err := json.Unmarshal(data, &r); err != nil {
		return "", fmt.Errorf("parse: %w", err)
	}
	if r.Chart.Error != nil {
		return "", fmt.Errorf("Yahoo Finance: %s", r.Chart.Error.Description)
	}
	if len(r.Chart.Result) == 0 || len(r.Chart.Result[0].Timestamp) == 0 {
		return "", fmt.Errorf("no history data for %s", sym)
	}

	res := r.Chart.Result[0]
	q := res.Indicators.Quote[0]
	var adj []*float64
	if len(res.Indicators.AdjClose) > 0 {
		adj = res.Indicators.AdjClose[0].AdjClose
	}

	rangeLabel := period
	if from != "" {
		rangeLabel = from + " to " + to
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s %s Candlestick History (%s)\n", strings.ToUpper(sym), interval, rangeLabel)
	sb.WriteString("Timestamp,Date,Open,High,Low,Close,AdjClose,Volume\n")
	for i, ts := range res.Timestamp {
		date := time.Unix(ts, 0).UTC().Format("2006-01-02")
		fmt.Fprintf(&sb, "%d,%s,%s,%s,%s,%s,%s,%s\n",
			ts, date,
			ptrF(q.Open, i), ptrF(q.High, i), ptrF(q.Low, i), ptrF(q.Close, i),
			ptrF(adj, i), ptrI(q.Volume, i),
		)
	}
	return sb.String(), nil
}

func getInfo(sym string) (string, error) {
	if sym == "" {
		return "", fmt.Errorf("symbol is required")
	}
	sym = strings.ToUpper(sym)

	// Step 1: v8 chart meta — works without crumb, has core price/range data.
	u := fmt.Sprintf(
		"https://query1.finance.yahoo.com/v8/finance/chart/%s?range=1d&interval=1d",
		url.PathEscape(sym),
	)
	data, err := yf.get(u)
	if err != nil {
		return "", err
	}

	var chartResp struct {
		Chart struct {
			Result []struct {
				Meta map[string]any `json:"meta"`
			} `json:"result"`
			Error *struct{ Description string } `json:"error"`
		} `json:"chart"`
	}
	if err := json.Unmarshal(data, &chartResp); err != nil {
		return "", fmt.Errorf("parse: %w", err)
	}
	if chartResp.Chart.Error != nil {
		return "", fmt.Errorf("Yahoo Finance: %s", chartResp.Chart.Error.Description)
	}
	if len(chartResp.Chart.Result) == 0 {
		return "", fmt.Errorf("no data for %s", sym)
	}
	meta := chartResp.Chart.Result[0].Meta

	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s Quote\n", sym)

	metaFields := []struct{ key, label string }{
		{"symbol", "Symbol"},
		{"longName", "Name"},
		{"fullExchangeName", "Exchange"},
		{"currency", "Currency"},
		{"instrumentType", "Type"},
		{"regularMarketPrice", "Price"},
		{"chartPreviousClose", "Prev Close"},
		{"regularMarketDayHigh", "Day High"},
		{"regularMarketDayLow", "Day Low"},
		{"regularMarketVolume", "Volume"},
		{"fiftyTwoWeekHigh", "52W High"},
		{"fiftyTwoWeekLow", "52W Low"},
	}
	for _, f := range metaFields {
		v, ok := meta[f.key]
		if !ok || v == nil {
			continue
		}
		fmt.Fprintf(&sb, "%-28s %v\n", f.label+":", v)
	}

	// Enrich with SEC EDGAR: company metadata + computed market cap, P/E, P/B.
	// All from reliable, auth-free sources.
	price, _ := meta["regularMarketPrice"].(float64)
	if cik, err := lookupCIK(sym); err == nil {
		secEnrich(&sb, cik, price)
	}

	return sb.String(), nil
}

// getFinancials is implemented in sec.go using SEC EDGAR XBRL data.

func getDividends(sym, period string) (string, error) {
	if sym == "" {
		return "", fmt.Errorf("symbol is required")
	}
	if period == "" {
		period = "5y"
	}

	u := fmt.Sprintf(
		"https://query1.finance.yahoo.com/v8/finance/chart/%s?range=%s&interval=1d&events=dividends",
		url.PathEscape(strings.ToUpper(sym)), period,
	)
	data, err := yf.get(u)
	if err != nil {
		return "", err
	}

	var r struct {
		Chart struct {
			Result []struct {
				Events *struct {
					Dividends map[string]struct {
						Amount float64 `json:"amount"`
						Date   int64   `json:"date"`
					} `json:"dividends"`
				} `json:"events"`
			} `json:"result"`
			Error *struct{ Description string } `json:"error"`
		} `json:"chart"`
	}

	if err := json.Unmarshal(data, &r); err != nil {
		return "", fmt.Errorf("parse: %w", err)
	}
	if r.Chart.Error != nil {
		return "", fmt.Errorf("Yahoo Finance: %s", r.Chart.Error.Description)
	}
	if len(r.Chart.Result) == 0 || r.Chart.Result[0].Events == nil ||
		len(r.Chart.Result[0].Events.Dividends) == 0 {
		return fmt.Sprintf("%s: no dividends found in the last %s.", strings.ToUpper(sym), period), nil
	}

	type div struct {
		date   string
		amount float64
	}
	var list []div
	for _, d := range r.Chart.Result[0].Events.Dividends {
		list = append(list, div{
			date:   time.Unix(d.Date, 0).UTC().Format("2006-01-02"),
			amount: d.Amount,
		})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].date < list[j].date })

	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s Dividends (%s)\n", strings.ToUpper(sym), period)
	sb.WriteString("Date,Amount\n")
	for _, d := range list {
		fmt.Fprintf(&sb, "%s,%.4f\n", d.date, d.amount)
	}
	return sb.String(), nil
}

// ─── Helpers ───────────────────────────────────────────────────────────────

func ptrF(vals []*float64, i int) string {
	if i >= len(vals) || vals[i] == nil {
		return ""
	}
	return fmt.Sprintf("%.4f", *vals[i])
}

func ptrI(vals []*int64, i int) string {
	if i >= len(vals) || vals[i] == nil {
		return ""
	}
	return fmt.Sprintf("%d", *vals[i])
}

// ─── MCP server ────────────────────────────────────────────────────────────

var enc = json.NewEncoder(os.Stdout)

func respond(id *json.RawMessage, result any) {
	enc.Encode(rpcMsg{JSONRPC: "2.0", ID: id, Result: result}) //nolint:errcheck
}

func respondErr(id *json.RawMessage, code int, msg string) {
	enc.Encode(rpcMsg{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}}) //nolint:errcheck
}

func handle(msg rpcMsg) {
	// Notifications have no ID — do not respond.
	if strings.HasPrefix(msg.Method, "notifications/") {
		return
	}

	switch msg.Method {
	case "initialize":
		respond(msg.ID, map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "yfinance-mcp", "version": "0.1.0"},
		})

	case "tools/list":
		respond(msg.ID, map[string]any{"tools": catalogue})

	case "tools/call":
		var p callParams
		if err := json.Unmarshal(msg.Params, &p); err != nil {
			respondErr(msg.ID, -32600, "invalid params: "+err.Error())
			return
		}
		text, err := dispatch(p)
		if err != nil {
			respond(msg.ID, mcpResult{
				Content: []mcpContent{{Type: "text", Text: err.Error()}},
				IsError: true,
			})
			return
		}
		respond(msg.ID, mcpResult{Content: []mcpContent{{Type: "text", Text: text}}})

	default:
		if msg.ID != nil {
			respondErr(msg.ID, -32601, "method not found: "+msg.Method)
		}
	}
}

func dispatch(p callParams) (string, error) {
	var args map[string]string
	json.Unmarshal(p.Arguments, &args) //nolint:errcheck

	get := func(k, def string) string {
		if v := args[k]; v != "" {
			return v
		}
		return def
	}

	switch p.Name {
	case "yfinance_get_history":
		return getHistory(get("symbol", ""), get("period", "1y"), get("interval", "1d"), get("from", ""), get("to", ""))
	case "yfinance_get_info":
		return getInfo(get("symbol", ""))
	case "yfinance_get_financials":
		return getFinancials(get("symbol", ""), get("statement", "income"), get("frequency", "annual"))
	case "yfinance_get_dividends":
		return getDividends(get("symbol", ""), get("period", "5y"))
	default:
		return "", fmt.Errorf("unknown tool: %s", p.Name)
	}
}

func main() {
	log.SetOutput(os.Stderr)
	log.SetFlags(0)
	log.Println("[yfinance-mcp] ready")

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var msg rpcMsg
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			log.Printf("[yfinance-mcp] parse error: %v", err)
			continue
		}
		handle(msg)
	}
	if err := scanner.Err(); err != nil {
		log.Fatalf("[yfinance-mcp] stdin: %v", err)
	}
}
