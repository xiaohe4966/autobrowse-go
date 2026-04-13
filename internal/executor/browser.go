package executor

import (
	"sync"

	"github.com/go-rod/rod"
)

// BrowserPool 浏览器池
type BrowserPool struct {
	browser *rod.Browser
	mux     struct {
		sync.Mutex
	}
}

// NewBrowserPool 创建浏览器池
func NewBrowserPool() (*BrowserPool, error) {
	b := rod.New().
		Headless(true).
		NoSandbox(true).
		MustConnect()
	
	return &BrowserPool{browser: b}, nil
}

// NewContext 创建一个新的浏览器上下文
func (p *BrowserPool) NewContext() (*rod.BrowserContext, error) {
	return p.browser.NewContext()
}

// Close 关闭浏览器
func (p *BrowserPool) Close() error {
	return p.browser.Close()
}

// Browser 返回原始浏览器实例
func (p *BrowserPool) Browser() *rod.Browser {
	return p.browser
}
