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

	"github.com/rajkumaar23/firefly-bridge/internal/firefly"
	"github.com/rajkumaar23/firefly-bridge/internal/utils"
)

type ChromeDP struct {
	Ctx             context.Context
	cancelFuncs     []context.CancelFunc
	workingDir      string
	downloadChannel chan string
}

func NewChromeDP(ctx context.Context, logger *logrus.Logger, browserExecPath string, downloads uint8, debug bool) (cdp *ChromeDP, err error) {
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

	cdp = &ChromeDP{
		Ctx:             ctx,
		cancelFuncs:     []context.CancelFunc{cancel, cancel2},
		workingDir:      workingDir,
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
	if err := cdp.enableDownloads(downloads); err != nil {
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

func (c *ChromeDP) enableDownloads(count uint8) error {
	c.downloadChannel = make(chan string, count)
	chromedp.ListenTarget(c.Ctx, func(v interface{}) {
		if ev, ok := v.(*browser.EventDownloadProgress); ok {
			if ev.State == browser.DownloadProgressStateCompleted {
				c.downloadChannel <- ev.GUID
			}
		}
	})

	return chromedp.Run(c.Ctx, browser.SetDownloadBehavior(browser.SetDownloadBehaviorBehaviorAllowAndName).WithDownloadPath(c.workingDir).WithEventsEnabled(true))
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
		if err := step.Step.Execute(c.Ctx, results); err != nil {
			return nil, fmt.Errorf("error executing step %s: %w", step.Step.Type(), err)
		}
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
	Execute(ctx context.Context, results map[StepType]interface{}) error
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

func (s NavigateStep) Execute(ctx context.Context, results map[StepType]interface{}) error {
	return chromedp.Run(ctx, chromedp.Navigate(s.URL))
}

// WaitStep represents a step to wait until a specific element is visible on the page.
type WaitStep struct {
	Selector string `yaml:"selector" validate:"required"`
}

func (s WaitStep) Type() StepType {
	return StepWait
}

func (s WaitStep) Execute(ctx context.Context, results map[StepType]interface{}) error {
	return chromedp.Run(ctx, chromedp.WaitVisible(s.Selector))
}

// ClickStep represents a step to click on a specific element on the page.
type ClickStep struct {
	Selector string `yaml:"selector" validate:"required"`
}

func (s ClickStep) Type() StepType {
	return StepClick
}

func (s ClickStep) Execute(ctx context.Context, results map[StepType]interface{}) error {
	return chromedp.Run(ctx, chromedp.Click(s.Selector))
}

// SendKeyStep represents a step to send keys (input) to a specific element on the page.
type SendKeyStep struct {
	Selector string `yaml:"selector" validate:"required"`
	Value    string `yaml:"value" validate:"required"`
}

func (s SendKeyStep) Type() StepType {
	return StepSendKey
}

func (s SendKeyStep) Execute(ctx context.Context, results map[StepType]interface{}) error {
	return chromedp.Run(ctx, chromedp.SendKeys(s.Selector, s.Value))
}

// SetValueStep represents a step to set a value for a specific element on the page.
type SetValueStep struct {
	Selector string `yaml:"selector" validate:"required"`
	Value    string `yaml:"value" validate:"required"`
}

func (s SetValueStep) Type() StepType {
	return StepSetValue
}

func (s SetValueStep) Execute(ctx context.Context, results map[StepType]interface{}) error {
	val, err := utils.ParseTemplate(s.Value)
	if err != nil {
		return fmt.Errorf("failed to parse template: %w", err)
	}
	return chromedp.Run(ctx, chromedp.SetValue(s.Selector, val))
}

// SleepStep represents a step to pause execution for a specified duration.
type SleepStep struct {
	Duration time.Duration `yaml:"duration" validate:"required"`
}

func (s SleepStep) Type() StepType {
	return StepSleep
}

func (s SleepStep) Execute(ctx context.Context, results map[StepType]interface{}) error {
	return chromedp.Run(ctx, chromedp.Sleep(s.Duration))
}

// BalanceStep represents a step to retrieve the balance from a specific element on the page.
type BalanceStep struct {
	Selector string `yaml:"selector" validate:"required"`
}

func (s BalanceStep) Type() StepType {
	return StepGetBalance
}

func (s BalanceStep) Execute(ctx context.Context, results map[StepType]interface{}) error {
	var result string
	if err := chromedp.Run(ctx, chromedp.Text(s.Selector, &result)); err != nil {
		return err
	}
	results[s.Type()] = result
	return nil
}

// GetTransactionsStep represents a step to retrieve transactions from a specific element on the page.
type GetTransactionsStep struct{}

func (s GetTransactionsStep) Type() StepType {
	return StepGetTransactions
}

func (s GetTransactionsStep) Execute(ctx context.Context, results map[StepType]interface{}) error {
	// Placeholder implementation.
	var txns []firefly.Transaction

	results[s.Type()] = txns
	return nil
}
