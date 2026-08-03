package chromedp

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/rajkumaar23/firefly-bridge/internal/csv"
	"github.com/rajkumaar23/firefly-bridge/internal/firefly"
	"github.com/rajkumaar23/firefly-bridge/internal/utils"
	"gopkg.in/yaml.v3"
)

type StepType string

const (
	StepNavigate        StepType = "navigate"
	StepWait            StepType = "wait_visible"
	StepWaitNotVisible  StepType = "wait_not_visible"
	StepWaitNotPresent  StepType = "wait_not_present"
	StepClick           StepType = "click"
	StepSleep           StepType = "sleep"
	StepReload          StepType = "reload"
	StepSendKey         StepType = "send_keys"
	StepSetValue        StepType = "set_value"
	StepGetBalance      StepType = "balance"
	StepGetTransactions StepType = "transactions"
	StepGetHoldings     StepType = "holdings"
	StepGetOrders       StepType = "orders"
	StepEvaluate        StepType = "evaluate"
	StepSkipRemainingIf StepType = "skip_remaining_if"
)

// ErrSkipRemaining is returned by a skip_remaining_if step whose condition is
// truthy. RunSteps treats it as a successful early exit from the flow, not an
// error.
var ErrSkipRemaining = errors.New("skip remaining steps")

type BrowserStep struct {
	Step Step
}

func (b *BrowserStep) UnmarshalYAML(value *yaml.Node) error {
	var typeHolder struct {
		Type StepType `yaml:"type"`
	}

	if err := value.Decode(&typeHolder); err != nil {
		return err
	}

	var step Step

	switch typeHolder.Type {
	case StepNavigate:
		step = &NavigateStep{}
	case StepWait:
		step = &WaitStep{}
	case StepWaitNotVisible:
		step = &WaitNotVisibleStep{}
	case StepWaitNotPresent:
		step = &WaitNotPresentStep{}
	case StepClick:
		step = &ClickStep{}
	case StepSleep:
		step = &SleepStep{}
	case StepReload:
		step = &ReloadStep{}
	case StepSendKey:
		step = &SendKeyStep{}
	case StepSetValue:
		step = &SetValueStep{}
	case StepGetBalance:
		step = &BalanceStep{}
	case StepGetTransactions:
		step = &GetTransactionsStep{}
	case StepGetHoldings:
		step = &HoldingsStep{}
	case StepGetOrders:
		step = &OrdersStep{}
	case StepEvaluate:
		step = &EvaluateStep{}
	case StepSkipRemainingIf:
		step = &SkipRemainingIfStep{}
	default:
		return fmt.Errorf("unknown browser step type: %s", typeHolder.Type)
	}

	if err := value.Decode(step); err != nil {
		return err
	}

	b.Step = step
	return nil
}

type Step interface {
	Type() StepType
	Execute(c *ChromeDP, results map[StepType]interface{}) error
}

// awaitPromise is an EvalOption that instructs Chrome to await a returned Promise.
// Required when the evaluate expression is an async function.
func awaitPromise(p *runtime.EvaluateParams) *runtime.EvaluateParams {
	return p.WithAwaitPromise(true)
}

// Below are implementations of different step types.
// Each step type has its own struct and implements the Step interface.

// NavigateStep represents a step to navigate to a specific URL.
type NavigateStep struct {
	URL string `yaml:"url" validate:"required,http_url"`
}

func (s NavigateStep) Type() StepType {
	return StepNavigate
}

func (s NavigateStep) Execute(c *ChromeDP, results map[StepType]interface{}) error {
	return chromedp.Run(c.Ctx, chromedp.Navigate(s.URL))
}

// WaitStep represents a step to wait until a specific element is visible on the page.
type WaitStep struct {
	Selector string `yaml:"selector" validate:"required_without=JSPath"`
	JSPath   string `yaml:"js_path" validate:"required_without=Selector"`
}

func (s WaitStep) Type() StepType {
	return StepWait
}

func (s WaitStep) Execute(c *ChromeDP, results map[StepType]interface{}) error {
	if s.JSPath != "" {
		return chromedp.Run(c.Ctx, chromedp.WaitVisible(s.JSPath, chromedp.ByJSPath))
	}
	return chromedp.Run(c.Ctx, chromedp.WaitVisible(s.Selector))
}

// WaitStepNotVisible represents a step to wait until a specific element is not visible on the page.
type WaitNotVisibleStep struct {
	Selector string `yaml:"selector" validate:"required_without=JSPath"`
	JSPath   string `yaml:"js_path" validate:"required_without=Selector"`
}

func (s WaitNotVisibleStep) Type() StepType {
	return StepWaitNotVisible
}

