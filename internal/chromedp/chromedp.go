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

type StepType string

const (
	StepNavigate StepType = "navigate"
	StepWait     StepType = "wait_visible"
	StepClick    StepType = "click"
	StepEvaluate StepType = "evaluate"
	StepSleep    StepType = "sleep"
	StepSendKey  StepType = "send_keys"
)

type BrowserStep struct {
	Type     StepType      `yaml:"type" validate:"required,oneof=navigate wait_visible click evaluate sleep send_keys"`
	URL      string        `yaml:"url" validate:"required_if=Type navigate,omitempty,http_url"`
	Selector string        `yaml:"selector" validate:"required_if=Type wait_visible,required_if=Type click,required_if=Type send_keys"`
	Script   string        `yaml:"script" validate:"required_if=Type evaluate"`
	Duration time.Duration `yaml:"duration" validate:"required_if=Type sleep"`
	Value    string        `yaml:"value" validate:"required_if=Type send_keys"`
}

func (c *ChromeDP) RunSteps(steps []BrowserStep) error {
	actions := []chromedp.Action{}
	for _, step := range steps {
		switch step.Type {
		case StepNavigate:
			actions = append(actions, chromedp.Navigate(step.URL))

		case StepWait:
			actions = append(actions, chromedp.WaitVisible(step.Selector))

		case StepClick:
			actions = append(actions, chromedp.Click(step.Selector))

		case StepSendKey:
			actions = append(actions, chromedp.SendKeys(step.Selector, step.Value))

		case StepEvaluate:
			actions = append(actions, chromedp.Evaluate(step.Script, nil))

		case StepSleep:
			actions = append(actions, chromedp.Sleep(step.Duration))
		}
	}

	return chromedp.Run(c.Ctx, actions...)
}
