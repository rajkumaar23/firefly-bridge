package chromedp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"

	"github.com/rajkumaar23/firefly-bridge/internal/csv"
	"github.com/rajkumaar23/firefly-bridge/internal/utils"
)

type ChromeDP struct {
	Ctx             context.Context
	cancelFuncs     []context.CancelFunc
	workingDir      string
	downloadPath    string
	downloadChannel chan string
}

func NewChromeDP(ctx context.Context, logger *logrus.Logger, browserExecPath string, downloads int, debug bool) (cdp *ChromeDP, err error) {
	workingDir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get working directory: %w", err)
	}

	opts := append(
		chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", false),
		chromedp.DisableGPU,
		chromedp.Flag("enable-automation", false),
		chromedp.Flag("disable-extensions", false),
		chromedp.UserDataDir(filepath.Join(workingDir, "chromedp-data")),
		chromedp.ExecPath(browserExecPath),
	)

	errorFn := func(s string, i ...interface{}) {
		if strings.Contains(s, "unhandled page event") {
			return
		}

		logger.Errorf(s, i...)
	}

	var debugFn func(s string, i ...interface{})
	if debug {
		debugFn = func(s string, i ...interface{}) {
			logger.Debugf(s, i...)

		}
	}

	ctx, cancel := chromedp.NewExecAllocator(ctx, opts...)
	ctx, cancel2 := chromedp.NewContext(ctx, chromedp.WithDebugf(debugFn), chromedp.WithErrorf(errorFn))

	downloadsDir := filepath.Join(workingDir, "downloads")
	cdp = &ChromeDP{
		Ctx:             ctx,
		cancelFuncs:     []context.CancelFunc{cancel, cancel2},
		workingDir:      workingDir,
		downloadPath:    downloadsDir,
		downloadChannel: make(chan string),
	}
	defer func() {
		if err != nil {
			cdp.Close()
		}
	}()

	if err := cdp.hideWebDriver(); err != nil {
		return nil, fmt.Errorf("failed to hide web driver: %w", err)
	}
	if err := cdp.enableDownloads(downloadsDir, downloads); err != nil {
		return nil, fmt.Errorf("failed to setup download channels: %w", err)
	}

	return cdp, nil
}

func (c *ChromeDP) hideWebDriver() error {
	return chromedp.Run(c.Ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		_, err := page.AddScriptToEvaluateOnNewDocument("Object.defineProperty(navigator, 'webdriver', { get: () => false, });").Do(ctx)
		if err != nil {
			return err
		}
		return nil
	}))
}

func (c *ChromeDP) enableDownloads(dir string, count int) error {
	c.downloadChannel = make(chan string, count)
	chromedp.ListenTarget(c.Ctx, func(v interface{}) {
		if ev, ok := v.(*browser.EventDownloadProgress); ok {
			if ev.State == browser.DownloadProgressStateCompleted {
				c.downloadChannel <- ev.GUID
			}
		}
	})

	behavior := browser.SetDownloadBehavior(browser.SetDownloadBehaviorBehaviorAllowAndName).
		WithDownloadPath(dir).
		WithEventsEnabled(true)
	return chromedp.Run(c.Ctx, behavior)
}

func (c *ChromeDP) Close() {
	close(c.downloadChannel)
	for _, cancel := range c.cancelFuncs {
		cancel()
	}
}

func (c *ChromeDP) RunSteps(steps []BrowserStep) (map[StepType]interface{}, error) {
	results := make(map[StepType]interface{})

	for _, step := range steps {
		if err := step.Step.Execute(c, results); err != nil {
			return nil, fmt.Errorf("error executing step %s: %w", step.Step.Type(), err)
		}
		time.Sleep(time.Second)
	}
	return results, nil
}

type StepType string

const (
	StepNavigate        StepType = "navigate"
	StepWait            StepType = "wait_visible"
	StepClick           StepType = "click"
	StepSleep           StepType = "sleep"
	StepSendKey         StepType = "send_keys"
	StepSetValue        StepType = "set_value"
	StepGetBalance      StepType = "balance"
	StepGetTransactions StepType = "transactions"
)

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
	case StepClick:
		step = &ClickStep{}
	case StepSleep:
		step = &SleepStep{}
	case StepSendKey:
		step = &SendKeyStep{}
	case StepSetValue:
		step = &SetValueStep{}
	case StepGetBalance:
		step = &BalanceStep{}
	case StepGetTransactions:
		step = &GetTransactionsStep{}
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
	val, err := utils.ParseTemplate(s.Value)
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
	val, err := utils.ParseTemplate(s.Value)
	if err != nil {
		return fmt.Errorf("failed to parse template: %w", err)
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
		action = chromedp.Evaluate(s.Evaluate, &result)
	} else {
		return fmt.Errorf("either selector or evaluate must be provided")
	}

	if err := chromedp.Run(c.Ctx, action); err != nil {
		return err
	}
	results[s.Type()] = result
	return nil
}

type (
	GetTransactionsStep struct {
		CSV struct {
			Opts   *csv.Options     `yaml:"options"`
			Config *csv.FieldConfig `yaml:"fields" validate:"required,validateFn"`
		} `yaml:"csv" validate:"required"`
	}
)

func (s GetTransactionsStep) Type() StepType {
	return StepGetTransactions
}

func (s GetTransactionsStep) Execute(c *ChromeDP, results map[StepType]interface{}) error {
	fileName := <-c.downloadChannel
	logger := utils.GetLogger(c.Ctx)
	logger.Debugf("received download event for file: %s", fileName)

	parser := csv.NewParser(c.Ctx, s.CSV.Opts, s.CSV.Config)
	transactions, err := parser.Parse(filepath.Join(c.downloadPath, fileName))
	if err != nil {
		return fmt.Errorf("error parsing transactions: %w", err)
	}

	results[s.Type()] = transactions
	return nil
}