func (s WaitNotVisibleStep) Execute(c *ChromeDP, results map[StepType]interface{}) error {
	if s.JSPath != "" {
		return chromedp.Run(c.Ctx, chromedp.WaitNotVisible(s.JSPath, chromedp.ByJSPath))
	}
	return chromedp.Run(c.Ctx, chromedp.WaitNotVisible(s.Selector))
}

// WaitNotPresentStep represents a step to wait until a specific element is removed from the DOM.
type WaitNotPresentStep struct {
	Selector string `yaml:"selector" validate:"required_without=JSPath"`
	JSPath   string `yaml:"js_path" validate:"required_without=Selector"`
}

func (s WaitNotPresentStep) Type() StepType {
	return StepWaitNotPresent
}

func (s WaitNotPresentStep) Execute(c *ChromeDP, results map[StepType]interface{}) error {
	if s.JSPath != "" {
		return chromedp.Run(c.Ctx, chromedp.WaitNotPresent(s.JSPath, chromedp.ByJSPath))
	}
	return chromedp.Run(c.Ctx, chromedp.WaitNotPresent(s.Selector))
}

// ClickStep represents a step to click on a specific element on the page.
type ClickStep struct {
	Selector string `yaml:"selector" validate:"required_without=JSPath"`
	JSPath   string `yaml:"js_path" validate:"required_without=Selector"`
}

func (s ClickStep) Type() StepType {
	return StepClick
}

func (s ClickStep) Execute(c *ChromeDP, results map[StepType]interface{}) error {
	if s.JSPath != "" {
		return chromedp.Run(c.Ctx, chromedp.Click(s.JSPath, chromedp.ByJSPath))
	}
	return chromedp.Run(c.Ctx, chromedp.Click(s.Selector))
}

// SendKeyStep represents a step to send keys (input) to a specific element on the page.
type SendKeyStep struct {
	Selector string `yaml:"selector" validate:"required_without=JSPath"`
	JSPath   string `yaml:"js_path" validate:"required_without=Selector"`
	Value    string `yaml:"value" validate:"required"`
}

func (s SendKeyStep) Type() StepType {
	return StepSendKey
}

func (s SendKeyStep) Execute(c *ChromeDP, results map[StepType]interface{}) error {
	val, err := utils.ParseTemplate(c.Ctx, s.Value, c.secretResolver)
	if err != nil {
		return fmt.Errorf("failed to parse template: %w", err)
	}
	if s.JSPath != "" {
		return chromedp.Run(c.Ctx, chromedp.SendKeys(s.JSPath, val, chromedp.ByJSPath))
	}
	return chromedp.Run(c.Ctx, chromedp.SendKeys(s.Selector, val))
}

// SetValueStep represents a step to set a value for a specific element on the page.
type SetValueStep struct {
	Selector string `yaml:"selector" validate:"required_without=JSPath"`
	JSPath   string `yaml:"js_path" validate:"required_without=Selector"`
	Value    string `yaml:"value" validate:"required"`
}

func (s SetValueStep) Type() StepType {
	return StepSetValue
}

func (s SetValueStep) Execute(c *ChromeDP, results map[StepType]interface{}) error {
	val, err := utils.ParseTemplate(c.Ctx, s.Value, c.secretResolver)
	if err != nil {
		return fmt.Errorf("failed to parse template: %w", err)
	}
	if s.JSPath != "" {
		return chromedp.Run(c.Ctx, chromedp.SetValue(s.JSPath, val, chromedp.ByJSPath))
	}
	return chromedp.Run(c.Ctx, chromedp.SetValue(s.Selector, val))
}

// SleepStep represents a step to pause execution for a specified duration.
type SleepStep struct {
	Duration time.Duration `yaml:"duration" validate:"required"`
}

func (s SleepStep) Type() StepType {
	return StepSleep
}

func (s SleepStep) Execute(c *ChromeDP, results map[StepType]interface{}) error {
	return chromedp.Run(c.Ctx, chromedp.Sleep(s.Duration))
}

// ReloadStep represents a step to reload the current page.
type ReloadStep struct{}

func (r ReloadStep) Type() StepType {
	return StepReload
}

func (r ReloadStep) Execute(c *ChromeDP, results map[StepType]interface{}) error {
	return chromedp.Run(c.Ctx, chromedp.Reload())
}

// BalanceStep represents a step to retrieve the balance from a specific element on the page.
type BalanceStep struct {
	Selector string `yaml:"selector" validate:"required_without=Evaluate"`
	Evaluate string `yaml:"evaluate" validate:"required_without=Selector"`
}

func (s BalanceStep) Type() StepType {
	return StepGetBalance
}

