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
	"github.com/rajkumaar23/firefly-bridge/internal/utils"
	"github.com/sirupsen/logrus"
)

type ChromeDP struct {
	Ctx             context.Context
	CSVDebug        bool
	cancelFuncs     []context.CancelFunc
	workingDir      string
	downloadPath    string
	downloadChannel chan string
	secretResolver  utils.SecretResolver
}

func NewChromeDP(ctx context.Context, logger *logrus.Logger, browserExecPath string, downloads int, debug bool, secretResolver utils.SecretResolver) (cdp *ChromeDP, err error) {
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
		secretResolver:  secretResolver,
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
