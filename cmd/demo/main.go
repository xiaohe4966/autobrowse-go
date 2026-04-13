package main

import (
	"fmt"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

// Cookie 定义
type Cookie struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Domain   string `json:"domain,omitempty"`
	Path     string `json:"path,omitempty"`
	Expires  int64  `json:"expires,omitempty"` // Unix timestamp
	HTTPOnly bool   `json:"httpOnly,omitempty"`
	Secure   bool   `json:"secure,omitempty"`
	SameSite string `json:"sameSite,omitempty"` // Strict, Lax, None
}

// Header 定义
type Header struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// TaskConfig 任务配置
type TaskConfig struct {
	URL      string   `json:"url"`
	Cookies  []Cookie `json:"cookies,omitempty"`
	Headers  []Header `json:"headers,omitempty"`
	Headless bool     `json:"headless"`
}

// 本地浏览器自动化测试 Demo
// 支持自定义 Cookie 和请求头

func main() {
	fmt.Println("🚀 启动浏览器自动化测试...")

	// ========== 配置区域（可自行修改测试） ==========
	config := TaskConfig{
		URL:      "https://httpbin.org/cookies",
		Headless: false, // true=无头模式, false=显示窗口
		Cookies: []Cookie{
			{
				Name:     "session_id",
				Value:    "abc123xyz789",
				Domain:   "httpbin.org",
				Path:     "/",
				HTTPOnly: true,
				Secure:   true,
			},
			{
				Name:  "user_pref",
				Value: "dark_mode",
				Path:  "/",
			},
		},
		Headers: []Header{
			{
				Name:  "X-Custom-Header",
				Value: "MyCustomValue",
			},
			{
				Name:  "Authorization",
				Value: "Bearer token123456",
			},
		},
	}

	// 打印配置
	printConfig(config)

	// 启动浏览器
	l := launcher.New().
		Bin("").
		Headless(!config.Headless). // 显示窗口模式
		NoSandbox(true)

	url := l.MustLaunch()
	browser := rod.New().ControlURL(url).MustConnect()
	defer browser.MustClose()

	fmt.Println("\n✅ 浏览器已启动")

	// 创建新页面
	page := browser.MustPage()
	defer page.MustClose()

	// 设置请求头
	if len(config.Headers) > 0 {
		fmt.Println("\n📍 设置自定义请求头:")
		setCustomHeaders(page, config.Headers)
	}

	// 设置 Cookie
	if len(config.Cookies) > 0 {
		fmt.Println("\n📍 设置自定义 Cookie:")
		setCookies(page, config.Cookies)
	}

	// 访问页面
	fmt.Printf("\n📍 访问页面: %s\n", config.URL)
	page.MustNavigate(config.URL)
	page.MustWaitLoad()
	fmt.Println("   ✅ 页面加载完成")

	time.Sleep(1 * time.Second)

	// 验证 Cookie
	fmt.Println("\n📍 验证当前页面 Cookie:")
	verifyCookies(page)

	// 截图
	fmt.Println("\n📍 保存截图")
	screenshotPath := "/tmp/demo_result.png"
	page.MustScreenshot(screenshotPath)
	fmt.Printf("   ✅ 截图已保存: %s\n", screenshotPath)

	// 测试百度搜索
	fmt.Println("\n" + repeatStr("=", 50))
	fmt.Println("📍 额外测试: 百度搜索")

	page.MustNavigate("https://www.baidu.com")
	page.MustWaitLoad()

	// 设置百度测试 cookie
	setCookies(page, []Cookie{
		{Name: "test_cookie", Value: "from_demo", Domain: ".baidu.com", Path: "/"},
	})

	fmt.Println("\n📍 输入搜索关键词")
	page.MustElement("#kw").MustInput("Go rod 浏览器自动化")
	page.MustElement("#su").MustClick()
	fmt.Println("   ✅ 已点击搜索")

	time.Sleep(2 * time.Second)
	page.MustWaitLoad()

	// 提取结果
	firstResult := page.MustElement("#content_left .result:first-child h3")
	title := firstResult.MustText()
	fmt.Printf("   ✅ 第一个结果: %s\n", title)

	// 截图
	baiduScreenshot := "/tmp/baidu_demo.png"
	page.MustScreenshot(baiduScreenshot)
	fmt.Printf("   ✅ 截图: %s\n", baiduScreenshot)

	// 完成
	fmt.Println("\n" + repeatStr("=", 50))
	fmt.Println("✅ 测试流程全部完成！")
	fmt.Printf("\n📊 测试摘要:\n")
	fmt.Printf("   - 访问页面: %s\n", config.URL)
	fmt.Printf("   - 百度搜索: %s\n", title)
	fmt.Printf("   - 截图文件: %s, %s\n", screenshotPath, baiduScreenshot)

	fmt.Println("\n⏳ 10秒后关闭浏览器...")
	time.Sleep(10 * time.Second)
}