func (s BalanceStep) Execute(c *ChromeDP, results map[StepType]interface{}) error {
	var result string
	var action chromedp.Action
	if s.Selector != "" {
		action = chromedp.Text(s.Selector, &result)
	} else if s.Evaluate != "" {
		eval, err := c.secretResolver.ResolveRefs(c.Ctx, s.Evaluate)
		if err != nil {
			return fmt.Errorf("failed to resolve secret refs: %w", err)
		}
		action = chromedp.Evaluate(eval, &result, awaitPromise)
	} else {
		return fmt.Errorf("either selector or evaluate must be provided")
	}

	if err := chromedp.Run(c.Ctx, action); err != nil {
		return err
	}
	results[s.Type()] = result
	return nil
}

// HoldingsStep represents a step to retrieve stock holdings from the page
// Multiple holdings steps will merge their results into a single map
type HoldingsStep struct {
	Evaluate string `yaml:"evaluate" validate:"required"`
}

func (s HoldingsStep) Type() StepType {
	return StepGetHoldings
}

func (s HoldingsStep) Execute(c *ChromeDP, results map[StepType]interface{}) error {
	var jsResult interface{}
	action := chromedp.Evaluate(s.Evaluate, &jsResult)

	if err := chromedp.Run(c.Ctx, action); err != nil {
		return fmt.Errorf("failed to evaluate holdings JavaScript: %w", err)
	}

	// Parse the JavaScript result into FireflyHoldings
	holdings, err := parseHoldingsFromJS(jsResult)
	if err != nil {
		return fmt.Errorf("failed to parse holdings from JavaScript result: %w", err)
	}

	// Merge with existing holdings if any (allows multiple holdings steps)
	if existingHoldings, ok := results[s.Type()].(*firefly.FireflyHoldings); ok {
		for symbol, qty := range *holdings {
			(*existingHoldings)[symbol] = qty
		}
	} else {
		results[s.Type()] = holdings
	}

	return nil
}

// parseHoldingsFromJS converts JavaScript evaluation result to FireflyHoldings
// Expects a JavaScript object like: {symbol: quantity, symbol2: quantity2}
func parseHoldingsFromJS(jsResult interface{}) (*firefly.FireflyHoldings, error) {
	holdings := make(firefly.FireflyHoldings)

	// JavaScript returns map[string]interface{} for objects
	rawMap, ok := jsResult.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("expected JavaScript object, got %T", jsResult)
	}

	for symbol, value := range rawMap {
		// Convert quantity to float64
		var qty float64
		switch v := value.(type) {
		case float64:
			qty = v
		case int:
			qty = float64(v)
		case int64:
			qty = float64(v)
		default:
			return nil, fmt.Errorf("unsupported quantity type for symbol %s: %T", symbol, value)
		}

		holdings[symbol] = qty
	}

	return &holdings, nil
}

// Order is a raw order row scraped from a vendor page by an OrdersStep. Dates
// and amounts are kept as strings here; internal/vendor parses them (this type
// lives in this package so that vendor can import chromedp without a cycle).
type Order struct {
	Date     string `json:"date"`
	Amount   string `json:"amount"`
	Items    string `json:"items"`
	Category string `json:"category"`
}

// OrdersStep scrapes vendor orders from the current page by evaluating a JS
// snippet that returns an array of {date, amount, items, category?} objects.
// Multiple orders steps in one flow (e.g. one per order-history page)
// accumulate their results.
type OrdersStep struct {
	Evaluate string `yaml:"evaluate" validate:"required"`
}

func (s OrdersStep) Type() StepType {
	return StepGetOrders
}

func (s OrdersStep) Execute(c *ChromeDP, results map[StepType]interface{}) error {
	eval, err := c.secretResolver.ResolveRefs(c.Ctx, s.Evaluate)
	if err != nil {
		return fmt.Errorf("failed to resolve secret refs: %w", err)
	}

	// Capture the raw JSON first so the exact payload can be logged when the
	// scrape returns nothing useful — vendor pages change often and this is
	// the fastest way to see what the JS actually produced.
	var raw json.RawMessage
	if err := chromedp.Run(c.Ctx, chromedp.Evaluate(eval, &raw, awaitPromise)); err != nil {
		return fmt.Errorf("failed to evaluate orders JavaScript: %w", err)
	}
	utils.GetLogger(c.Ctx).Debugf("orders step returned: %s", truncate(string(raw), 2000))

	var orders []Order
	if err := json.Unmarshal(raw, &orders); err != nil {
		return fmt.Errorf("orders JavaScript must return an array of {date, amount, items}, got %s: %w",
			truncate(string(raw), 200), err)
	}

	if existing, ok := results[s.Type()].([]Order); ok {
		results[s.Type()] = append(existing, orders...)
	} else {
		results[s.Type()] = orders
	}
	return nil
}

