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
// marketID is a string that identifies the market to fetch the price from. It can be one of the following:
// - cash: returns 1 (used for cash accounts in firefly)
// - mi: fetches the current NAV for the given fund from markets.businessinsider.com
// - mc: fetches the current NAV for the given mutual fund from moneycontrol.com
// - gold-{purity}: fetches the current gold spot price from kitco.com and applies the purity multiplier, where {purity} is a decimal number between 0 and 1 representing the purity of the gold (eg: gold-0.916 for 22k gold, gold-1 for 24)
// - any other value: treated as a stock symbol and fetches the current stock price for the given symbol from finance.yahoo.com
func (m *Market) GetPrice(marketID string, symbol string) (float64, error) {
	if strings.HasPrefix(marketID, GoldPrefix) {
		purityStr := strings.TrimPrefix(marketID, GoldPrefix)
		purity, err := strconv.ParseFloat(purityStr, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid purity percentage in market ID: %s", marketID)
		}
		if purity <= 0 || purity > 1 {
			return 0, fmt.Errorf("purity percentage must be between 0 and 1 in market ID: %s", marketID)
		}
		return m.getGoldSpotPrice(purity)
	}

	switch marketID {
	case CashPrefix:
		return 1, nil
	case MarketsInsiderPrefix:
		return m.getMarketsInsiderNAV(symbol)
	case MoneyControlPrefix:
		return m.getMoneyControlNAV(symbol)
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

// getGoldSpotPrice fetches the current gold spot price from kitco.com
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
			PM struct {
				Symbol     string  `json:"symbol"`
				CurrentBid float64 `json:"currentBid,string"`
			} `json:"PM"`
		} `json:"PreciousMetals"`
	}
	err = json.Unmarshal(body, &kitcoRes)
	if err != nil {
		return 0, fmt.Errorf("error unmarshaling json response from kitco: %w", err)
	}

	if kitcoRes.PreciousMetals.PM.Symbol != "AU" {
		return 0, fmt.Errorf("unexpected symbol in kitco response: %s", kitcoRes.PreciousMetals.PM.Symbol)
	}

	if kitcoRes.PreciousMetals.PM.CurrentBid == 0 {
		return 0, fmt.Errorf("gold spot price is zero in kitco response")
	}

	return kitcoRes.PreciousMetals.PM.CurrentBid * purityMultiplier, nil
}
