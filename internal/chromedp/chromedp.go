package chromedp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

type ChromeDP struct {
	Ctx             context.Context
	cancelFuncs     []context.CancelFunc
	workingDir      string
	downloadChannel chan string
}

func NewChromeDP(ctx context.Context, browserExecPath string, downloads uint8) (cdp *ChromeDP, err error) {
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

	ctx, cancel := chromedp.NewExecAllocator(ctx, opts...)
	ctx, cancel2 := chromedp.NewContext(ctx)

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
	if c.downloadChannel != nil {
		close(c.downloadChannel)
	}
	for _, cancel := range c.cancelFuncs {
		cancel()
	}
}