// EvaluateStep runs arbitrary JavaScript on the page and discards the result.
// Use this for side-effectful JS (e.g. triggering a download, clicking via JS).
type EvaluateStep struct {
	Evaluate string `yaml:"evaluate" validate:"required"`
}

func (s EvaluateStep) Type() StepType {
	return StepEvaluate
}

func (s EvaluateStep) Execute(c *ChromeDP, results map[StepType]interface{}) error {
	logger := utils.GetLogger(c.Ctx)
	eval, err := c.secretResolver.ResolveRefs(c.Ctx, s.Evaluate)
	if err != nil {
		return fmt.Errorf("failed to resolve secret refs: %w", err)
	}
	var discard interface{}
	if err := chromedp.Run(c.Ctx, chromedp.Evaluate(eval, &discard, awaitPromise)); err != nil {
		logger.Errorf("evaluate step failed: %v", err)
		return err
	}
	return nil
}

// SkipRemainingIfStep evaluates a JS expression and, when the result is
// truthy, ends the current flow early (successfully). Its main use is at the
// top of login flows: the browser profile persists between runs, so a session
// is often still valid and the credential steps would otherwise hang waiting
// for a login form that never appears.
type SkipRemainingIfStep struct {
	Evaluate string `yaml:"evaluate" validate:"required"`
}

func (s SkipRemainingIfStep) Type() StepType {
	return StepSkipRemainingIf
}

func (s SkipRemainingIfStep) Execute(c *ChromeDP, results map[StepType]interface{}) error {
	eval, err := c.secretResolver.ResolveRefs(c.Ctx, s.Evaluate)
	if err != nil {
		return fmt.Errorf("failed to resolve secret refs: %w", err)
	}

	var result interface{}
	if err := chromedp.Run(c.Ctx, chromedp.Evaluate(eval, &result, awaitPromise)); err != nil {
		return fmt.Errorf("failed to evaluate skip_remaining_if JavaScript: %w", err)
	}

	if isTruthy(result) {
		return ErrSkipRemaining
	}
	return nil
}

// truncate shortens s for logging, marking that it was cut.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "… (truncated)"
}

// isTruthy applies JavaScript-like truthiness to an evaluated result.
func isTruthy(v interface{}) bool {
	switch val := v.(type) {
	case nil:
		return false
	case bool:
		return val
	case string:
		return val != ""
	case float64:
		return val != 0
	case int:
		return val != 0
	default:
		return true
	}
}

type (
	GetTransactionsStep struct {
		CSV *struct {
			Opts   *csv.Options     `yaml:"options"`
			Config *csv.FieldConfig `yaml:"fields" validate:"required,validateFn"`
		} `yaml:"csv" validate:"required_without=Excel,excluded_with=Excel"`
		Excel *struct {
			// Worksheet is the 1-based index of the worksheet to parse.
			Worksheet int              `yaml:"worksheet" validate:"required"`
			Password  string           `yaml:"password"`
			Opts      *csv.Options     `yaml:"options"`
			Config    *csv.FieldConfig `yaml:"fields" validate:"required,validateFn"`
		} `yaml:"excel" validate:"required_without=CSV,excluded_with=CSV"`
	}
)

func (s GetTransactionsStep) Type() StepType {
	return StepGetTransactions
}

func (s GetTransactionsStep) Execute(c *ChromeDP, results map[StepType]interface{}) error {
	fileName := <-c.downloadChannel
	logger := utils.GetLogger(c.Ctx)
	logger.Debugf("received download event for file: %s", fileName)

	fullPath := filepath.Join(c.downloadPath, fileName)

	var transactions []*firefly.TransactionSplitStore
	var err error
	if s.Excel != nil {
		var password string
		password, err = c.secretResolver.ResolveRefs(c.Ctx, s.Excel.Password)
		if err != nil {
			return fmt.Errorf("failed to resolve excel password: %w", err)
		}
		parser := csv.NewParser(c.Ctx, s.Excel.Opts, s.Excel.Config, c.CSVDebug)
		transactions, err = parser.ParseFromExcel(fullPath, s.Excel.Worksheet-1, password)
	} else if s.CSV != nil {
		parser := csv.NewParser(c.Ctx, s.CSV.Opts, s.CSV.Config, c.CSVDebug)
		transactions, err = parser.Parse(fullPath)
	} else {
		return fmt.Errorf("either csv or excel must be configured for get_transactions step")
	}
	if err != nil {
		return fmt.Errorf("error parsing transactions: %w", err)
	}

	if existing, ok := results[s.Type()].([]*firefly.TransactionSplitStore); ok {
		results[s.Type()] = append(existing, transactions...)
	} else {
		results[s.Type()] = transactions
	}
	return nil
}
