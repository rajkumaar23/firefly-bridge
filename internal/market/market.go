package market

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
)

type Market struct {
	resty *resty.Client
}

const (
	MarketsInsiderPrefix = "mi"
	MoneyControlPrefix   = "mc"
	CashPrefix           = "cash"
	GoldPrefix           = "gold"
)

func NewMarket() *Market {
	client := resty.New()
	client.SetHeader("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.3 Safari/605.1.15")
	client.SetHeader("Content-Type", "application/json")
	return &Market{resty: client}
}

// GetPrice fetches the current price for the given market ID and symbol
// marketID is a string that identifies the market to fetch the price from. It can be one of the following (case-insensitive):
// - cash: returns 1 (used for cash accounts in firefly)
// - mi: fetches the current NAV for the given fund from markets.businessinsider.com
// - mc: fetches the current NAV for the given mutual fund from moneycontrol.com
// - gold: fetches the current gold spot price from kitco.com (purity of the gold should be specified in the symbol, eg: 916 for 22k gold, 999 for 24k gold)
// - any other value: treated as a stock symbol and fetches the current stock price for the given symbol from finance.yahoo.com
func (m *Market) GetPrice(marketID string, symbol string) (float64, error) {
	marketIDLower := strings.ToLower(marketID)
	switch marketIDLower {
	case CashPrefix:
		return 1, nil
	case MarketsInsiderPrefix:
		return m.getMarketsInsiderNAV(symbol)
	case MoneyControlPrefix:
		return m.getMoneyControlNAV(symbol)
	case GoldPrefix:
		purity, err := strconv.ParseFloat(symbol, 64)
		if err != nil {
			return 0, fmt.Errorf("error parsing purity for gold in symbol: %s", symbol)
		}
		purityPercentage := purity / 1000
		return m.getGoldSpotPrice(purityPercentage)
	default:
		return m.getYahooStockPrice(symbol)
	}
}

// getYahooStockPrice fetches the current stock price for the given symbol from finance.yahoo.com
func (m *Market) getYahooStockPrice(symbol string) (float64, error) {
	var yahooRes struct {
		Chart struct {
			Result []struct {
				Meta struct {
					CurrentPrice float64 `json:"regularMarketPrice"`
				} `json:"meta"`
			} `json:"result"`
		} `json:"chart"`
	}
	resp, err := m.resty.R().
		SetResult(&yahooRes).
		Get(fmt.Sprintf("https://query2.finance.yahoo.com/v8/finance/chart/%s", symbol))
	if err != nil {
		return 0, err
	}
	if !resp.IsSuccess() || resp.IsError() {
		return 0, fmt.Errorf("unexpected status code: %s, body: %s", resp.Status(), resp.Body())
	}

	if len(yahooRes.Chart.Result) == 0 {
		return 0, fmt.Errorf("no result found for symbol: %s", symbol)
	}

	price := yahooRes.Chart.Result[0].Meta.CurrentPrice
	if price == 0 {
		return 0, fmt.Errorf("stock price is zero for symbol: %s", symbol)
	}

	return yahooRes.Chart.Result[0].Meta.CurrentPrice, nil
}

// getMarketsInsiderNAV fetches the current NAV for the given fund from markets.businessinsider.com
func (m *Market) getMarketsInsiderNAV(path string) (float64, error) {
	resp, err := m.resty.R().
		Get(fmt.Sprintf("https://markets.businessinsider.com/funds/%s", path))
	if err != nil {
		return 0, err
	}
	body := resp.Body()
	if !resp.IsSuccess() || resp.IsError() {
		return 0, fmt.Errorf("unexpected status code: %s, body: %s", resp.Status(), body)
	}

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return 0, err
	}

	priceStr := doc.Find(".price-section__current-value").Text()
	if priceStr == "" {
		return 0, fmt.Errorf("failed to find price in the document")
	}

	price, err := strconv.ParseFloat(priceStr, 64)
	if err != nil {
		return 0, err
	}

	if price == 0 {
		return 0, fmt.Errorf("NAV price is zero for path: %s", path)
	}

	return price, nil
}

// getMoneyControlNAV fetches the current NAV for the given mutual fund from moneycontrol.com
func (m *Market) getMoneyControlNAV(path string) (float64, error) {
	resp, err := m.resty.R().
		Get(fmt.Sprintf("https://www.moneycontrol.com/mutual-funds/nav/random-string/%s", path))
	if err != nil {
		return 0, err
	}
	body := resp.Body()
	if !resp.IsSuccess() || resp.IsError() {
		return 0, fmt.Errorf("unexpected status code: %s, body: %s", resp.Status(), body)
	}

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return 0, err
	}

	nextDataText := doc.Find("#__NEXT_DATA__").Text()
	if nextDataText == "" {
		return 0, fmt.Errorf("failed to find #__NEXT_DATA__ in the document")
	}

	type nextData struct {
		Props struct {
			PageProps struct {
				Data struct {
					Overview struct {
						LatestNAV string `json:"latestNav"`
					} `json:"overview"`
				} `json:"data"`
			} `json:"pageProps"`
		} `json:"props"`
	}

	var nd nextData
	err = json.Unmarshal([]byte(nextDataText), &nd)
	if err != nil {
		return 0, err
	}

	price, err := strconv.ParseFloat(nd.Props.PageProps.Data.Overview.LatestNAV, 64)
	if err != nil {
		return 0, err
	}
	if price == 0 {
		return 0, fmt.Errorf("NAV price is zero for path: %s", path)
	}

	return price, nil
}

// getGoldSpotPrice fetches the current gold spot price per 1 troy ounce (ozt) from kitco.com
// purityMultiplier is the multiplier to apply to the spot price based on the purity of the gold (eg: 0.916 for 22k gold, 1 for 24k gold)
func (m *Market) getGoldSpotPrice(purityMultiplier float64) (float64, error) {
	resp, err := m.resty.R().
		Get("https://api.kitco.com/api/v1/precious-metals/au/")
	if err != nil {
		return 0, err
	}
	body := resp.Body()
	if !resp.IsSuccess() || resp.IsError() {
		return 0, fmt.Errorf("unexpected status code: %s, body: %s", resp.Status(), body)
	}

	var kitcoRes struct {
		PreciousMetals struct {
			PM []struct {
				Symbol     string  `json:"Symbol"`
				CurrentBid float64 `json:"current_bid,string"`
			} `json:"PM"`
		} `json:"PreciousMetals"`
	}
	err = json.Unmarshal(body, &kitcoRes)
	if err != nil {
		return 0, fmt.Errorf("error unmarshaling json response from kitco: %w", err)
	}

	pm := kitcoRes.PreciousMetals.PM
	if len(pm) == 0 {
		return 0, fmt.Errorf("no precious metals found in kitco response")
	}

	gold := pm[0]
	if gold.Symbol != "AU" {
		return 0, fmt.Errorf("unexpected symbol in kitco response: %s", gold.Symbol)
	}

	if gold.CurrentBid == 0 {
		return 0, fmt.Errorf("gold spot price is zero in kitco response")
	}

	return gold.CurrentBid * purityMultiplier, nil
}