// 打印配置
func printConfig(config TaskConfig) {
	fmt.Println("\n📋 任务配置:")
	fmt.Printf("   URL: %s\n", config.URL)
	fmt.Printf("   Headless: %v\n", config.Headless)

	if len(config.Cookies) > 0 {
		fmt.Printf("   Cookies (%d):\n", len(config.Cookies))
		for i, c := range config.Cookies {
			fmt.Printf("      [%d] %s=%s (domain=%s, path=%s)\n",
				i+1, c.Name, c.Value, c.Domain, c.Path)
		}
	}

	if len(config.Headers) > 0 {
		fmt.Printf("   Headers (%d):\n", len(config.Headers))
		for i, h := range config.Headers {
			fmt.Printf("      [%d] %s: %s\n", i+1, h.Name, h.Value)
		}
	}
}

// 设置自定义请求头（通过 JS 拦截实现）
func setCustomHeaders(page *rod.Page, headers []Header) {
	for _, h := range headers {
		fmt.Printf("   ➕ %s: %s\n", h.Name, h.Value)
	}

	// 通过 JS 拦截 fetch/XMLHttpRequest 添加请求头
	headerJSON := "{"
	for i, h := range headers {
		if i > 0 {
			headerJSON += ", "
		}
		headerJSON += fmt.Sprintf(`"%s": "%s"`, h.Name, h.Value)
	}
	headerJSON += "}"

	page.MustEval(fmt.Sprintf(`() => {
		const extraHeaders = %s;
		const origFetch = window.fetch;
		window.fetch = function(url, options = {}) {
			options.headers = { ...options.headers, ...extraHeaders };
			return origFetch(url, options);
		};
		const origXHR = window.XMLHttpRequest;
		window.XMLHttpRequest = function() {
			const xhr = new origXHR();
			const origOpen = xhr.open;
			xhr.open = function(method, url, ...args) {
				for (const [k, v] of Object.entries(extraHeaders)) {
					xhr.setRequestHeader(k, v);
				}
				return origOpen.call(this, method, url, ...args);
			};
			return xhr;
		};
	}`, headerJSON))

	fmt.Println("   ✅ 请求头已设置")
}

// 设置 Cookie
func setCookies(page *rod.Page, cookies []Cookie) {
	for _, c := range cookies {
		params := proto.NetworkCookieParam{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			HTTPOnly: c.HTTPOnly,
			Secure:   c.Secure,
		}

		if c.Expires > 0 {
			params.Expires = proto.TimeSinceEpoch(c.Expires)
		}

		switch c.SameSite {
		case "Strict":
			params.SameSite = proto.NetworkCookieSameSiteStrict
		case "Lax":
			params.SameSite = proto.NetworkCookieSameSiteLax
		case "None":
			params.SameSite = proto.NetworkCookieSameSiteNone
		}

		page.Browser().SetCookies([]*proto.NetworkCookieParam{&params})
		domain := c.Domain
		if domain == "" {
			domain = "(自动)"
		}
		fmt.Printf("   🍪 %s=%s (domain=%s)\n", c.Name, c.Value, domain)
	}
	fmt.Println("   ✅ Cookie 已设置")
}

// 验证当前页面的 Cookie
func verifyCookies(page *rod.Page) {
	cookies, _ := page.Browser().GetCookies()
	if len(cookies) == 0 {
		fmt.Println("   ⚠️ 当前页面无 Cookie")
		return
	}
	for _, c := range cookies {
		fmt.Printf("   🍪 %s=%s (domain=%s, path=%s)\n",
			c.Name, c.Value, c.Domain, c.Path)
	}
}

func repeatStr(s string, count int) string {
	result := ""
	for i := 0; i < count; i++ {
		result += s
	}
	return result
}
