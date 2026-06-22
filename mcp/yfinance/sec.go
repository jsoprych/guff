package main

// SEC EDGAR XBRL data source for financial statements.
// Uses data.sec.gov APIs — free, no auth, no crumb required.
// SEC usage policy: https://www.sec.gov/developer

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// secUA must identify the requester per SEC policy: https://www.sec.gov/developer
// Override with env var SEC_USER_AGENT="YourApp yourname@email.com"
var secUA = func() string {
	if ua := os.Getenv("SEC_USER_AGENT"); ua != "" {
		return ua
	}
	return "guff-mcp/0.1 guff-mcp@localhost"
}()

var cikRE = regexp.MustCompile(`<cik>(\d+)</cik>`)

// cikCache maps ticker → zero-padded CIK (e.g. "0000320193").
var cikCache struct {
	sync.RWMutex
	m map[string]string
}

func init() {
	cikCache.m = make(map[string]string)
}

// lookupCIK resolves a ticker to its padded SEC CIK string.
// Uses EDGAR's company browse endpoint which accepts tickers directly.
func lookupCIK(ticker string) (string, error) {
	ticker = strings.ToUpper(ticker)

	cikCache.RLock()
	if cik, ok := cikCache.m[ticker]; ok {
		cikCache.RUnlock()
		return cik, nil
	}
	cikCache.RUnlock()

	u := fmt.Sprintf(
		"https://www.sec.gov/cgi-bin/browse-edgar?action=getcompany&CIK=%s&type=10-K&dateb=&owner=include&count=1&output=atom",
		url.QueryEscape(ticker),
	)
	body, err := secGet(u)
	if err != nil {
		return "", fmt.Errorf("CIK lookup for %s: %w", ticker, err)
	}
	m := cikRE.FindSubmatch(body)
	if m == nil {
		return "", fmt.Errorf("CIK not found for ticker %q (not a US-listed company?)", ticker)
	}
	cik := string(m[1])

	cikCache.Lock()
	cikCache.m[ticker] = cik
	cikCache.Unlock()

	return cik, nil
}

// secGet performs an HTTP GET against SEC EDGAR with the required User-Agent.
// SEC policy requires: "User-Agent: CompanyName contact@email.com"
func secGet(rawURL string) ([]byte, error) {
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", secUA)
	req.Header.Set("Accept", "application/json, application/atom+xml, text/html")

	resp, err := yf.http.Do(req) // reuse client for connection pooling
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("EDGAR HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}

// secConcept fetches the XBRL concept time-series for one GAAP tag.
// Returns entries filtered to the given form type ("10-K" or "10-Q"),
// deduped, and sorted chronologically.
func secConcept(cik, concept, form string) ([]secEntry, error) {
	u := fmt.Sprintf(
		"https://data.sec.gov/api/xbrl/companyconcept/CIK%s/us-gaap/%s.json",
		cik, concept,
	)
	body, err := secGet(u)
	if err != nil {
		return nil, err
	}

	var r struct {
		Units map[string][]secEntry `json:"units"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("parse concept %s: %w", concept, err)
	}

	// USD is the expected unit; fall back to first available.
	entries, ok := r.Units["USD"]
	if !ok {
		for _, v := range r.Units {
			entries = v
			break
		}
	}

	// Filter to target form type + FY or quarterly period designators.
	// Deduplicate by (end-date, val) to remove amended re-filings.
	seen := map[string]bool{}
	var out []secEntry
	for _, e := range entries {
		if e.Form != form {
			continue
		}
		// For 10-K use FY periods only; for 10-Q accept Q1–Q4.
		if form == "10-K" && e.FP != "FY" {
			continue
		}
		key := e.End + fmt.Sprint(e.Val)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, e)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].End < out[j].End })
	return out, nil
}

type secEntry struct {
	End  string  `json:"end"`
	Val  float64 `json:"val"`
	Form string  `json:"form"`
	FP   string  `json:"fp"`
	FY   int     `json:"fy"`
}

// latestAnnualVal returns the most recent 10-K (FY) value for a GAAP concept.
// Returns 0 if the concept is unavailable or errors.
func latestAnnualVal(cik, concept string) float64 {
	entries, err := secConcept(cik, concept, "10-K")
	if err != nil || len(entries) == 0 {
		return 0
	}
	return entries[len(entries)-1].Val
}

// formatLarge formats a raw USD value as T/B/M with 2 decimal places.
func formatLarge(v float64) string {
	switch {
	case v >= 1e12:
		return fmt.Sprintf("%.2fT", v/1e12)
	case v >= 1e9:
		return fmt.Sprintf("%.2fB", v/1e9)
	case v >= 1e6:
		return fmt.Sprintf("%.2fM", v/1e6)
	default:
		return fmt.Sprintf("%.0f", v)
	}
}

// secEnrich writes company metadata and computed valuation metrics to sb.
// Called from getInfo after the chart meta section.
// Sources: EDGAR submissions (name/sector/incorporation) + XBRL concepts.
func secEnrich(sb *strings.Builder, cik string, price float64) {
	// ── Company metadata from submissions endpoint ──────────────────────────
	submURL := fmt.Sprintf("https://data.sec.gov/submissions/CIK%s.json", cik)
	if body, err := secGet(submURL); err == nil {
		var s struct {
			Name                 string `json:"name"`
			SIC                  string `json:"sic"`
			SICDescription       string `json:"sicDescription"`
			StateOfIncorporation string `json:"stateOfIncorporation"`
			FiscalYearEnd        string `json:"fiscalYearEnd"` // e.g. "0930"
			Category             string `json:"category"`
		}
		if json.Unmarshal(body, &s) == nil {
			sb.WriteString("\n")
			if s.SICDescription != "" {
				fmt.Fprintf(sb, "%-28s %s (SIC %s)\n", "Industry:", s.SICDescription, s.SIC)
			}
			if s.StateOfIncorporation != "" {
				fmt.Fprintf(sb, "%-28s %s\n", "Incorporated:", s.StateOfIncorporation)
			}
			if len(s.FiscalYearEnd) == 4 {
				// "0930" → "Sep 30"
				months := []string{"", "Jan", "Feb", "Mar", "Apr", "May", "Jun",
					"Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
				mo := int(s.FiscalYearEnd[0]-'0')*10 + int(s.FiscalYearEnd[1]-'0')
				day := s.FiscalYearEnd[2:]
				if mo >= 1 && mo <= 12 {
					fmt.Fprintf(sb, "%-28s %s %s\n", "Fiscal Year End:", months[mo], day)
				}
			}
			if s.Category != "" {
				fmt.Fprintf(sb, "%-28s %s\n", "Filer Category:", s.Category)
			}
		}
	}

	// ── Computed valuation metrics from XBRL ────────────────────────────────
	shares := latestAnnualVal(cik, "CommonStockSharesOutstanding")
	eps := latestAnnualVal(cik, "EarningsPerShareDiluted")
	equity := latestAnnualVal(cik, "StockholdersEquity")
	if equity == 0 {
		equity = latestAnnualVal(cik, "StockholdersEquityAttributableToParent")
	}

	if shares > 0 || eps != 0 || equity != 0 {
		sb.WriteString("\n")
	}
	if shares > 0 {
		fmt.Fprintf(sb, "%-28s %s\n", "Shares Outstanding:", formatLarge(shares))
		if price > 0 {
			fmt.Fprintf(sb, "%-28s %s\n", "Market Cap (calc):", formatLarge(shares*price))
		}
	}
	if eps != 0 && price > 0 {
		fmt.Fprintf(sb, "%-28s %.2fx\n", "P/E (annual EPS):", price/eps)
	}
	if equity > 0 && shares > 0 {
		bvps := equity / shares
		fmt.Fprintf(sb, "%-28s %.2f\n", "Book Value/Share:", bvps)
		if price > 0 {
			fmt.Fprintf(sb, "%-28s %.2fx\n", "P/B:", price/bvps)
		}
	}
}

// getFinancials fetches income / balance / cashflow statements from SEC EDGAR.
func getFinancials(sym, statement, frequency string) (string, error) {
	if sym == "" {
		return "", fmt.Errorf("symbol is required")
	}
	sym = strings.ToUpper(sym)
	if statement == "" {
		statement = "income"
	}
	if frequency == "" {
		frequency = "annual"
	}

	form := "10-K"
	if frequency == "quarterly" {
		form = "10-Q"
	}

	cik, err := lookupCIK(sym)
	if err != nil {
		return "", err
	}

	type conceptDef struct {
		label    string
		tags     []string // try in order; use first that returns data
		divisor  float64  // 1 = raw USD, 1e6 = millions, 1e9 = billions
		suffix   string
	}

	type statementDef struct {
		title    string
		concepts []conceptDef
	}

	statements := map[string]statementDef{
		"income": {
			title: "Income Statement",
			concepts: []conceptDef{
				{label: "Revenue", tags: []string{
					"RevenueFromContractWithCustomerExcludingAssessedTax",
					"Revenues", "SalesRevenueNet", "RevenueFromContractWithCustomerIncludingAssessedTax",
				}, divisor: 1e9, suffix: "B"},
				{label: "Gross Profit", tags: []string{"GrossProfit"}, divisor: 1e9, suffix: "B"},
				{label: "Operating Income", tags: []string{"OperatingIncomeLoss"}, divisor: 1e9, suffix: "B"},
				{label: "EBITDA (approx)", tags: []string{"EarningsBeforeInterestTaxesDepreciationAndAmortization"}, divisor: 1e9, suffix: "B"},
				{label: "Net Income", tags: []string{"NetIncomeLoss"}, divisor: 1e9, suffix: "B"},
				{label: "EPS (Basic)", tags: []string{"EarningsPerShareBasic"}, divisor: 1, suffix: ""},
				{label: "EPS (Diluted)", tags: []string{"EarningsPerShareDiluted"}, divisor: 1, suffix: ""},
			},
		},
		"balance": {
			title: "Balance Sheet",
			concepts: []conceptDef{
				{label: "Total Assets", tags: []string{"Assets"}, divisor: 1e9, suffix: "B"},
				{label: "Current Assets", tags: []string{"AssetsCurrent"}, divisor: 1e9, suffix: "B"},
				{label: "Cash & Equivalents", tags: []string{
					"CashAndCashEquivalentsAtCarryingValue",
					"CashCashEquivalentsAndShortTermInvestments",
				}, divisor: 1e9, suffix: "B"},
				{label: "Total Liabilities", tags: []string{"Liabilities"}, divisor: 1e9, suffix: "B"},
				{label: "Current Liabilities", tags: []string{"LiabilitiesCurrent"}, divisor: 1e9, suffix: "B"},
				{label: "Long-Term Debt", tags: []string{
					"LongTermDebt", "LongTermDebtNoncurrent",
				}, divisor: 1e9, suffix: "B"},
				{label: "Stockholders' Equity", tags: []string{
					"StockholdersEquity", "StockholdersEquityAttributableToParent",
				}, divisor: 1e9, suffix: "B"},
			},
		},
		"cashflow": {
			title: "Cash Flow Statement",
			concepts: []conceptDef{
				{label: "Operating Cash Flow", tags: []string{"NetCashProvidedByUsedInOperatingActivities"}, divisor: 1e9, suffix: "B"},
				{label: "Investing Cash Flow", tags: []string{"NetCashProvidedByUsedInInvestingActivities"}, divisor: 1e9, suffix: "B"},
				{label: "Financing Cash Flow", tags: []string{"NetCashProvidedByUsedInFinancingActivities"}, divisor: 1e9, suffix: "B"},
				{label: "CapEx", tags: []string{
					"PaymentsToAcquirePropertyPlantAndEquipment",
					"CapitalExpendituresIncurredButNotYetPaid",
				}, divisor: 1e9, suffix: "B"},
				{label: "Free Cash Flow (est)", tags: []string{"NetCashProvidedByUsedInOperatingActivities"}, divisor: 1e9, suffix: "B"},
			},
		},
	}

	def, ok := statements[statement]
	if !ok {
		return "", fmt.Errorf("unknown statement %q — use: income, balance, cashflow", statement)
	}

	// Fetch all concepts. Collect the union of period end-dates.
	type row struct {
		label  string
		byDate map[string]float64
		div    float64
		suffix string
	}

	allDates := map[string]bool{}
	rows := make([]row, 0, len(def.concepts))

	for _, c := range def.concepts {
		r := row{label: c.label, byDate: map[string]float64{}, div: c.divisor, suffix: c.suffix}
		for _, tag := range c.tags {
			entries, err := secConcept(cik, tag, form)
			if err != nil || len(entries) == 0 {
				continue
			}
			// Take last 8 periods.
			if len(entries) > 8 {
				entries = entries[len(entries)-8:]
			}
			for _, e := range entries {
				r.byDate[e.End] = e.Val
				allDates[e.End] = true
			}
			break // use first tag that returned data
		}
		rows = append(rows, r)
	}

	// Sort dates.
	dates := make([]string, 0, len(allDates))
	for d := range allDates {
		dates = append(dates, d)
	}
	sort.Strings(dates)
	if len(dates) > 5 {
		dates = dates[len(dates)-5:]
	}

	if len(dates) == 0 {
		return fmt.Sprintf("No SEC EDGAR XBRL data found for %s. The company may not be US-listed or may not file XBRL.", sym), nil
	}

	// Build output table.
	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s %s (%s) — via SEC EDGAR\n\n", sym, def.title, frequency)

	// Header row.
	sb.WriteString(fmt.Sprintf("%-30s", ""))
	for _, d := range dates {
		sb.WriteString(fmt.Sprintf("  %12s", d))
	}
	sb.WriteByte('\n')
	sb.WriteString(strings.Repeat("-", 30+14*len(dates)) + "\n")

	for _, r := range rows {
		if len(r.byDate) == 0 {
			continue
		}
		fmt.Fprintf(&sb, "%-30s", r.label)
		for _, d := range dates {
			v, ok := r.byDate[d]
			if !ok {
				sb.WriteString(fmt.Sprintf("  %12s", "—"))
				continue
			}
			if r.div == 1 {
				sb.WriteString(fmt.Sprintf("  %12.2f", v))
			} else {
				sb.WriteString(fmt.Sprintf("  %11.2f%s", v/r.div, r.suffix))
			}
		}
		sb.WriteByte('\n')
	}

	sb.WriteString("\nValues in USD billions (B) unless noted. Source: SEC EDGAR XBRL.\n")
	return sb.String(), nil
}
